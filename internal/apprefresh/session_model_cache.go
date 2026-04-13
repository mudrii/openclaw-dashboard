package apprefresh

import (
	"context"
	"encoding/json"
	"log"
	"maps"
	"os/exec"
	"sync"
	"time"

	appsystem "github.com/mudrii/openclaw-dashboard/internal/appsystem"
)

var fetchLiveSessionModels = fetchLiveSessionModelsCLI
var resolveOpenclawBin = appsystem.ResolveOpenclawBin
var execCommandContext = exec.CommandContext

type liveSessionModelCache struct {
	mu         sync.Mutex
	cond       *sync.Cond
	expiresAt  time.Time
	models     map[string]string
	refreshing bool
}

var sessionModelCache liveSessionModelCache

func getLiveSessionModels(now time.Time, ttl time.Duration) map[string]string {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	sessionModelCache.mu.Lock()
	if sessionModelCache.cond == nil {
		sessionModelCache.cond = sync.NewCond(&sessionModelCache.mu)
	}
	for {
		if now.Before(sessionModelCache.expiresAt) {
			models := maps.Clone(sessionModelCache.models)
			sessionModelCache.mu.Unlock()
			return models
		}
		if !sessionModelCache.refreshing {
			sessionModelCache.refreshing = true
			break
		}
		sessionModelCache.cond.Wait()
	}
	sessionModelCache.mu.Unlock()

	models := fetchLiveSessionModels()

	sessionModelCache.mu.Lock()
	sessionModelCache.models = maps.Clone(models)
	sessionModelCache.expiresAt = now.Add(ttl)
	sessionModelCache.refreshing = false
	sessionModelCache.cond.Broadcast()
	cached := maps.Clone(sessionModelCache.models)
	sessionModelCache.mu.Unlock()
	return cached
}

func resetLiveSessionModelCacheForTest() {
	sessionModelCache.mu.Lock()
	sessionModelCache.expiresAt = time.Time{}
	sessionModelCache.models = nil
	sessionModelCache.refreshing = false
	sessionModelCache.cond = nil
	sessionModelCache.mu.Unlock()
}

func fetchLiveSessionModelsCLI() map[string]string {
	models := map[string]string{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := execCommandContext(ctx, resolveOpenclawBin(), "sessions", "--json").Output()
	if err != nil {
		log.Printf("[dashboard] fetchLiveSessionModelsCLI: command failed: %v", err)
		return models
	}
	if len(out) == 0 {
		return models
	}

	var sessions any
	if err := json.Unmarshal(out, &sessions); err != nil {
		log.Printf("[dashboard] fetchLiveSessionModelsCLI: JSON parse failed: %v", err)
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
