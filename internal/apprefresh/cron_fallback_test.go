package apprefresh

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestCollectDashboardData_CronModelPrettifiedFromCatalog drives the CLI-success
// cron path AND the model catalog through collectDashboardData, proving the
// catalog is refreshed BEFORE the cron collector goroutine so the cron MODEL
// column resolves to the prettified catalog name. A reorder regression (catalog
// refreshed after crons) would leave the model collapsed to "MiniMax" and fail
// this test — which the unit cronJobToMap test (hand-seeded catalog) cannot
// catch.
func TestCollectDashboardData_CronModelPrettifiedFromCatalog(t *testing.T) {
	cronJSON := `{"jobs":[{"id":"j1","name":"C","enabled":true,` +
		`"schedule":{"kind":"cron","expr":"0 0 * * *"},` +
		`"payload":{"model":"minimax/MiniMax-M3"},"state":{"lastRunStatus":"ok"}}]}`
	catalogJSON := `{"models":[{"key":"minimax/MiniMax-M3","name":"MiniMax M3"}]}`

	prev := execCommandContext
	t.Cleanup(func() { execCommandContext = prev })
	execCommandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		j := strings.Join(args, " ")
		switch {
		case strings.Contains(j, "cron list"):
			return exec.CommandContext(ctx, "printf", "%s", cronJSON)
		case strings.Contains(j, "models list"):
			return exec.CommandContext(ctx, "printf", "%s", catalogJSON)
		default:
			return exec.CommandContext(ctx, "false")
		}
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
	crons, ok := data["crons"].([]map[string]any)
	if !ok || len(crons) != 1 {
		t.Fatalf("crons = %v, want 1 from the CLI", data["crons"])
	}
	if crons[0]["model"] != "MiniMax M3" {
		t.Errorf("cron model = %v, want MiniMax M3 (catalog must load before crons resolve)", crons[0]["model"])
	}
}
