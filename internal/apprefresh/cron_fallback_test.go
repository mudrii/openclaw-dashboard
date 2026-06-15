package apprefresh

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// TestCollectDashboardData_CronCLIFailureFallsBackToFile drives the migration's
// cron wiring end-to-end through collectDashboardData: when `openclaw cron list
// --json` fails (here every CLI shell-out is forced to fail), the collector must
// fall back to the legacy cron/jobs.json file rather than show an empty panel.
// The two halves (collectCronsViaCLI / CollectCrons) are unit-tested in
// isolation; this locks the wired fallback branch a regression could silently
// break.
func TestCollectDashboardData_CronCLIFailureFallsBackToFile(t *testing.T) {
	prev := execCommandContext
	t.Cleanup(func() { execCommandContext = prev })
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "false") // every openclaw CLI call fails
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
	jobs := `{"jobs":[{"id":"j1","name":"FileCron","enabled":true,` +
		`"schedule":{"kind":"cron","expr":"0 0 * * *"},` +
		`"state":{"lastRunStatus":"ok","lastDurationMs":5}}]}`
	if err := os.WriteFile(filepath.Join(openclaw, "cron", "jobs.json"), []byte(jobs), 0o644); err != nil {
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
	crons, ok := data["crons"].([]map[string]any)
	if !ok {
		t.Fatalf("crons type = %T, want []map[string]any", data["crons"])
	}
	if len(crons) != 1 || crons[0]["name"] != "FileCron" {
		t.Fatalf("expected file-sourced cron 'FileCron' after CLI failure, got %v", crons)
	}
	if crons[0]["lastStatus"] != "ok" {
		t.Errorf("lastStatus = %v, want ok (from file state)", crons[0]["lastStatus"])
	}
}
