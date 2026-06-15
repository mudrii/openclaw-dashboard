package apprefresh

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// modelCatalog is the parsed `openclaw models list --json` result: model id
// (key) → display name and id → context window (tokens).
type modelCatalog struct {
	names   map[string]string
	windows map[string]int
}

// modelCatalogNames / modelCatalogWindows are package-level snapshots of the
// live catalog. ModelName and lookupModelLimits read them synchronously (both
// are pure / ctx-free); the catalog cache publishes into them during collection.
// A nil snapshot means "no catalog yet" → callers rely on their fallbacks.
var (
	modelCatalogNames   atomic.Pointer[map[string]string]
	modelCatalogWindows atomic.Pointer[map[string]int]
)

// setModelCatalogNames publishes a new display-name snapshot for ModelName.
func setModelCatalogNames(m map[string]string) { modelCatalogNames.Store(&m) }

// setModelCatalogWindows publishes a new context-window snapshot for lookupModelLimits.
func setModelCatalogWindows(m map[string]int) { modelCatalogWindows.Store(&m) }

// resetModelCatalogForTest clears the published snapshots and the default cache
// so a test cannot leak catalog state into another test via the package globals
// that pure ModelName/lookupModelLimits read. Call from t.Cleanup.
func resetModelCatalogForTest() {
	modelCatalogNames.Store(nil)
	modelCatalogWindows.Store(nil)
	defaultModelCatalogCache.mu.Lock()
	defaultModelCatalogCache.expiresAt = time.Time{}
	defaultModelCatalogCache.catalog = modelCatalog{}
	defaultModelCatalogCache.refreshing = false
	defaultModelCatalogCache.cond = sync.NewCond(&defaultModelCatalogCache.mu)
	defaultModelCatalogCache.mu.Unlock()
}

// catalogDisplayName returns the live display name for a model id, if the
// current catalog snapshot knows it.
func catalogDisplayName(model string) (string, bool) {
	p := modelCatalogNames.Load()
	if p == nil {
		return "", false
	}
	n, ok := (*p)[model]
	return n, ok && n != ""
}

// catalogContextWindow returns the live context window for a model id, if the
// current catalog snapshot knows it.
func catalogContextWindow(model string) (int, bool) {
	p := modelCatalogWindows.Load()
	if p == nil {
		return 0, false
	}
	w, ok := (*p)[model]
	return w, ok && w > 0
}

// parseModelCatalog parses `openclaw models list --json` output into a catalog of
// id(key) → display name and id → context window. It accepts both the envelope
// form {count, models:[…]} and a bare array. Names without a value are skipped;
// the context window prefers contextWindow and falls back to contextTokens.
func parseModelCatalog(out []byte) modelCatalog {
	cat := modelCatalog{names: map[string]string{}, windows: map[string]int{}}
	var rows []any
	var envelope struct {
		Models []any `json:"models"`
	}
	if err := json.Unmarshal(out, &envelope); err == nil && envelope.Models != nil {
		rows = envelope.Models
	} else if err := json.Unmarshal(out, &rows); err != nil {
		return cat
	}
	for _, r := range rows {
		rm := asObj(r)
		if rm == nil {
			continue
		}
		key, _ := rm["key"].(string)
		if key == "" {
			continue
		}
		if name, _ := rm["name"].(string); name != "" {
			cat.names[key] = name
		}
		if w, ok := rm["contextWindow"].(float64); ok && w > 0 {
			cat.windows[key] = int(w)
		} else if w, ok := rm["contextTokens"].(float64); ok && w > 0 {
			cat.windows[key] = int(w)
		}
	}
	// Session logs frequently carry bare model ids (e.g. "MiniMax-M3") while the
	// catalog is keyed by full id ("minimax/MiniMax-M3"). Add unambiguous bare-id
	// aliases so those still resolve to the catalog name/window.
	indexCatalogByBareID(cat.names)
	indexCatalogByBareID(cat.windows)
	return cat
}

// indexCatalogByBareID adds bare-id keys (the segment after the last "/") to a
// catalog map. A bare id is added only when unambiguous: it is not already a
// full key, and every full key sharing that bare id maps to the same value.
// Ambiguous bare ids (same suffix, different value across providers) are skipped
// so a wrong name is never shown.
func indexCatalogByBareID[T comparable](m map[string]T) {
	type entry struct {
		val      T
		conflict bool
	}
	bare := map[string]entry{}
	for k, v := range m {
		i := strings.LastIndex(k, "/")
		if i < 0 {
			continue // already bare
		}
		b := k[i+1:]
		if _, isFullKey := m[b]; isFullKey {
			continue // a full key already owns this string
		}
		if e, ok := bare[b]; ok {
			if e.val != v {
				e.conflict = true
				bare[b] = e
			}
		} else {
			bare[b] = entry{val: v}
		}
	}
	for b, e := range bare {
		if !e.conflict {
			m[b] = e.val
		}
	}
}

// modelCatalogCache caches the live model display-name map under a TTL with a
// singleflight refresh, mirroring liveSessionModelCache. Collaborators are
// injectable so tests run without real shell-out.
type modelCatalogCache struct {
	mu         sync.Mutex
	cond       *sync.Cond
	expiresAt  time.Time
	catalog    modelCatalog
	refreshing bool

	resolveOpenclaw func() string
	runner          func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// clone returns a deep copy of the catalog so cached state is never shared with
// callers (who may publish/mutate snapshots).
func (c modelCatalog) clone() modelCatalog {
	return modelCatalog{names: maps.Clone(c.names), windows: maps.Clone(c.windows)}
}

func newModelCatalogCache() *modelCatalogCache {
	c := &modelCatalogCache{}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// defaultModelCatalogCache is the singleton refreshed during collection.
var defaultModelCatalogCache = newModelCatalogCache()

// fetch returns the cached catalog or refreshes it under the TTL.
func (c *modelCatalogCache) fetch(ctx context.Context, now time.Time, ttl time.Duration) modelCatalog {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	c.mu.Lock()
	if c.cond == nil {
		c.cond = sync.NewCond(&c.mu)
	}
	for {
		if now.Before(c.expiresAt) {
			cat := c.catalog.clone()
			c.mu.Unlock()
			return cat
		}
		if !c.refreshing {
			c.refreshing = true
			break
		}
		c.cond.Wait()
	}
	c.mu.Unlock()
	return c.refreshAndStore(ctx, now, ttl)
}

func (c *modelCatalogCache) refreshAndStore(ctx context.Context, now time.Time, ttl time.Duration) (cached modelCatalog) {
	defer func() {
		c.mu.Lock()
		c.refreshing = false
		c.cond.Broadcast()
		c.mu.Unlock()
	}()

	cat := c.callFetch(ctx)

	c.mu.Lock()
	c.catalog = cat.clone()
	c.expiresAt = now.Add(ttl)
	cached = c.catalog.clone()
	c.mu.Unlock()
	return cached
}

func (c *modelCatalogCache) callFetch(ctx context.Context) modelCatalog {
	runner := c.runner
	if runner == nil {
		runner = execCommandContext
	}
	resolve := c.resolveOpenclaw
	if resolve == nil {
		resolve = resolveOpenclawBin
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := boundedOutput(runner(ctx, resolve(), "models", "list", "--json"), maxCLIOutputBytes)
	if err != nil {
		slog.Warn("[dashboard] modelCatalogCache: command failed", "error", err)
		return modelCatalog{names: map[string]string{}, windows: map[string]int{}}
	}
	return parseModelCatalog(out)
}

// refreshModelCatalog refreshes the catalog and publishes the name + context
// window snapshots for ModelName and lookupModelLimits. Best-effort: a fetch
// that yields nothing leaves the previous snapshots in place.
func refreshModelCatalog(ctx context.Context, now time.Time, ttl time.Duration) {
	cat := defaultModelCatalogCache.fetch(ctx, now, ttl)
	if len(cat.names) > 0 {
		setModelCatalogNames(cat.names)
	}
	if len(cat.windows) > 0 {
		setModelCatalogWindows(cat.windows)
	}
}
