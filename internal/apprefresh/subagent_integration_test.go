package apprefresh

import (
	"context"
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
