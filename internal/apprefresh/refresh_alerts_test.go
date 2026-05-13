package apprefresh

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildAlerts_HighCost(t *testing.T) {
	alerts := BuildAlerts(75, 50, 20, nil, nil, 80, map[string]any{"status": "online"}, 1<<30)
	if len(alerts) != 1 || alerts[0]["severity"] != "high" {
		t.Fatalf("expected one high-severity alert, got %+v", alerts)
	}
}

func TestBuildAlerts_WarnCostBoundary(t *testing.T) {
	// totalCostToday == costWarn should NOT alert; current code uses strict >.
	alerts := BuildAlerts(20, 50, 20, nil, nil, 80, map[string]any{"status": "online"}, 1<<30)
	if len(alerts) != 0 {
		t.Fatalf("boundary value should not alert, got %+v", alerts)
	}
	// just-above
	alerts = BuildAlerts(20.01, 50, 20, nil, nil, 80, map[string]any{"status": "online"}, 1<<30)
	if len(alerts) != 1 || alerts[0]["severity"] != "medium" {
		t.Fatalf("expected medium alert, got %+v", alerts)
	}
}

func TestBuildAlerts_CronFailure(t *testing.T) {
	crons := []map[string]any{
		{"name": "daily-rollup", "lastStatus": "error"},
		{"name": "ok-cron", "lastStatus": "ok"},
	}
	alerts := BuildAlerts(0, 50, 20, crons, nil, 80, map[string]any{"status": "online"}, 1<<30)
	if len(alerts) != 1 || alerts[0]["severity"] != "high" {
		t.Fatalf("expected one cron alert, got %+v", alerts)
	}
}

func TestBuildAlerts_StatusComparisonIsCaseInsensitive(t *testing.T) {
	// Mixed-case "Error" and "OFFLINE" must still produce alerts.
	crons := []map[string]any{{"name": "x", "lastStatus": "Error"}}
	alerts := BuildAlerts(0, 50, 20, crons, nil, 80, map[string]any{"status": "OFFLINE"}, 1<<30)
	if len(alerts) != 2 {
		t.Fatalf("want 2 alerts (cron + gateway), got %d: %+v", len(alerts), alerts)
	}
}

func TestBuildAlerts_HighContext(t *testing.T) {
	sessions := []map[string]any{
		{"name": "noisy", "contextPct": 85.0},
		{"name": "quiet", "contextPct": 10.0},
	}
	alerts := BuildAlerts(0, 50, 20, nil, sessions, 80, map[string]any{"status": "online"}, 1<<30)
	if len(alerts) != 1 || alerts[0]["severity"] != "medium" {
		t.Fatalf("expected one medium context alert, got %+v", alerts)
	}
}

func TestBuildAlerts_GatewayOffline(t *testing.T) {
	alerts := BuildAlerts(0, 50, 20, nil, nil, 80, map[string]any{"status": "offline"}, 1<<30)
	if len(alerts) != 1 || alerts[0]["severity"] != "critical" {
		t.Fatalf("expected critical alert, got %+v", alerts)
	}
}

func TestBuildAlerts_HighMemory(t *testing.T) {
	gw := map[string]any{"status": "online", "rss": 2 * 1024 * 1024, "memory": "2.0 GB"}
	alerts := BuildAlerts(0, 50, 20, nil, nil, 80, gw, float64(1024*1024))
	if len(alerts) != 1 || alerts[0]["severity"] != "medium" {
		t.Fatalf("expected one memory alert, got %+v", alerts)
	}
}

func TestBuildAlerts_AggregatesMultiple(t *testing.T) {
	crons := []map[string]any{{"name": "x", "lastStatus": "error"}}
	gw := map[string]any{"status": "offline", "rss": 2 * 1024 * 1024, "memory": "2.0 GB"}
	alerts := BuildAlerts(75, 50, 20, crons, nil, 80, gw, float64(1024*1024))
	// high cost + cron + offline + memory = 4
	if len(alerts) != 4 {
		t.Fatalf("expected 4 alerts, got %d: %+v", len(alerts), alerts)
	}
}

func TestBuildCostBreakdown_SortedDesc(t *testing.T) {
	m := map[string]*TokenBucket{
		"cheap":   {Cost: 0.5},
		"big":     {Cost: 12.34},
		"medium":  {Cost: 5.0},
		"nothing": {Cost: 0}, // filtered out
	}
	out := BuildCostBreakdown(m)
	if len(out) != 3 {
		t.Fatalf("want 3 entries, got %d", len(out))
	}
	if out[0]["model"] != "big" || out[1]["model"] != "medium" || out[2]["model"] != "cheap" {
		t.Fatalf("wrong order: %+v", out)
	}
	if out[0]["cost"] != 12.34 {
		t.Fatalf("cost should be rounded to 2dp, got %v", out[0]["cost"])
	}
}

func TestFilterByDate(t *testing.T) {
	runs := []map[string]any{
		{"date": "2026-05-01"},
		{"date": "2026-05-03"},
		{"date": "2026-05-05"},
	}
	if got := FilterByDate(runs, "2026-05-03", "=="); len(got) != 1 {
		t.Errorf("== match: want 1, got %d", len(got))
	}
	if got := FilterByDate(runs, "2026-05-03", ">="); len(got) != 2 {
		t.Errorf(">= match: want 2, got %d", len(got))
	}
	if got := FilterByDate(runs, "2026-05-03", "<"); len(got) != 0 {
		t.Errorf("unknown op should return empty, got %v", got)
	}
}

func TestLimitSlice(t *testing.T) {
	in := []int{1, 2, 3, 4, 5}
	if got := LimitSlice(in, 3); len(got) != 3 || got[2] != 3 {
		t.Errorf("want first 3, got %v", got)
	}
	if got := LimitSlice(in, 10); len(got) != 5 {
		t.Errorf("oversize limit should return all, got %v", got)
	}
}

// BuildDailyChart frozen-merge contract:
// - if frozen > current → frozen values replace current (total, tokens, subagentRuns, subagentCost, models)
// - if frozen ≤ current → current preserved, frozen's other fields ignored
func TestBuildDailyChart_FrozenMergeContract(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	// Target a date that will appear in the chart (today)
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	dailyCosts := map[string]map[string]float64{
		today:     {"GPT-5": 5.0},
		yesterday: {"GPT-5": 10.0},
	}
	dailyTokens := map[string]map[string]int{today: {"GPT-5": 100}, yesterday: {"GPT-5": 200}}
	dailyCalls := map[string]map[string]int{today: {"GPT-5": 5}, yesterday: {"GPT-5": 10}}
	dailySubagentCosts := map[string]float64{today: 1.0, yesterday: 2.0}
	dailySubagentCount := map[string]int{today: 3, yesterday: 4}

	// Frozen says: today should be 20.0 (replaces 5.0), yesterday 5.0 (less than 10 → ignored)
	frozen := map[string]map[string]any{
		today: {
			"total":        20.0,
			"tokens":       float64(999),
			"subagentRuns": float64(7),
			"subagentCost": 9.0,
			"models":       map[string]any{"Frozen-Model": 20.0},
		},
		yesterday: {
			"total":  5.0, // less than current 10 → no merge
			"tokens": float64(1),
		},
	}
	frozenBytes, _ := json.Marshal(frozen)
	if err := os.WriteFile(filepath.Join(dir, "frozen-daily.json"), frozenBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	chart := BuildDailyChart(now, dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount, dir)

	var todayEntry, yesterdayEntry map[string]any
	for _, e := range chart {
		switch e["date"] {
		case today:
			todayEntry = e
		case yesterday:
			yesterdayEntry = e
		}
	}
	if todayEntry == nil || yesterdayEntry == nil {
		t.Fatalf("missing entries: today=%v yesterday=%v", todayEntry, yesterdayEntry)
	}

	// Today: frozen wins, all fields replaced
	if todayEntry["total"] != 20.0 {
		t.Errorf("today total: want 20.0 (frozen wins), got %v", todayEntry["total"])
	}
	if todayEntry["tokens"] != 999 {
		t.Errorf("today tokens: want 999 (frozen), got %v", todayEntry["tokens"])
	}
	if todayEntry["subagentRuns"] != 7 {
		t.Errorf("today subagentRuns: want 7 (frozen), got %v", todayEntry["subagentRuns"])
	}
	if m, ok := todayEntry["models"].(map[string]any); !ok || m["Frozen-Model"] != 20.0 {
		t.Errorf("today models: want frozen Frozen-Model=20, got %v", todayEntry["models"])
	}

	// Yesterday: current wins; frozen tokens/subagent fields must be ignored
	if yesterdayEntry["total"] != 10.0 {
		t.Errorf("yesterday total: want 10.0 (current wins), got %v", yesterdayEntry["total"])
	}
	if yesterdayEntry["tokens"] != 200 {
		t.Errorf("yesterday tokens: want 200 (current), got %v", yesterdayEntry["tokens"])
	}
}
