package apprefresh

import (
	"context"
	"os/exec"
	"slices"
	"testing"
)

func TestChannelStatusFromBytes_GatewayAccounts(t *testing.T) {
	input := []byte(`{
		"gatewayReachable": true,
		"channelAccounts": {
			"slack": [{"enabled": true, "configured": true, "connected": false, "healthState": "degraded", "lastError": "token expired"}],
			"telegram": [{"enabled": true, "configured": true, "probe": {"ok": true}}]
		}
	}`)
	got, ok := channelStatusFromBytes(input)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	slack := got["slack"].(map[string]any)
	if slack["connected"] != false || slack["health"] != "degraded" || slack["error"] != "token expired" {
		t.Fatalf("slack status = %+v", slack)
	}
	telegram := got["telegram"].(map[string]any)
	if telegram["connected"] != true || telegram["health"] != "healthy" {
		t.Fatalf("telegram status = %+v", telegram)
	}
}

func TestOverlayChannelStatusFromCLI(t *testing.T) {
	agentConfig := map[string]any{
		"channelStatus": map[string]any{
			"slack":    map[string]any{"enabled": true, "configured": true, "connected": true, "health": "active"},
			"telegram": map[string]any{"enabled": true, "configured": true, "connected": nil, "health": nil},
		},
	}
	cliStatus := map[string]any{
		"slack": map[string]any{"enabled": true, "configured": true, "connected": false, "health": "unhealthy", "error": "probe failed"},
	}
	overlayChannelStatus(agentConfig, cliStatus)

	cs := agentConfig["channelStatus"].(map[string]any)
	slack := cs["slack"].(map[string]any)
	if slack["connected"] != false || slack["health"] != "unhealthy" || slack["error"] != "probe failed" {
		t.Fatalf("slack status = %+v, want CLI probe status", slack)
	}
	telegram := cs["telegram"].(map[string]any)
	if telegram["connected"] != nil || telegram["health"] != nil {
		t.Fatalf("telegram should be untouched when CLI has no telegram status: %+v", telegram)
	}
}

func TestCollectChannelStatusViaCLI_RunnerError(t *testing.T) {
	runner := func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "false")
	}
	if got, ok := collectChannelStatusViaCLI(context.Background(), runner, func() string { return "openclaw" }); ok || got != nil {
		t.Fatalf("got (%v,%v), want nil,false on runner error", got, ok)
	}
}

func TestCollectChannelStatusViaCLI_CommandContract(t *testing.T) {
	var gotName string
	var gotArgs []string
	runner := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return exec.CommandContext(ctx, "printf", `%s`, `{
			"channelAccounts":{"slack":[{"enabled":true,"configured":true,"connected":true,"healthState":"healthy"}]}
		}`)
	}
	status, ok := collectChannelStatusViaCLI(context.Background(), runner, func() string { return "/bin/openclaw" })
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if gotName != "/bin/openclaw" {
		t.Fatalf("runner name = %q, want /bin/openclaw", gotName)
	}
	wantArgs := []string{"channels", "status", "--probe", "--json", "--timeout", "10000"}
	if !slices.Equal(gotArgs, wantArgs) {
		t.Fatalf("runner args = %v, want %v", gotArgs, wantArgs)
	}
	if slack, ok := status["slack"].(map[string]any); !ok || slack["connected"] != true || slack["health"] != "healthy" {
		t.Fatalf("slack status = %#v, want healthy connected", status["slack"])
	}
}
