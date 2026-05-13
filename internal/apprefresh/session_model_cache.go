package apprefresh

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"os/exec"
	"sync"
	"time"

	appsystem "github.com/mudrii/openclaw-dashboard/internal/appsystem"
)

// liveSessionModelCache caches the live model→key map fetched from the openclaw
// CLI under a TTL and prevents thundering-herd refresh via a singleflight
// pattern (one inflight refresh; concurrent callers wait or return stale).
//
// All collaborators (binary path resolver, CLI fetcher, command runner) are
// fields so tests can construct an isolated cache without touching globals —
// enables t.Parallel() in test packages that previously had to serialize.
type liveSessionModelCache struct {
	mu         sync.Mutex
	cond       *sync.Cond
	expiresAt  time.Time
	models     map[string]string
	refreshing bool

	// Injectable seams. nil → use package-level defaults.
	fetchFn         func(ctx context.Context, runner func(ctx context.Context, name string, args ...string) *exec.Cmd, bin string) map[string]string
	resolveOpenclaw func() string
	runner          func(ctx context.Context, name string, args ...string) *exec.Cmd
}

func newLiveSessionModelCache() *liveSessionModelCache {
	c := &liveSessionModelCache{}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// defaultSessionModelCache is the singleton used by collectSessions. Tests that
// need isolation construct their own via newLiveSessionModelCache.
var defaultSessionModelCache = newLiveSessionModelCache()

// Package-level overrides kept for back-compat with existing tests that swap
// these. New tests should construct a liveSessionModelCache instead.
var fetchLiveSessionModels = fetchLiveSessionModelsCLI
var resolveOpenclawBin = appsystem.ResolveOpenclawBin
var execCommandContext = exec.CommandContext

// fetch returns the cached map or refreshes it. Uses the receiver's injected
// collaborators when set, otherwise falls back to package-level defaults.
func (c *liveSessionModelCache) fetch(ctx context.Context, now time.Time, ttl time.Duration) map[string]string {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	c.mu.Lock()
	if c.cond == nil {
		c.cond = sync.NewCond(&c.mu)
	}
	for {
		if now.Before(c.expiresAt) {
			models := maps.Clone(c.models)
			c.mu.Unlock()
			return models
		}
		if !c.refreshing {
			c.refreshing = true
			break
		}
		c.cond.Wait()
	}
	c.mu.Unlock()

	models := c.callFetch(ctx)

	c.mu.Lock()
	c.models = maps.Clone(models)
	c.expiresAt = now.Add(ttl)
	c.refreshing = false
	c.cond.Broadcast()
	cached := maps.Clone(c.models)
	c.mu.Unlock()
	return cached
}

// callFetch dispatches via injected fetch function (test seam) or the package-
// level default fetchLiveSessionModels.
func (c *liveSessionModelCache) callFetch(ctx context.Context) map[string]string {
	if c.fetchFn != nil {
		runner := c.runner
		if runner == nil {
			runner = execCommandContext
		}
		resolve := c.resolveOpenclaw
		if resolve == nil {
			resolve = resolveOpenclawBin
		}
		return c.fetchFn(ctx, runner, resolve())
	}
	return fetchLiveSessionModels(ctx)
}

// getLiveSessionModels is the package-level entry point used by collectSessions.
// Delegates to the default cache so existing call sites stay unchanged.
func getLiveSessionModels(ctx context.Context, now time.Time, ttl time.Duration) map[string]string {
	return defaultSessionModelCache.fetch(ctx, now, ttl)
}

// resetLiveSessionModelCacheForTest clears the default cache. Used by legacy
// tests; new tests should construct a private cache instead.
func resetLiveSessionModelCacheForTest() {
	defaultSessionModelCache.mu.Lock()
	defaultSessionModelCache.expiresAt = time.Time{}
	defaultSessionModelCache.models = nil
	defaultSessionModelCache.refreshing = false
	defaultSessionModelCache.cond = sync.NewCond(&defaultSessionModelCache.mu)
	defaultSessionModelCache.mu.Unlock()
}

func fetchLiveSessionModelsCLI(ctx context.Context) map[string]string {
	models := map[string]string{}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := execCommandContext(ctx, resolveOpenclawBin(), "sessions", "--json").Output()
	if err != nil {
		slog.Warn("[dashboard] fetchLiveSessionModelsCLI: command failed", "error", err)
		return models
	}
	if len(out) == 0 {
		return models
	}

	var sessions any
	if err := json.Unmarshal(out, &sessions); err != nil {
		slog.Warn("[dashboard] fetchLiveSessionModelsCLI: JSON parse failed", "error", err)
		return models
	}
	switch s := sessions.(type) {
	case []any:
		for _, gs := range s {
			gm := asObj(gs)
			key, _ := gm["key"].(string)
			model, _ := gm["model"].(string)
			if key != "" && model != "" {
				models[key] = model
			}
		}
	case map[string]any:
		if arr, ok := s["sessions"].([]any); ok {
			for _, gs := range arr {
				gm := asObj(gs)
				key, _ := gm["key"].(string)
				model, _ := gm["model"].(string)
				if key != "" && model != "" {
					models[key] = model
				}
			}
		}
	}
	return models
}
