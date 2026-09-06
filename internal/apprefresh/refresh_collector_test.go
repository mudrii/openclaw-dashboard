package apprefresh_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
	apprefresh "github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

func TestRunRefreshCollectorWritesDashboardJSONAtomically(t *testing.T) {
	t.Setenv("OPENCLAW_CONTAINER", "unrelated-test-container")
	t.Setenv("OPENCLAW_STATE_DIR", "")
	tmp := t.TempDir()
	dashboardDir := filepath.Join(tmp, "dashboard")
	openclawPath := filepath.Join(tmp, "openclaw")
	if err := os.MkdirAll(filepath.Join(openclawPath, "agents", "main", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(openclawPath, "cron"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dashboardDir, 0o755); err != nil {
		t.Fatal(err)
	}

	openclawConfig := `{
		"agents": {
			"defaults": {
				"model": {"primary": "openai/gpt-5"},
				"compaction": {"mode": "auto"}
			},
			"list": [{"id": "main", "model": "openai/gpt-5"}]
		}
	}`
	if err := os.WriteFile(filepath.Join(openclawPath, "openclaw.json"), []byte(openclawConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(openclawPath, "cron", "jobs.json"), []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := appconfig.Default()
	cfg.Openclaw.Mode = "native"
	// Exercise file collection without contacting an installed CLI or gateway.
	cfg.Openclaw.Binary = filepath.Join(tmp, "missing-openclaw")
	cfg.Bot.Name = "Test Dashboard"
	cfg.Bot.Emoji = "*"
	cfg.Timezone = "UTC"
	cfg.Refresh.IntervalSeconds = 30
	cfg.AI.GatewayPort = 0
	// Simulate another refresh holding the old shared temporary filename open.
	// Publishing our snapshot must not let that writer change the final file.
	otherWriter, err := os.Create(filepath.Join(dashboardDir, "data.json.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = otherWriter.Close() })

	if err := apprefresh.RunRefreshCollector(context.Background(), dashboardDir, openclawPath, cfg); err != nil {
		t.Fatalf("RunRefreshCollector() error = %v", err)
	}
	if _, err := otherWriter.WriteAt([]byte("not JSON"), 0); err != nil {
		t.Fatal(err)
	}
	if files, err := filepath.Glob(filepath.Join(dashboardDir, "data.json.tmp-*")); err != nil || len(files) != 0 {
		t.Fatalf("temporary files leaked: %v, %v", files, err)
	}
	info, err := os.Stat(filepath.Join(dashboardDir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("data.json mode = %v, want 0600", got)
	}

	data, err := os.ReadFile(filepath.Join(dashboardDir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("data.json is invalid JSON: %v", err)
	}
	if payload["botName"] != "Test Dashboard" || payload["botEmoji"] != "*" {
		t.Errorf("bot fields not projected: %v %v", payload["botName"], payload["botEmoji"])
	}
	if payload["compactionMode"] != "auto" {
		t.Errorf("compactionMode = %v, want auto", payload["compactionMode"])
	}
	if _, ok := payload["agentConfig"].(map[string]any); !ok {
		t.Fatalf("agentConfig type = %T, want object", payload["agentConfig"])
	}
	dailyChart, ok := payload["dailyChart"].([]any)
	if !ok {
		t.Fatalf("dailyChart type = %T, want array", payload["dailyChart"])
	}
	if len(dailyChart) != 30 {
		t.Errorf("dailyChart length = %d, want 30", len(dailyChart))
	}
}
