package dashboard

import (
	"context"
	"encoding/json"
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

func TestCollectTokenUsage_ZeroCostSubagentStillCountsRun(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "agents")
	sessionDir := filepath.Join(basePath, "main", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	jsonl := `{"timestamp":"2026-03-22T10:00:00Z","message":{"role":"assistant","model":"openai/gpt-5","usage":{"totalTokens":100,"input":60,"output":40,"cacheRead":0,"cost":{"total":0}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(sessionDir, "sub-zero.jsonl"), []byte(jsonl), 0o644); err != nil {
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
		map[string]string{"sub-zero": "subagent"},
		map[string]string{"sub-zero": "agent:main:subagent:abc"},
		map[string]string{},
		modelsAll, modelsToday, models7d, models30d,
		subagentAll, subagentToday, subagent7d, subagent30d,
		dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
	)

	if len(runs) != 1 {
		t.Fatalf("expected one zero-cost subagent run, got %+v", runs)
	}
	if got := dailySubagentCount["2026-03-22"]; got != 1 {
		t.Fatalf("expected daily subagent count to include zero-cost run, got %d", got)
	}
}

// --------------- modelName ---------------

func TestModelName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"anthropic/claude-sonnet-4-20250514", "Claude Sonnet"},
		{"openai/gpt-5", "GPT-5"},
		{"openai/gpt-4o-2024-05-13", "GPT-4o"},
		{"anthropic/claude-opus-4-6-20260312", "Claude Opus 4.6"},
		{"anthropic/claude-opus-4-20250108", "Claude Opus 4.5"},
		{"claude-haiku-3", "Claude Haiku"},
		{"google/gemini-2.5-pro-preview", "Gemini 2.5 Pro"},
		{"google/gemini-2.5-flash-preview", "Gemini 2.5 Flash"},
		{"google/gemini-3-flash-preview", "Gemini 3 Flash"},
		{"xai/grok-4-fast", "Grok 4 Fast"},
		{"xai/grok-4", "Grok 4"},
		{"openai/o3-mini", "O3"},
		{"openai/o1-preview", "O1"},
		{"minimax-m2.5-chat", "MiniMax M2.5"},
		{"minimax-m2-chat", "MiniMax"},
		{"glm-5-plus", "GLM-5"},
		{"glm-4-plus", "GLM-4"},
		{"k2p5-chat", "Kimi K2.5"},
		{"gpt-5.3-codex", "GPT-5.3 Codex"},
		{"totally-unknown-model", "totally-unknown-model"},
	}
	for _, tc := range tests {
		if got := modelName(tc.input); got != tc.want {
			t.Errorf("modelName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --------------- fmtTokens ---------------

func TestFmtTokens(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{999_999, "1000.0K"},
		{1_000_000, "1.0M"},
		{2_500_000, "2.5M"},
	}
	for _, tc := range tests {
		if got := fmtTokens(tc.n); got != tc.want {
			t.Errorf("fmtTokens(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// --------------- titleCase ---------------

func TestTitleCase(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"", ""},
		{"hello", "Hello"},
		{"Hello", "Hello"},
		{"openai", "Openai"},
		{"123abc", "123abc"},
	}
	for _, tc := range tests {
		if got := titleCase(tc.input); got != tc.want {
			t.Errorf("titleCase(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --------------- filterByDate ---------------

func TestFilterByDate(t *testing.T) {
	runs := []map[string]any{
		{"date": "2026-03-20", "id": 1},
		{"date": "2026-03-21", "id": 2},
		{"date": "2026-03-22", "id": 3},
	}

	eq := filterByDate(runs, "2026-03-21", "==")
	if len(eq) != 1 || eq[0]["id"] != 2 {
		t.Fatalf("filterByDate == got %+v", eq)
	}

	gte := filterByDate(runs, "2026-03-21", ">=")
	if len(gte) != 2 {
		t.Fatalf("filterByDate >= expected 2 results, got %d", len(gte))
	}

	none := filterByDate(runs, "2026-03-25", "==")
	if len(none) != 0 {
		t.Fatalf("filterByDate no-match expected 0, got %d", len(none))
	}
}

// --------------- limitSlice ---------------

func TestLimitSlice(t *testing.T) {
	s := []int{1, 2, 3, 4, 5}
	if got := limitSlice(s, 3); len(got) != 3 {
		t.Fatalf("limitSlice(5, 3) = %d items", len(got))
	}
	if got := limitSlice(s, 10); len(got) != 5 {
		t.Fatalf("limitSlice(5, 10) = %d items", len(got))
	}
	if got := limitSlice([]int{}, 5); len(got) != 0 {
		t.Fatalf("limitSlice(0, 5) = %d items", len(got))
	}
}

// --------------- buildAlerts ---------------

func TestBuildAlerts_HighCost(t *testing.T) {
	alerts := buildAlerts(100, 50, 20, nil, nil, 80, map[string]any{"status": "online"}, 640*1024)
	found := false
	for _, a := range alerts {
		if a["severity"] == "high" && a["icon"] == "💰" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected high-cost alert")
	}
}

func TestBuildAlerts_WarnCost(t *testing.T) {
	alerts := buildAlerts(30, 50, 20, nil, nil, 80, map[string]any{"status": "online"}, 640*1024)
	found := false
	for _, a := range alerts {
		if a["severity"] == "medium" && a["icon"] == "💵" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected warn-cost alert")
	}
}

func TestBuildAlerts_NoCostAlert(t *testing.T) {
	alerts := buildAlerts(5, 50, 20, nil, nil, 80, map[string]any{"status": "online"}, 640*1024)
	for _, a := range alerts {
		if a["icon"] == "💰" || a["icon"] == "💵" {
			t.Fatalf("unexpected cost alert: %+v", a)
		}
	}
}

func TestBuildAlerts_CronFailure(t *testing.T) {
	crons := []map[string]any{{"name": "daily-report", "lastStatus": "error"}}
	alerts := buildAlerts(0, 50, 20, crons, nil, 80, map[string]any{"status": "online"}, 640*1024)
	found := false
	for _, a := range alerts {
		if a["icon"] == "❌" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected cron-failure alert")
	}
}

func TestBuildAlerts_HighContext(t *testing.T) {
	sessions := []map[string]any{{"name": "test-session", "contextPct": 95.0}}
	alerts := buildAlerts(0, 50, 20, nil, sessions, 80, map[string]any{"status": "online"}, 640*1024)
	found := false
	for _, a := range alerts {
		if a["icon"] == "⚠️" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected high-context alert")
	}
}

func TestBuildAlerts_GatewayOffline(t *testing.T) {
	alerts := buildAlerts(0, 50, 20, nil, nil, 80, map[string]any{"status": "offline"}, 640*1024)
	found := false
	for _, a := range alerts {
		if a["severity"] == "critical" && a["icon"] == "🔴" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected gateway-offline alert")
	}
}

func TestBuildAlerts_HighMemory(t *testing.T) {
	gw := map[string]any{"status": "online", "rss": 700 * 1024, "memory": "700 MB"}
	alerts := buildAlerts(0, 50, 20, nil, nil, 80, gw, 640*1024)
	found := false
	for _, a := range alerts {
		if a["icon"] == "🧠" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected high-memory alert")
	}
}

// --------------- collectCrons ---------------

func TestCollectCrons_ParsesJobs(t *testing.T) {
	dir := t.TempDir()
	cronPath := filepath.Join(dir, "jobs.json")

	data, err := os.ReadFile(filepath.Join("testdata", "cron", "jobs.every.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cronPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	crons := collectCrons(cronPath, time.UTC)
	if len(crons) != 1 {
		t.Fatalf("expected 1 cron, got %d", len(crons))
	}
	if crons[0]["name"] != "daily-report" {
		t.Fatalf("name = %v", crons[0]["name"])
	}
	if crons[0]["schedule"] != "Every 1h" {
		t.Fatalf("schedule = %v", crons[0]["schedule"])
	}
	if crons[0]["enabled"] != true {
		t.Fatalf("enabled = %v", crons[0]["enabled"])
	}
}

func TestCollectCrons_ScheduleKinds(t *testing.T) {
	makeJob := func(sched map[string]any) map[string]any {
		return map[string]any{
			"name": "test", "enabled": true,
			"schedule": sched,
			"state":    map[string]any{},
			"payload":  map[string]any{},
		}
	}

	tests := []struct {
		sched map[string]any
		want  string
	}{
		{map[string]any{"kind": "cron", "expr": "0 */6 * * *"}, "0 */6 * * *"},
		{map[string]any{"kind": "every", "everyMs": float64(86400000)}, "Every 1d"},
		{map[string]any{"kind": "every", "everyMs": float64(60000)}, "Every 1m"},
		{map[string]any{"kind": "every", "everyMs": float64(5000)}, "Every 5000ms"},
		{map[string]any{"kind": "at", "at": "2026-03-22T10:00"}, "2026-03-22T10:00"},
	}

	for i, tc := range tests {
		dir := t.TempDir()
		cronPath := filepath.Join(dir, "jobs.json")
		d, _ := json.Marshal(map[string]any{"jobs": []any{makeJob(tc.sched)}})
		os.WriteFile(cronPath, d, 0o644)
		crons := collectCrons(cronPath, time.UTC)
		if len(crons) != 1 {
			t.Fatalf("case %d: expected 1 cron, got %d", i, len(crons))
		}
		if crons[0]["schedule"] != tc.want {
			t.Errorf("case %d: schedule = %q, want %q", i, crons[0]["schedule"], tc.want)
		}
	}
}

func TestCollectCrons_MissingFile(t *testing.T) {
	crons := collectCrons("/nonexistent/path/jobs.json", time.UTC)
	if crons != nil {
		t.Fatalf("expected nil for missing file, got %+v", crons)
	}
}

func TestCollectCrons_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "jobs.json")
	os.WriteFile(p, []byte("not json"), 0o644)
	crons := collectCrons(p, time.UTC)
	if crons != nil {
		t.Fatalf("expected nil for invalid JSON, got %+v", crons)
	}
}

// --------------- buildCostBreakdown ---------------

func TestBuildCostBreakdown(t *testing.T) {
	m := map[string]*tokenBucket{
		"GPT-5":         {Calls: 10, Cost: 5.0},
		"Claude Sonnet": {Calls: 20, Cost: 12.0},
		"Haiku":         {Calls: 5, Cost: 0.50},
	}
	result := buildCostBreakdown(m)
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
	if result[0]["model"] != "Claude Sonnet" {
		t.Errorf("first entry should be Claude Sonnet (highest cost), got %v", result[0]["model"])
	}
	if result[2]["model"] != "Haiku" {
		t.Errorf("last entry should be Haiku (lowest cost), got %v", result[2]["model"])
	}
	if result[0]["cost"] != 12.0 {
		t.Errorf("first cost = %v, want 12.0", result[0]["cost"])
	}
}

func TestBuildCostBreakdown_Empty(t *testing.T) {
	result := buildCostBreakdown(map[string]*tokenBucket{})
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d entries", len(result))
	}
}

// --------------- bucketsToList ---------------

func TestBucketsToList_SortedByCostDesc(t *testing.T) {
	m := map[string]*tokenBucket{
		"cheap":  {Calls: 1, Input: 100, Output: 50, Total: 150, Cost: 0.01},
		"pricey": {Calls: 5, Input: 5000, Output: 2000, CacheRead: 1000, Total: 8000, Cost: 2.50},
	}
	list := bucketsToList(m)
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
	if list[0].Model != "pricey" {
		t.Errorf("first entry = %q, want pricey", list[0].Model)
	}
	if list[0].Input != "5.0K" {
		t.Errorf("input = %q, want 5.0K", list[0].Input)
	}
}

// --------------- trimLabel ---------------

func TestTrimLabel(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"", ""},
		{"hello", "hello"},
		{"user id: 12345", "user"},
		{"chat id:-67890", "chat"},
		{"  spaces  ", "spaces"},
	}
	for _, tc := range tests {
		if got := trimLabel(tc.input); got != tc.want {
			t.Errorf("trimLabel(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --------------- buildDailyChart ---------------

func TestBuildDailyChart_Returns30Days(t *testing.T) {
	now := time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)
	dailyCosts := map[string]map[string]float64{
		"2026-03-22": {"GPT-5": 1.5},
		"2026-03-21": {"Claude Sonnet": 2.0},
	}
	dailyTokens := map[string]map[string]int{
		"2026-03-22": {"GPT-5": 1000},
	}
	dailyCalls := map[string]map[string]int{
		"2026-03-22": {"GPT-5": 5},
	}
	dailySubagentCosts := map[string]float64{"2026-03-22": 0.5}
	dailySubagentCount := map[string]int{"2026-03-22": 2}

	chart := buildDailyChart(now, dailyCosts, dailyTokens, dailyCalls,
		dailySubagentCosts, dailySubagentCount, t.TempDir())
	if len(chart) != 30 {
		t.Fatalf("expected 30 days, got %d", len(chart))
	}
	last := chart[len(chart)-1]
	if last["date"] != "2026-03-22" {
		t.Errorf("last date = %v, want 2026-03-22", last["date"])
	}
	cost, _ := last["total"].(float64)
	if cost != 1.5 {
		t.Errorf("last total cost = %v, want 1.5", cost)
	}
}

// --------------- buildSIDToKeyMap ---------------

func TestBuildSIDToKeyMap(t *testing.T) {
	stores := []sessionStoreFile{
		{
			AgentName: "main",
			Store: map[string]map[string]any{
				"session-abc": {"type": "task", "sessionId": "sid1"},
				"session-def": {"type": "chat", "sessionId": "sid2"},
			},
		},
	}
	m := buildSIDToKeyMap(stores)
	if m["sid1"] != "session-abc" {
		t.Errorf("sid1 -> %q, want session-abc", m["sid1"])
	}
	if m["sid2"] != "session-def" {
		t.Errorf("sid2 -> %q, want session-def", m["sid2"])
	}
}

// --------------- runRefreshCollector with pre-loaded Config ---------------

func TestRunRefreshCollector_AcceptsConfig(t *testing.T) {
	dir := t.TempDir()
	openclawPath := t.TempDir()
	agentsDir := filepath.Join(openclawPath, "agents", "main", "sessions")
	os.MkdirAll(agentsDir, 0o755)

	cfg := Config{Timezone: "UTC"}
	err := runRefreshCollectorWithContext(context.Background(), dir, openclawPath, cfg)
	if err != nil {
		t.Fatalf("runRefreshCollector with config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "data.json")); err != nil {
		t.Fatalf("data.json not created: %v", err)
	}
}

// Ensure the old two-arg call still works (variadic cfg not provided).
func TestRunRefreshCollector_WithoutConfig(t *testing.T) {
	dir := t.TempDir()
	openclawPath := t.TempDir()
	agentsDir := filepath.Join(openclawPath, "agents", "main", "sessions")
	os.MkdirAll(agentsDir, 0o755)

	err := runRefreshCollectorWithContext(context.Background(), dir, openclawPath, loadConfig(dir))
	if err != nil {
		t.Fatalf("runRefreshCollector without config: %v", err)
	}
}
