package apprefresh

import (
	"context"
	"encoding/json"
	"log"
	"os/exec"
	"sync"
	"time"
)

var fetchLiveSessionModels = fetchLiveSessionModelsCLI

type liveSessionModelCache struct {
	mu        sync.Mutex
	expiresAt time.Time
	models    map[string]string
}

var sessionModelCache liveSessionModelCache

func getLiveSessionModels(now time.Time, ttl time.Duration) map[string]string {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	sessionModelCache.mu.Lock()
	if now.Before(sessionModelCache.expiresAt) {
		models := cloneStringMap(sessionModelCache.models)
		sessionModelCache.mu.Unlock()
		return models
	}
	sessionModelCache.mu.Unlock()

	models := fetchLiveSessionModels()

	sessionModelCache.mu.Lock()
	sessionModelCache.models = cloneStringMap(models)
	sessionModelCache.expiresAt = now.Add(ttl)
	cached := cloneStringMap(sessionModelCache.models)
	sessionModelCache.mu.Unlock()
	return cached
}

func fetchLiveSessionModelsCLI() map[string]string {
	models := map[string]string{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "openclaw", "sessions", "--json").Output()
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

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
