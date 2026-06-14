package apprefresh

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// modelCatalogNames is the package-level snapshot of the live model id → display
// name map. ModelName reads it synchronously (it is pure and ctx-free); the
// catalog cache publishes into it during collection. A nil snapshot means "no
// catalog yet" and ModelName relies entirely on its hardcoded switch.
var modelCatalogNames atomic.Pointer[map[string]string]

// setModelCatalogNames publishes a new catalog snapshot for ModelName to read.
func setModelCatalogNames(m map[string]string) { modelCatalogNames.Store(&m) }

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

// parseModelCatalog parses `openclaw models list --json` output into a
// id(key) → display name map. It accepts both the envelope form
// {count, models:[…]} and a bare array, and ignores rows without a usable name.
func parseModelCatalog(out []byte) map[string]string {
	names := map[string]string{}
	var rows []any
	var envelope struct {
		Models []any `json:"models"`
	}
	if err := json.Unmarshal(out, &envelope); err == nil && envelope.Models != nil {
		rows = envelope.Models
	} else if err := json.Unmarshal(out, &rows); err != nil {
		return names
	}
	for _, r := range rows {
		rm := asObj(r)
		if rm == nil {
			continue
		}
		key, _ := rm["key"].(string)
		name, _ := rm["name"].(string)
		if key != "" && name != "" {
			names[key] = name
		}
	}
	return names
}

// modelCatalogCache caches the live model display-name map under a TTL with a
// singleflight refresh, mirroring liveSessionModelCache. Collaborators are
// injectable so tests run without real shell-out.
type modelCatalogCache struct {
	mu         sync.Mutex
	cond       *sync.Cond
	expiresAt  time.Time
	names      map[string]string
	refreshing bool

	resolveOpenclaw func() string
	runner          func(ctx context.Context, name string, args ...string) *exec.Cmd
}

func newModelCatalogCache() *modelCatalogCache {
	c := &modelCatalogCache{}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// defaultModelCatalogCache is the singleton refreshed during collection.
var defaultModelCatalogCache = newModelCatalogCache()

// fetch returns the cached catalog or refreshes it under the TTL.
func (c *modelCatalogCache) fetch(ctx context.Context, now time.Time, ttl time.Duration) map[string]string {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	c.mu.Lock()
	if c.cond == nil {
		c.cond = sync.NewCond(&c.mu)
	}
	for {
		if now.Before(c.expiresAt) {
			names := maps.Clone(c.names)
			c.mu.Unlock()
			return names
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

func (c *modelCatalogCache) refreshAndStore(ctx context.Context, now time.Time, ttl time.Duration) (cached map[string]string) {
	defer func() {
		c.mu.Lock()
		c.refreshing = false
		c.cond.Broadcast()
		c.mu.Unlock()
	}()

	names := c.callFetch(ctx)

	c.mu.Lock()
	c.names = maps.Clone(names)
	c.expiresAt = now.Add(ttl)
	cached = maps.Clone(c.names)
	c.mu.Unlock()
	return cached
}

func (c *modelCatalogCache) callFetch(ctx context.Context) map[string]string {
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
	out, err := runner(ctx, resolve(), "models", "list", "--json").Output()
	if err != nil {
		slog.Warn("[dashboard] modelCatalogCache: command failed", "error", err)
		return map[string]string{}
	}
	return parseModelCatalog(out)
}

// refreshModelCatalog refreshes the catalog and publishes the snapshot for
// ModelName. Best-effort: a failed fetch leaves the previous snapshot in place.
func refreshModelCatalog(ctx context.Context, now time.Time, ttl time.Duration) {
	names := defaultModelCatalogCache.fetch(ctx, now, ttl)
	if len(names) > 0 {
		setModelCatalogNames(names)
	}
}
