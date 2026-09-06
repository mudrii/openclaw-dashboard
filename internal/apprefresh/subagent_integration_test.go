package apprefresh

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// TestCollectDashboardData_SubagentRunsFromCLI drives the migration's subagent
// path end-to-end through collectDashboardData: a stubbed `openclaw tasks list
// --json --runtime subagent` populates data.json's subagentRuns (sorted,
// windowed, projected) with NO cost field. Other CLI calls fail so only the
// subagent path is under test. Exercises the integration the unit test
// (collectSubagentRuns) cannot: the collector wiring + JSON projection.
func TestCollectDashboardData_SubagentRunsFromCLI(t *testing.T) {
	// These pre-migration fixtures must not inherit a selected modern runtime.
	t.Setenv("OPENCLAW_CONTAINER", "")
	t.Setenv("OPENCLAW_STATE_DIR", "")
	now := time.Now().UnixMilli()
	taskJSON := `{"tasks":[{"agentId":"main","task":"demo research task","status":"succeeded",` +
		`"createdAt":` + strconv.FormatInt(now, 10) + `,"startedAt":` + strconv.FormatInt(now, 10) +
		`,"endedAt":` + strconv.FormatInt(now+5000, 10) + `}]}`

	prev := execCommandContext
	t.Cleanup(func() { execCommandContext = prev })
	execCommandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		if strings.Contains(strings.Join(args, " "), "tasks list") {
			return exec.CommandContext(ctx, "printf", "%s", taskJSON)
		}
		return exec.CommandContext(ctx, "false") // every other openclaw CLI call fails
	}
	t.Cleanup(resetModelCatalogForTest)

	tmp := t.TempDir()
	openclaw := filepath.Join(tmp, "openclaw")
	for _, d := range []string{"cron", "agents"} {
		if err := os.MkdirAll(filepath.Join(openclaw, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(openclaw, "openclaw.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dash := filepath.Join(tmp, "dashboard")
	if err := os.MkdirAll(dash, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := appconfig.Default()
	cfg.Timezone = "UTC"
	cfg.AI.GatewayPort = 0

	data := collectDashboardData(context.Background(), dash, openclaw, cfg)
	runs, ok := data["subagentRuns"].([]map[string]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("subagentRuns = %v (%T), want 1 run from the CLI", data["subagentRuns"], data["subagentRuns"])
	}
	r := runs[0]
	if r["agent"] != "main" || r["status"] != "succeeded" {
		t.Errorf("run = %v, want agent=main status=succeeded", r)
	}
	if r["durationSec"] != 5 {
		t.Errorf("durationSec = %v, want 5", r["durationSec"])
	}
	if _, hasCost := r["cost"]; hasCost {
		t.Errorf("subagent run must not carry a cost field post-migration; got %v", r["cost"])
	}
	// The windowed today list should also include the just-created run.
	today, _ := data["subagentRunsToday"].([]map[string]any)
	if len(today) != 1 {
		t.Errorf("subagentRunsToday = %d, want 1", len(today))
	}
}

func TestCollectDashboardData_LegacySubagentUsageDoesNotCreateRuns(t *testing.T) {
	t.Setenv("OPENCLAW_CONTAINER", "")
	t.Setenv("OPENCLAW_STATE_DIR", "")
	prev := execCommandContext
	t.Cleanup(func() { execCommandContext = prev })
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "false") // tasks CLI fails; no run fallback allowed
	}
	t.Cleanup(resetModelCatalogForTest)

	tmp := t.TempDir()
	openclaw := filepath.Join(tmp, "openclaw")
	sessionDir := filepath.Join(openclaw, "agents", "main", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"cron"} {
		if err := os.MkdirAll(filepath.Join(openclaw, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(openclaw, "openclaw.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	store := map[string]map[string]any{
		"agent:main:subagent:legacy-task": {
			"sessionId":     "legacy-sub",
			"updatedAt":     float64(now.UnixMilli()),
			"contextTokens": 1000.0,
			"totalTokens":   100.0,
		},
	}
	storeData, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "sessions.json"), storeData, 0o644); err != nil {
		t.Fatal(err)
	}
	line := `{"timestamp":` + strconv.Quote(now.Format(time.RFC3339Nano)) + `,` +
		`"message":{"role":"assistant","model":"openai/gpt-5","usage":{"totalTokens":100,"input":60,"output":40,"cacheRead":0,"cost":{"total":0.12}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(sessionDir, "legacy-sub.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	dash := filepath.Join(tmp, "dashboard")
	if err := os.MkdirAll(dash, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := appconfig.Default()
	cfg.Timezone = "UTC"
	cfg.AI.GatewayPort = 0

	data := collectDashboardData(context.Background(), dash, openclaw, cfg)
	runs, ok := data["subagentRuns"].([]map[string]any)
	if !ok {
		t.Fatalf("subagentRuns type = %T, want []map[string]any", data["subagentRuns"])
	}
	if len(runs) != 0 {
		t.Fatalf("legacy token-derived subagent runs must be discarded; got %v", runs)
	}
	usage, ok := data["subagentUsage"].([]TokenUsageEntry)
	if !ok || len(usage) == 0 {
		t.Fatalf("subagentUsage = %#v (%T), want legacy token usage aggregates", data["subagentUsage"], data["subagentUsage"])
	}
	if usage[0].TotalTokensRaw != 100 {
		t.Fatalf("subagentUsage total = %d, want 100", usage[0].TotalTokensRaw)
	}
}

func TestCollectDashboardData_TopLevelContractKeys(t *testing.T) {
	t.Setenv("OPENCLAW_CONTAINER", "")
	t.Setenv("OPENCLAW_STATE_DIR", "")
	prev := execCommandContext
	t.Cleanup(func() { execCommandContext = prev })
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "false")
	}
	t.Cleanup(resetModelCatalogForTest)

	tmp := t.TempDir()
	openclaw := filepath.Join(tmp, "openclaw")
	for _, d := range []string{"cron", "agents"} {
		if err := os.MkdirAll(filepath.Join(openclaw, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(openclaw, "openclaw.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dash := filepath.Join(tmp, "dashboard")
	if err := os.MkdirAll(dash, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := appconfig.Default()
	cfg.Timezone = "UTC"
	cfg.AI.GatewayPort = 0

	data := collectDashboardData(context.Background(), dash, openclaw, cfg)
	assertTopLevelType[map[string]any](t, data, "gateway")
	assertTopLevelType[[]map[string]any](t, data, "crons")
	assertTopLevelType[[]map[string]any](t, data, "sessions")
	assertTopLevelType[[]map[string]any](t, data, "alerts")
	assertTopLevelType[[]map[string]any](t, data, "gitLog")
	assertTopLevelType[[]map[string]any](t, data, "skills")
	assertTopLevelType[[]map[string]any](t, data, "availableModels")
	assertTopLevelType[map[string]any](t, data, "agentConfig")
	assertTopLevelType[[]map[string]any](t, data, "subagentRuns")
	assertTopLevelType[[]TokenUsageEntry](t, data, "tokenUsage")
	assertTopLevelType[[]TokenUsageEntry](t, data, "subagentUsage")
	assertTopLevelType[[]map[string]any](t, data, "dailyChart")
	assertTopLevelType[map[string]any](t, data, "logConfig")
	for _, key := range []string{
		"totalCostToday", "totalCostAllTime", "projectedMonthly",
		"subagentCostAllTime", "subagentCostToday", "subagentCost7d", "subagentCost30d",
		"lastRefresh", "lastRefreshMs", "sessionCount", "botName", "botEmoji",
	} {
		if _, ok := data[key]; !ok {
			t.Fatalf("data missing top-level key %q", key)
		}
	}
}

func assertTopLevelType[T any](t *testing.T, data map[string]any, key string) {
	t.Helper()
	if _, ok := data[key].(T); !ok {
		t.Fatalf("%s type = %T, want %T", key, data[key], *new(T))
	}
}
