package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssue26FrontendFixtureContract(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("web", "testdata", "issue-26-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Dashboard map[string]any `json:"dashboard"`
		System    map[string]any `json:"system"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("fixture is invalid JSON: %v", err)
	}

	dashboard := fixture.Dashboard
	if _, ok := dashboard["agentConfig"].(map[string]any); !ok {
		t.Fatalf("dashboard.agentConfig type = %T, want object", dashboard["agentConfig"])
	}
	if skills, ok := dashboard["skills"].([]any); !ok || len(skills) != 0 {
		t.Fatalf("dashboard.skills = %#v, want empty array for empty-state regression", dashboard["skills"])
	}
	crons, ok := dashboard["crons"].([]any)
	if !ok || len(crons) != 1 {
		t.Fatalf("dashboard.crons = %#v, want one cron with delivery/flapping state", dashboard["crons"])
	}
	cron := crons[0].(map[string]any)
	if cron["lastDeliveryStatus"] != "not-delivered" || cron["flapping"] != true {
		t.Fatalf("cron delivery/flapping = %v/%v, want not-delivered/true", cron["lastDeliveryStatus"], cron["flapping"])
	}
	if diagnostics, ok := cron["lastDiagnostics"].([]any); !ok || len(diagnostics) != 2 {
		t.Fatalf("cron.lastDiagnostics = %#v, want diagnostic strings", cron["lastDiagnostics"])
	}
	agentConfig := dashboard["agentConfig"].(map[string]any)
	channelStatus, ok := agentConfig["channelStatus"].(map[string]any)
	if !ok {
		t.Fatalf("dashboard.agentConfig.channelStatus type = %T, want object", agentConfig["channelStatus"])
	}
	slack, ok := channelStatus["slack"].(map[string]any)
	if !ok || slack["health"] != "unhealthy" || slack["connected"] != false {
		t.Fatalf("slack channel status = %#v, want unhealthy disconnected", channelStatus["slack"])
	}

	openclaw, ok := fixture.System["openclaw"].(map[string]any)
	if !ok {
		t.Fatalf("system.openclaw type = %T, want object", fixture.System["openclaw"])
	}
	gateway, ok := openclaw["gateway"].(map[string]any)
	if !ok {
		t.Fatalf("system.openclaw.gateway type = %T, want object", openclaw["gateway"])
	}
	if gateway["live"] != true || gateway["ready"] != false {
		t.Fatalf("gateway live/ready = %v/%v, want true/false degraded fixture", gateway["live"], gateway["ready"])
	}
	failing, ok := gateway["failing"].([]any)
	if !ok || len(failing) != 1 || !strings.Contains(failing[0].(string), "<script>") {
		t.Fatalf("gateway.failing = %#v, want one markup-bearing dependency name", gateway["failing"])
	}
	status, ok := openclaw["status"].(map[string]any)
	if !ok {
		t.Fatalf("system.openclaw.status type = %T, want object", openclaw["status"])
	}
	if tasks, ok := status["tasks"].(map[string]any); !ok || tasks["active"] != float64(2) {
		t.Fatalf("system.openclaw.status.tasks = %#v, want active task fixture", status["tasks"])
	}
	if eventLoop, ok := status["eventLoop"].(map[string]any); !ok || eventLoop["degraded"] != true {
		t.Fatalf("system.openclaw.status.eventLoop = %#v, want degraded fixture", status["eventLoop"])
	}
	if pc, ok := status["pluginCompatibility"].(map[string]any); !ok || pc["count"] != float64(1) {
		t.Fatalf("system.openclaw.status.pluginCompatibility = %#v, want warning fixture", status["pluginCompatibility"])
	}
	if summary, ok := status["channelSummary"].([]any); !ok || len(summary) != 2 {
		t.Fatalf("system.openclaw.status.channelSummary = %#v, want channel summary fixture", status["channelSummary"])
	}

	index, err := os.ReadFile(filepath.Join("web", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(index)
	for _, snippet := range []string{
		"gwReadyAlertEl.querySelector('.alert-msg').textContent = alertMsg;",
		"msg.textContent = alertMsg;",
		"const diagnostics=Array.isArray(c.lastDiagnostics)?c.lastDiagnostics.filter(Boolean).join(' · '):'';",
		"const flap=c.flapping?",
		"const healthColor = ['unhealthy','disconnected','offline','error','down','failing'].includes(healthLc) ? 'var(--red)'",
		"const tasks=ocStatus.tasks, evl=ocStatus.eventLoop, pc=ocStatus.pluginCompatibility, hb=ocStatus.lastHeartbeat;",
		"No skills configured",
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("web/index.html missing frontend contract snippet %q", snippet)
		}
	}
}
