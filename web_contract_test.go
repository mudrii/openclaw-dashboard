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

	index, err := os.ReadFile(filepath.Join("web", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(index)
	for _, snippet := range []string{
		"gwReadyAlertEl.querySelector('.alert-msg').textContent = alertMsg;",
		"msg.textContent = alertMsg;",
		"No skills configured",
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("web/index.html missing frontend contract snippet %q", snippet)
		}
	}
}
