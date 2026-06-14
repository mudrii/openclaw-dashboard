package apprefresh

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// TestRunRefreshCollector_ReadyzFailingMarksChannelUnhealthy proves the INT-1
// wire-up end to end: collectDashboardData queries the /readyz probe and feeds
// failing[] into backfillChannelConnectivity, so a configured channel reported
// failing by the gateway lands in data.json with health="unhealthy".
func TestRunRefreshCollector_ReadyzFailingMarksChannelUnhealthy(t *testing.T) {
	prev := readyzProbe
	readyzProbe = func(_ context.Context, _ int) ([]string, bool) {
		return []string{"telegram"}, true
	}
	t.Cleanup(func() { readyzProbe = prev })

	tmp := t.TempDir()
	dashboardDir := filepath.Join(tmp, "dashboard")
	openclawPath := filepath.Join(tmp, "openclaw")
	for _, d := range []string{
		filepath.Join(openclawPath, "agents", "main", "sessions"),
		filepath.Join(openclawPath, "cron"),
		dashboardDir,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	openclawConfig := `{
		"channels": {"telegram": {"enabled": true, "token": "x"}},
		"agents": {"defaults": {"model": {"primary": "openai/gpt-5"}}, "list": [{"id": "main", "model": "openai/gpt-5"}]}
	}`
	if err := os.WriteFile(filepath.Join(openclawPath, "openclaw.json"), []byte(openclawConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(openclawPath, "cron", "jobs.json"), []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := appconfig.Default()
	cfg.Timezone = "UTC"
	cfg.Refresh.IntervalSeconds = 30
	cfg.AI.GatewayPort = 18789

	if err := RunRefreshCollector(context.Background(), dashboardDir, openclawPath, cfg); err != nil {
		t.Fatalf("RunRefreshCollector() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dashboardDir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	agentConfig, _ := payload["agentConfig"].(map[string]any)
	cs, _ := agentConfig["channelStatus"].(map[string]any)
	tg, ok := cs["telegram"].(map[string]any)
	if !ok {
		t.Fatalf("channelStatus.telegram missing; channelStatus=%v", cs)
	}
	if tg["health"] != "unhealthy" {
		t.Errorf("telegram health: want unhealthy (from /readyz failing[]), got %v", tg["health"])
	}
	if tg["connected"] != false {
		t.Errorf("telegram connected: want false, got %v", tg["connected"])
	}
}

func TestBuildGroupNames_ExtractsSubject(t *testing.T) {
	stores := []SessionStoreFile{{
		AgentName: "work",
		Store: map[string]map[string]any{
			"agent:work:group:42:main": {"subject": "My Team"},
			"agent:work:group:43:main": {"displayName": "Other Team"},
			// Should be skipped: contains topic
			"agent:work:group:44:topic:foo": {"subject": "Topic Sub"},
			// Should be skipped: telegram-prefixed name
			"agent:work:group:45:main": {"subject": "telegram:thing"},
		},
	}}
	out := buildGroupNames(stores)
	if out["42"] != "My Team" {
		t.Errorf("gid=42: want %q, got %q", "My Team", out["42"])
	}
	if out["43"] != "Other Team" {
		t.Errorf("gid=43: want %q, got %q", "Other Team", out["43"])
	}
	if _, ok := out["44"]; ok {
		t.Errorf("gid=44 (topic) should be skipped")
	}
	if _, ok := out["45"]; ok {
		t.Errorf("gid=45 (telegram-prefixed) should be skipped")
	}
}

func TestEnrichBindings_FillsNames(t *testing.T) {
	agentConfig := map[string]any{
		"bindings": []any{
			map[string]any{"id": "42", "name": ""},
			map[string]any{"id": "99", "name": ""},
			map[string]any{"id": "", "name": ""},
		},
	}
	enrichBindings(agentConfig, map[string]string{"42": "My Team"})

	bindings := agentConfig["bindings"].([]any)
	if got := bindings[0].(map[string]any)["name"]; got != "My Team" {
		t.Errorf("binding 42 name: want %q, got %q", "My Team", got)
	}
	if got := bindings[1].(map[string]any)["name"]; got != "" {
		t.Errorf("binding 99 (unmapped) name: want empty, got %q", got)
	}
	if got := bindings[2].(map[string]any)["name"]; got != "" {
		t.Errorf("binding empty-id name: want empty, got %q", got)
	}
}

func TestEnrichBindings_NoBindingsField(t *testing.T) {
	// Should not panic on missing/invalid bindings.
	enrichBindings(map[string]any{}, map[string]string{"42": "X"})
	enrichBindings(map[string]any{"bindings": "not-a-slice"}, map[string]string{"42": "X"})
}

func TestBackfillChannelConnectivity_MarksActive(t *testing.T) {
	sessions := []map[string]any{
		{"key": "agent:work:telegram:123:main", "active": true},
		{"key": "agent:main:cron:42:main", "active": true},     // ignored: cron
		{"key": "agent:main:subagent:5:main", "active": true},  // ignored: subagent
		{"key": "agent:work:whatsapp:7:main", "active": false}, // inactive
	}
	agentConfig := map[string]any{
		"channelStatus": map[string]any{
			"telegram": map[string]any{"connected": nil, "health": nil},
			"whatsapp": map[string]any{"connected": nil, "health": nil},
			"slack":    map[string]any{"connected": true, "health": "ok"},
		},
	}
	backfillChannelConnectivity(agentConfig, sessions, nil)

	cs := agentConfig["channelStatus"].(map[string]any)
	tg := cs["telegram"].(map[string]any)
	if tg["connected"] != true {
		t.Errorf("telegram connected: want true, got %v", tg["connected"])
	}
	if tg["health"] != "active" {
		t.Errorf("telegram health: want active, got %v", tg["health"])
	}
	wa := cs["whatsapp"].(map[string]any)
	if wa["connected"] != nil {
		t.Errorf("whatsapp should remain unset (no active session), got %v", wa["connected"])
	}
	sl := cs["slack"].(map[string]any)
	if sl["connected"] != true || sl["health"] != "ok" {
		t.Errorf("slack pre-existing values must not be overwritten, got %+v", sl)
	}
}

// TestBackfillChannelConnectivity_FailingMarksUnhealthy proves the INT-1
// /readyz integration: a channel whose config key appears in the gateway's
// failing[] is forced connected=false / health="unhealthy", and this readiness
// signal overrides the session-activity heuristic — a channel with a live
// session but a failing token (e.g. revoked API credential) must still show red.
func TestBackfillChannelConnectivity_FailingMarksUnhealthy(t *testing.T) {
	sessions := []map[string]any{
		{"key": "agent:work:telegram:123:main", "active": true}, // active but failing
	}
	agentConfig := map[string]any{
		"channelStatus": map[string]any{
			"telegram": map[string]any{"connected": nil, "health": nil},
			"slack":    map[string]any{"connected": nil, "health": nil},
		},
	}
	backfillChannelConnectivity(agentConfig, sessions, []string{"telegram"})

	cs := agentConfig["channelStatus"].(map[string]any)
	tg := cs["telegram"].(map[string]any)
	if tg["connected"] != false {
		t.Errorf("telegram connected: want false (failing overrides active session), got %v", tg["connected"])
	}
	if tg["health"] != "unhealthy" {
		t.Errorf("telegram health: want unhealthy, got %v", tg["health"])
	}
	// slack is not failing and has no active session: heuristic leaves it unset.
	sl := cs["slack"].(map[string]any)
	if sl["connected"] != nil {
		t.Errorf("slack should remain unset (not failing, no active session), got %v", sl["connected"])
	}
}

// TestBackfillChannelConnectivity_NonChannelFailingTokenIgnored proves that
// non-channel readiness reasons ("startup-sidecars", "internal") in failing[]
// never create spurious channelStatus entries — only keys already present are
// touched.
func TestBackfillChannelConnectivity_NonChannelFailingTokenIgnored(t *testing.T) {
	agentConfig := map[string]any{
		"channelStatus": map[string]any{
			"telegram": map[string]any{"connected": nil, "health": nil},
		},
	}
	backfillChannelConnectivity(agentConfig, nil, []string{"startup-sidecars", "internal"})

	cs := agentConfig["channelStatus"].(map[string]any)
	if _, exists := cs["startup-sidecars"]; exists {
		t.Errorf("non-channel token startup-sidecars must not create a channelStatus entry")
	}
	tg := cs["telegram"].(map[string]any)
	if tg["connected"] != nil {
		t.Errorf("telegram not in failing[]: want unset, got %v", tg["connected"])
	}
}
