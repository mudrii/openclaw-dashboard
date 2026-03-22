package apprefresh

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCollectTokenUsageWithCache_HandlesLargeJSONLLine(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "agents")
	sessionDir := filepath.Join(basePath, "main", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	large := map[string]any{
		"timestamp": "2026-03-22T10:00:00Z",
		"message": map[string]any{
			"role":  "assistant",
			"model": "openai/gpt-5",
			"usage": map[string]any{
				"totalTokens": 100.0,
				"input":       60.0,
				"output":      40.0,
				"cacheRead":   0.0,
				"cost":        map[string]any{"total": 0.12},
			},
			"content": strings.Repeat("x", 2*1024*1024+512),
		},
	}
	data, err := json.Marshal(large)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(sessionDir, "big.jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	modelsAll := map[string]*TokenBucket{}
	modelsToday := map[string]*TokenBucket{}
	models7d := map[string]*TokenBucket{}
	models30d := map[string]*TokenBucket{}
	subagentAll := map[string]*TokenBucket{}
	subagentToday := map[string]*TokenBucket{}
	subagent7d := map[string]*TokenBucket{}
	subagent30d := map[string]*TokenBucket{}
	dailyCosts := map[string]map[string]float64{}
	dailyTokens := map[string]map[string]int{}
	dailyCalls := map[string]map[string]int{}
	dailySubagentCosts := map[string]float64{}
	dailySubagentCount := map[string]int{}

	CollectTokenUsageWithCache(
		filepath.Join(t.TempDir(), "token-cache.json"),
		basePath, time.UTC, "2026-03-22", "2026-03-15", "2026-02-20",
		map[string]string{}, map[string]string{}, map[string]string{},
		modelsAll, modelsToday, models7d, models30d,
		subagentAll, subagentToday, subagent7d, subagent30d,
		dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
	)

	got := modelsAll["GPT-5"]
	if got == nil || got.Total != 100 {
		t.Fatalf("expected oversized JSONL line to be counted, got %+v", got)
	}
}

func TestCollectTokenUsageWithCache_ReusesUnchangedFileSummary(t *testing.T) {
	tmp := t.TempDir()
	basePath := filepath.Join(tmp, "agents")
	sessionDir := filepath.Join(basePath, "main", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	line := `{"timestamp":"2026-03-22T10:00:00Z","message":{"role":"assistant","model":"openai/gpt-5","usage":{"totalTokens":100,"input":60,"output":40,"cacheRead":0,"cost":{"total":0.12}}}}` + "\n"
	filePath := filepath.Join(sessionDir, "cached.jsonl")
	if err := os.WriteFile(filePath, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(tmp, "token-cache.json")

	run := func() map[string]*TokenBucket {
		modelsAll := map[string]*TokenBucket{}
		modelsToday := map[string]*TokenBucket{}
		models7d := map[string]*TokenBucket{}
		models30d := map[string]*TokenBucket{}
		subagentAll := map[string]*TokenBucket{}
		subagentToday := map[string]*TokenBucket{}
		subagent7d := map[string]*TokenBucket{}
		subagent30d := map[string]*TokenBucket{}
		dailyCosts := map[string]map[string]float64{}
		dailyTokens := map[string]map[string]int{}
		dailyCalls := map[string]map[string]int{}
		dailySubagentCosts := map[string]float64{}
		dailySubagentCount := map[string]int{}

		CollectTokenUsageWithCache(
			cachePath,
			basePath, time.UTC, "2026-03-22", "2026-03-15", "2026-02-20",
			map[string]string{}, map[string]string{}, map[string]string{},
			modelsAll, modelsToday, models7d, models30d,
			subagentAll, subagentToday, subagent7d, subagent30d,
			dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
		)
		return modelsAll
	}

	first := run()
	if first["GPT-5"] == nil || first["GPT-5"].Total != 100 {
		t.Fatalf("expected initial parse to count tokens, got %+v", first["GPT-5"])
	}

	if err := os.Chmod(filePath, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filePath, 0o644)

	second := run()
	if second["GPT-5"] == nil || second["GPT-5"].Total != 100 {
		t.Fatalf("expected cached summary to be reused, got %+v", second["GPT-5"])
	}
}

func TestCollectSessions_CachesLiveModelLookup(t *testing.T) {
	prevFetcher := fetchLiveSessionModels
	defer func() { fetchLiveSessionModels = prevFetcher }()
	sessionModelCache = liveSessionModelCache{}

	calls := 0
	fetchLiveSessionModels = func() map[string]string {
		calls++
		return map[string]string{"agent:main:chat": "openai/gpt-5"}
	}

	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	modelAliases := map[string]string{"openai/gpt-5": "GPT-5"}
	stores := []SessionStoreFile{{
		AgentName: "main",
		Store: map[string]map[string]any{
			"agent:main:chat": {
				"sessionId":     "sid-1",
				"updatedAt":     float64(now.UnixMilli()),
				"contextTokens": 1000.0,
				"totalTokens":   100.0,
			},
		},
	}}

	knownA := map[string]string{}
	gotA := collectSessions(stores, t.TempDir(), time.UTC, now, "2026-03-23", modelAliases, knownA, nil, 30*time.Second)
	knownB := map[string]string{}
	gotB := collectSessions(stores, t.TempDir(), time.UTC, now.Add(5*time.Second), "2026-03-23", modelAliases, knownB, nil, 30*time.Second)

	if calls != 1 {
		t.Fatalf("expected one live model fetch within TTL, got %d", calls)
	}
	if len(gotA) != 1 || len(gotB) != 1 {
		t.Fatalf("expected sessions on both calls, got %d and %d", len(gotA), len(gotB))
	}
	if gotA[0]["model"] != "GPT-5" || gotB[0]["model"] != "GPT-5" {
		t.Fatalf("expected cached live model mapping to apply, got %v and %v", gotA[0]["model"], gotB[0]["model"])
	}
}
