package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
		"data-section=\"runtime\"",
		"'runtime':false",
		"Runtime Health unavailable",
		"No models detected",
		"SR.provider && SR.provider !== '—'",
		"No skill entries reported",
		// Sub-agent panel post-migration: agent/duration/status columns, no cost.
		"<th>Task</th><th>Agent</th><th class=\"r\">Duration</th><th>Status</th><th>Time</th>",
		"$('subCostLbl').textContent=runs.length+(runs.length===1?' run':' runs');",
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("web/index.html missing frontend contract snippet %q", snippet)
		}
	}
	// The tasks store exposes no per-run cost/tokens, so the sub-agent runs table
	// must NOT render a cost cell (dropped in the SQLite-migration rework).
	if strings.Contains(html, "(r.cost||0).toFixed(4)") {
		t.Fatal("web/index.html: sub-agent runs table still renders a cost cell; cost was dropped post-migration")
	}

	runtimeSection := strings.Index(html, `data-section="runtime"`)
	agentConfigSection := strings.Index(html, `data-section="agent-config"`)
	if runtimeSection == -1 || agentConfigSection == -1 || runtimeSection > agentConfigSection {
		t.Fatal("web/index.html: runtime panels must be in a default-visible runtime section before collapsed agent configuration")
	}
	for _, id := range []string{"gatewayRuntimePanel", "runtimeHealthPanel"} {
		idx := strings.Index(html, `id="`+id+`"`)
		if idx == -1 {
			t.Fatalf("web/index.html missing %s", id)
		}
		if idx > agentConfigSection {
			t.Fatalf("web/index.html: %s is still nested after agent configuration", id)
		}
	}

	assertUniqueHTMLIDs(t, html)
	assertAriaControlsResolve(t, html)
	assertStaticIDLookupsResolve(t, html)
	assertSectionBindingsCoherent(t, html)
	assertNoExternalFrontendDependencies(t, html)
	assertReadmeScreenshotLinksExist(t)
}

func assertUniqueHTMLIDs(t *testing.T, html string) {
	t.Helper()
	re := regexp.MustCompile(`\bid="([A-Za-z][A-Za-z0-9_:-]*)"`)
	seen := map[string]bool{}
	for _, match := range re.FindAllStringSubmatch(html, -1) {
		id := match[1]
		if seen[id] {
			t.Fatalf("web/index.html duplicate id %q", id)
		}
		seen[id] = true
	}
}

func assertAriaControlsResolve(t *testing.T, html string) {
	t.Helper()
	idRe := regexp.MustCompile(`\bid="([A-Za-z][A-Za-z0-9_:-]*)"`)
	ids := map[string]bool{}
	for _, match := range idRe.FindAllStringSubmatch(html, -1) {
		ids[match[1]] = true
	}
	controlsRe := regexp.MustCompile(`\baria-controls="([A-Za-z][A-Za-z0-9_:-]*)"`)
	for _, match := range controlsRe.FindAllStringSubmatch(html, -1) {
		if !ids[match[1]] {
			t.Fatalf("web/index.html aria-controls target %q does not exist", match[1])
		}
	}
}

func assertStaticIDLookupsResolve(t *testing.T, html string) {
	t.Helper()
	idRe := regexp.MustCompile(`\bid="([A-Za-z][A-Za-z0-9_:-]*)"`)
	ids := map[string]bool{}
	for _, match := range idRe.FindAllStringSubmatch(html, -1) {
		ids[match[1]] = true
	}
	allowDynamic := map[string]bool{
		"gw-readiness-alert":    true,
		"oc-collapse-bootstrap": true,
	}
	for _, pattern := range []string{
		`\$\('([A-Za-z][A-Za-z0-9_:-]*)'\)`,
		`document\.getElementById\('([A-Za-z][A-Za-z0-9_:-]*)'\)`,
		`document\.getElementById\("([A-Za-z][A-Za-z0-9_:-]*)"\)`,
	} {
		re := regexp.MustCompile(pattern)
		for _, match := range re.FindAllStringSubmatch(html, -1) {
			id := match[1]
			if !ids[id] && !allowDynamic[id] {
				t.Fatalf("web/index.html static ID lookup %q has no matching element id", id)
			}
		}
	}
}

func assertSectionBindingsCoherent(t *testing.T, html string) {
	t.Helper()
	sectionRe := regexp.MustCompile(`(?s)<section class="oc-section" data-section="([A-Za-z0-9_-]+)">(.*?)</section>`)
	matches := sectionRe.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		t.Fatal("web/index.html has no collapsible oc-section blocks")
	}
	for _, match := range matches {
		key, block := match[1], match[2]
		bodyID := "oc-body-" + key
		for _, want := range []string{
			`aria-controls="` + bodyID + `"`,
			`onclick="Sections.toggle('` + key + `')"`,
			`id="` + bodyID + `"`,
		} {
			if !strings.Contains(block, want) {
				t.Fatalf("section %q missing binding %q", key, want)
			}
		}
		for _, want := range []string{
			`'` + key + `'`,
			`'` + key + `':`,
		} {
			if !strings.Contains(html, want) {
				t.Fatalf("section %q missing collapse registry token %q", key, want)
			}
		}
	}
}

func assertNoExternalFrontendDependencies(t *testing.T, html string) {
	t.Helper()
	for _, pattern := range []string{
		`(?is)<script\b[^>]*\bsrc\s*=`,
		`(?is)<link\b[^>]*\brel\s*=\s*["']?stylesheet\b`,
		`(?m)^\s*import\s+`,
		`(?i)@import\s+`,
		`https://cdn\.`,
		`https://unpkg\.com`,
		`https://esm\.sh`,
	} {
		re := regexp.MustCompile(pattern)
		if match := re.FindString(html); match != "" {
			t.Fatalf("web/index.html must remain embedded and dependency-free; found %q", match)
		}
	}
}

func assertReadmeScreenshotLinksExist(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`!\[[^\]]*\]\((screenshots/[^)]+\.png)\)`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("README.md has no screenshot image links")
	}
	for _, match := range matches {
		if _, err := os.Stat(match[1]); err != nil {
			t.Fatalf("README.md screenshot link %q is not readable: %v", match[1], err)
		}
	}
}
