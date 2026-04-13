package apprefresh

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkParseTokenUsageFile(b *testing.B) {
	// Create a temp JSONL file with realistic token usage entries
	dir := b.TempDir()
	sessionDir := filepath.Join(dir, "agents", "main", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		b.Fatal(err)
	}
	jsonlPath := filepath.Join(sessionDir, "bench-session.jsonl")

	f, err := os.Create(jsonlPath)
	if err != nil {
		b.Fatal(err)
	}
	// Write 500 realistic JSONL entries
	for i := range 500 {
		ts := time.Now().Add(-time.Duration(i) * time.Minute).UTC().Format(time.RFC3339)
		entry := map[string]any{
			"timestamp": ts,
			"message": map[string]any{
				"role":  "assistant",
				"model": "anthropic/claude-opus-4-6",
				"usage": map[string]any{
					"input":       float64(1000 + i),
					"output":      float64(500 + i),
					"cacheRead":   float64(200 + i),
					"totalTokens": float64(1700 + 2*i),
					"cost": map[string]any{
						"total": 0.05 + float64(i)*0.001,
					},
				},
			},
		}
		data, _ := json.Marshal(entry)
		fmt.Fprintf(f, "%s\n", data)
	}
	_ = f.Close()

	info, err := os.Stat(jsonlPath)
	if err != nil {
		b.Fatal(err)
	}
	loc := time.UTC

	b.ResetTimer()
	for b.Loop() {
		_, err := parseTokenUsageFile(jsonlPath, info, loc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildDailyChart(b *testing.B) {
	now := time.Now()
	dailyCosts := map[string]map[string]float64{}
	dailyTokens := map[string]map[string]int{}
	dailyCalls := map[string]map[string]int{}
	dailySubagentCosts := map[string]float64{}
	dailySubagentCount := map[string]int{}

	models := []string{"Claude Opus 4.6", "Claude Sonnet", "Gemini 2.5 Pro", "GPT-5", "Kimi K2.5"}
	for i := range 30 {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		dailyCosts[d] = map[string]float64{}
		dailyTokens[d] = map[string]int{}
		dailyCalls[d] = map[string]int{}
		for _, m := range models {
			dailyCosts[d][m] = float64(i+1) * 0.5
			dailyTokens[d][m] = (i + 1) * 10000
			dailyCalls[d][m] = (i + 1) * 10
		}
		dailySubagentCosts[d] = float64(i) * 0.1
		dailySubagentCount[d] = i
	}

	dir := b.TempDir()
	b.ResetTimer()
	for b.Loop() {
		BuildDailyChart(now, dailyCosts, dailyTokens, dailyCalls,
			dailySubagentCosts, dailySubagentCount, dir)
	}
}

func BenchmarkBuildCostBreakdown(b *testing.B) {
	m := map[string]*TokenBucket{}
	models := []string{"Claude Opus 4.6", "Claude Sonnet", "Gemini 2.5 Pro", "GPT-5", "Kimi K2.5", "Grok 4", "O3", "GLM-5"}
	for i, name := range models {
		m[name] = &TokenBucket{
			Calls:  100 + i*10,
			Input:  50000 + i*5000,
			Output: 20000 + i*2000,
			Total:  70000 + i*7000,
			Cost:   float64(10+i) * 1.5,
		}
	}

	b.ResetTimer()
	for b.Loop() {
		BuildCostBreakdown(m)
	}
}

func BenchmarkBucketsToList(b *testing.B) {
	m := map[string]*TokenBucket{}
	for i := range 10 {
		m[fmt.Sprintf("model-%d", i)] = &TokenBucket{
			Calls:     100 + i*10,
			Input:     50000 + i*5000,
			Output:    20000 + i*2000,
			CacheRead: 10000 + i*1000,
			Total:     80000 + i*8000,
			Cost:      float64(10+i) * 1.5,
		}
	}

	b.ResetTimer()
	for b.Loop() {
		BucketsToList(m)
	}
}

func BenchmarkModelName(b *testing.B) {
	models := []string{
		"anthropic/claude-opus-4-6",
		"anthropic/claude-sonnet-4-6",
		"google/gemini-2.5-pro",
		"openai/gpt-5",
		"kimi-coding/k2p5",
		"xai/grok-4-fast",
		"unknown-provider/unknown-model",
	}
	b.ResetTimer()
	for b.Loop() {
		for _, m := range models {
			ModelName(m)
		}
	}
}
