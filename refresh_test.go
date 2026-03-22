package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCollectTokenUsage_UnknownSessionIsNotSubagent(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "agents")
	sessionDir := filepath.Join(basePath, "main", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	jsonl := `{"timestamp":"2026-03-22T10:00:00Z","message":{"role":"assistant","model":"openai/gpt-5","usage":{"totalTokens":100,"input":60,"output":40,"cacheRead":0,"cost":{"total":0.12}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(sessionDir, "orphan.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatal(err)
	}

	modelsAll := map[string]*tokenBucket{}
	modelsToday := map[string]*tokenBucket{}
	models7d := map[string]*tokenBucket{}
	models30d := map[string]*tokenBucket{}
	subagentAll := map[string]*tokenBucket{}
	subagentToday := map[string]*tokenBucket{}
	subagent7d := map[string]*tokenBucket{}
	subagent30d := map[string]*tokenBucket{}
	dailyCosts := map[string]map[string]float64{}
	dailyTokens := map[string]map[string]int{}
	dailyCalls := map[string]map[string]int{}
	dailySubagentCosts := map[string]float64{}
	dailySubagentCount := map[string]int{}

	runs := collectTokenUsage(
		basePath,
		time.UTC,
		"2026-03-22",
		"2026-03-15",
		"2026-02-20",
		map[string]string{},
		map[string]string{},
		map[string]string{},
		modelsAll, modelsToday, models7d, models30d,
		subagentAll, subagentToday, subagent7d, subagent30d,
		dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
	)

	if len(runs) != 0 {
		t.Fatalf("expected no subagent runs for unknown session, got %+v", runs)
	}
	if len(subagentAll) != 0 {
		t.Fatalf("expected unknown session not to count toward subagent usage, got %+v", subagentAll)
	}
	if len(modelsAll) == 0 {
		t.Fatal("expected usage to still count toward overall model usage")
	}
}

func TestCollectTokenUsage_ExplicitSubagentSessionCountsAsSubagent(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "agents")
	sessionDir := filepath.Join(basePath, "main", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	jsonl := `{"timestamp":"2026-03-22T10:00:00Z","message":{"role":"assistant","model":"openai/gpt-5","usage":{"totalTokens":100,"input":60,"output":40,"cacheRead":0,"cost":{"total":0.12}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(sessionDir, "sub1.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatal(err)
	}

	modelsAll := map[string]*tokenBucket{}
	modelsToday := map[string]*tokenBucket{}
	models7d := map[string]*tokenBucket{}
	models30d := map[string]*tokenBucket{}
	subagentAll := map[string]*tokenBucket{}
	subagentToday := map[string]*tokenBucket{}
	subagent7d := map[string]*tokenBucket{}
	subagent30d := map[string]*tokenBucket{}
	dailyCosts := map[string]map[string]float64{}
	dailyTokens := map[string]map[string]int{}
	dailyCalls := map[string]map[string]int{}
	dailySubagentCosts := map[string]float64{}
	dailySubagentCount := map[string]int{}

	runs := collectTokenUsage(
		basePath,
		time.UTC,
		"2026-03-22",
		"2026-03-15",
		"2026-02-20",
		map[string]string{"sub1": "subagent"},
		map[string]string{"sub1": "agent:main:subagent:abc"},
		map[string]string{},
		modelsAll, modelsToday, models7d, models30d,
		subagentAll, subagentToday, subagent7d, subagent30d,
		dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
	)

	if len(runs) != 1 {
		t.Fatalf("expected one subagent run, got %+v", runs)
	}
	if len(subagentAll) == 0 {
		t.Fatal("expected explicit subagent session to count toward subagent usage")
	}
}
