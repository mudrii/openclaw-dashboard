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
	start := strings.Index(text, "// === Runtime data presentation ===")
	end := strings.Index(text, "// === Theme ===")
	if start < 0 || end < start {
		t.Fatal("runtime presentation helpers missing")
	}
	js := `const assert=require('node:assert/strict'); const esc=s=>String(s??'').replaceAll('<','&lt;').replaceAll('>','&gt;');` + text[start:end] + `
assert.equal(runtimeMoney(null),'Unknown');
assert.equal(runtimeMoney(0),'$0.00');
assert.equal(runtimeMoney(undefined),'Unknown');
assert.equal(nativeSessionPath('agent:main:main'),'/chat/main');
assert.equal(nativeSessionPath('agent:main:cron:job'),'/chat/main/cron/job');
assert.equal(nativeSessionPath('agent:main:../private'),'/chat/main/~key/%2E%2E%2Fprivate');
assert.equal(nativeSessionPath('https://bad.example'),'/');
assert.match(collectionLabel({state:'stale',errorCode:'permission_denied',collectedAt:'old'}),/stale.*permission_denied/);
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
