package dashboard

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestFrontendReleaseRegressions(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available for embedded JavaScript behavior tests")
	}
	cmd := exec.CommandContext(t.Context(), node, "scripts/frontend-regression.cjs")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("frontend regressions: %v\n%s", err, output)
	}
}

func TestRuntimeFrontendUnknownAndEscapedStates(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available for embedded JavaScript behavior tests")
	}
	html, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	text := string(html)
	for _, script := range regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`).FindAllStringSubmatch(text, -1) {
		cmd := exec.CommandContext(t.Context(), node, "-e", "new (require('node:vm').Script)(process.argv[1])", script[1])
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("embedded script syntax: %v\n%s", err, output)
		}
	}
	// The slice starts at relTime because collectionLabel renders a stale
	// collectedAt through formatAbsTime, which lives in the same helper block.
	start := strings.Index(text, "function relTime(")
	end := strings.Index(text, "// === Theme ===")
	if start < 0 || end < start {
		t.Fatal("runtime presentation helpers missing")
	}
	js := `const assert=require('node:assert/strict'); const esc=s=>String(s??'').replaceAll('<','&lt;').replaceAll('>','&gt;');
const State={data:{timezone:'UTC'}};` + text[start:end] + `
assert.equal(runtimeMoney(null),'Unknown');
assert.equal(runtimeMoney(0),'$0.00');
assert.equal(runtimeMoney(undefined),'Unknown');
assert.equal(nativeSessionPath('agent:main:main'),'/chat/main');
assert.equal(nativeSessionPath('agent:main:cron:job'),'/chat/main/cron/job');
assert.equal(nativeSessionPath('agent:main:../private'),'/chat/main/~key/%2E%2E%2Fprivate');
assert.equal(nativeSessionPath('https://bad.example'),'/');
assert.match(collectionLabel({state:'stale',errorCode:'permission_denied',collectedAt:'2026-09-05T09:07:00Z'}),/stale.*permission_denied/);
assert.match(collectionLabel({state:'stale',collectedAt:'2026-09-05T09:07:00Z'}),/09:07.*UTC/);
assert.match(collectionLabel({state:'partial',errorCode:'configuration_unavailable',failedAgents:['main']}),/failed agents: main/);
assert.equal(gatewayUnknownHtml({status:'unknown',statusReason:'host_probe_not_applicable'}).includes('Unknown (container: host probe not applicable)'),true);
assert.equal(gatewayUnknownHtml({status:'unknown',error:'host_probe_not_applicable'}).includes('Unknown (container: host probe not applicable)'),true);
assert.equal(gatewayUnknownHtml({status:'offline'}),'');
// An unknown status is never repainted as Offline; only the parenthetical
// explanation depends on the backend's reason code.
assert.match(gatewayUnknownHtml({status:'unknown',statusReason:'permission_denied'}),/Unknown/);
assert.doesNotMatch(gatewayUnknownHtml({status:'unknown',statusReason:'permission_denied'}),/container/);
assert.match(gatewayUnknownHtml({status:'unknown'}),/Unknown/);
assert.doesNotMatch(gatewayUnknownHtml({status:'unknown'}),/container/);
assert.equal(runtimeActivity({active:null}),'Activity not reported');
assert.equal(runtimeActivity({active:false,activeRunIds:[]}), 'Idle');
assert.equal(runtimeActivity({active:true,activeRunIds:['run']}),'Running (1)');
assert.equal(filterRuntimeTasks([{id:'a',status:'blocked',runtime:'subagent',task:'<script>'},{id:'b',status:'failed',runtime:'cron'}],'blocked','all','').length,1);
assert.match(runtimeTable(['Name'], [['<script>']]),/&lt;script&gt;/);
assert.doesNotMatch(runtimeTable(['Name'], [['<script>']]),/<script>/);
`
	cmd := exec.CommandContext(t.Context(), node, "-e", js)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("frontend behavior: %v\n%s", err, output)
	}
}
