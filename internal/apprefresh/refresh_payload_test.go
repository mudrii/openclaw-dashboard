package apprefresh

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appconfig"
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// stubRuntimeGateway answers every gateway RPC with an empty, well-formed body
// so collectDashboardData runs hermetically on the runtime path.
func stubRuntimeGateway(t *testing.T) {
	t.Helper()
	previous := execCommandContext
	t.Cleanup(func() { execCommandContext = previous; resetModelCatalogForTest() })
	execCommandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		body := `{}`
		if len(args) > 2 && args[0] == "gateway" {
			switch args[2] {
			case "sessions.list":
				body = `{"sessions":[],"hasMore":false}`
			case "tasks.list":
				body = `{"tasks":[]}`
			}
		}
		return exec.CommandContext(ctx, "printf", "%s", body)
	}
}

func TestDashboardInvalidTimezoneLabelsUTC(t *testing.T) {
	stubRuntimeGateway(t)
	cfg := appconfig.Default()
	cfg.Openclaw = appopenclaw.Target{Mode: "container", Container: "fixture"}
	cfg.Timezone = "Not/AZone"
	data := collectDashboardData(t.Context(), t.TempDir(), t.TempDir(), cfg)
	if got := data["lastRefresh"].(string); !strings.HasSuffix(got, " UTC") {
		t.Fatalf("lastRefresh = %q, want UTC label for the fallback zone", got)
	}
	if data["timezone"] != "UTC" {
		t.Fatalf("timezone = %v, want UTC", data["timezone"])
	}
}

func TestDashboardEmptyBotConfigUsesAppconfigDefaults(t *testing.T) {
	stubRuntimeGateway(t)
	cfg := appconfig.Default()
	cfg.Openclaw = appopenclaw.Target{Mode: "container", Container: "fixture"}
	cfg.Bot = appconfig.BotConfig{}
	data := collectDashboardData(t.Context(), t.TempDir(), t.TempDir(), cfg)
	def := appconfig.Default().Bot
	if data["botName"] != def.Name || data["botEmoji"] != def.Emoji {
		t.Fatalf("bot = %v %v, want %q %q", data["botName"], data["botEmoji"], def.Name, def.Emoji)
	}
}

// TestRunRefreshCollectorRetainsLastGoodSessionsAcrossRuns drives retention
// through the real data.json written by a previous run.
func TestRunRefreshCollectorRetainsLastGoodSessionsAcrossRuns(t *testing.T) {
	t.Setenv("OPENCLAW_CONTAINER", "")
	t.Setenv("OPENCLAW_STATE_DIR", "")
	previous := execCommandContext
	t.Cleanup(func() { execCommandContext = previous; resetModelCatalogForTest() })
	sessionsUp := true
	execCommandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		body := `{}`
		if len(args) > 2 && args[0] == "gateway" {
			switch args[2] {
			case "sessions.list":
				if !sessionsUp {
					return exec.CommandContext(ctx, "sh", "-c", "exit 1")
				}
				body = `{"sessions":[{"key":"agent:main:main","sessionId":"s","displayName":"Kept"}],"hasMore":false,"totalCount":1}`
			case "tasks.list":
				body = `{"tasks":[]}`
			}
		}
		return exec.CommandContext(ctx, "printf", "%s", body)
	}
	dashboardDir := t.TempDir()
	cfg := appconfig.Default()
	cfg.Openclaw = appopenclaw.Target{Mode: "container", Container: "fixture"}
	openclawPath := t.TempDir()

	if err := RunRefreshCollector(t.Context(), dashboardDir, openclawPath, cfg); err != nil {
		t.Fatal(err)
	}
	sessionsUp = false
	if err := RunRefreshCollector(t.Context(), dashboardDir, openclawPath, cfg); err != nil {
		t.Fatal(err)
	}

	snapshot := readPreviousSnapshot(filepath.Join(dashboardDir, "data.json"))
	status := asObj(asObj(snapshot["collections"])["sessions"])
	if status["state"] != "stale" || jsonStr(status, "collectedAt") == "" {
		t.Fatalf("sessions status=%v, want stale with the first run's collectedAt", status)
	}
	rows, _ := snapshot["sessions"].([]any)
	if len(rows) != 1 || asObj(rows[0])["name"] != "Kept" {
		t.Fatalf("sessions=%v, want the first run's row retained", snapshot["sessions"])
	}
}
