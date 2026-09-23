package appopenclaw

import (
	"strings"
	"testing"
)

// redactBenchLines mixes typical gateway log lines (most carry no secret)
// with a few that do, roughly matching a real log tail.
var redactBenchLines = []string{
	"2026-09-23T10:00:00.123Z [gateway] [INFO] session agent:main started run 4f1c2a",
	"2026-09-23T10:00:01.456Z [gateway] [INFO] model claude-opus responded in 1834ms",
	"2026-09-23T10:00:02.789Z [cron] [WARN] job nightly-digest skipped: previous run still active",
	"2026-09-23T10:00:03.000Z [gateway] [ERROR] upstream 502 from provider after 3 retries",
	"2026-09-23T10:00:04.111Z [channels] [INFO] telegram channel healthy (latency 212ms)",
	"2026-09-23T10:00:05.222Z [gateway] [INFO] usage input=1200 output=340 cacheRead=8000",
	"2026-09-23T10:00:06.333Z [gateway] [DEBUG] tokens: 1540 total for session main",
	"2026-09-23T10:00:07.444Z [gateway] [WARN] Authorization: Bearer abcdef0123456789abcdef rejected",
	`{"time":"2026-09-23T10:00:08Z","level":"info","msg":"connected to ws://127.0.0.1:18789/rpc"}`,
	"2026-09-23T10:00:09.555Z [gateway] [INFO] heartbeat ok",
}

func BenchmarkRedactLogLines(b *testing.B) {
	for b.Loop() {
		for _, line := range redactBenchLines {
			_ = Redact(line)
		}
	}
}

// BenchmarkRedactPatternsLogLines is the pre-prefilter baseline: every line
// runs the full regex set.
func BenchmarkRedactPatternsLogLines(b *testing.B) {
	for b.Loop() {
		for _, line := range redactBenchLines {
			_ = redactPatterns(line)
		}
	}
}

// FuzzRedactPrefilterMatchesPatterns proves the literal prefilter never skips
// a line the full pattern set would change.
func FuzzRedactPrefilterMatchesPatterns(f *testing.F) {
	for _, line := range redactBenchLines {
		f.Add(line)
	}
	for _, seed := range []string{
		"toKen=abc123secretvalue", // Kelvin sign folds to k under (?i)
		"ſecret: hunter2",         // long s folds to s under (?i)
		"bot123456:" + strings.Repeat("A", 35),
		"https://user:pw@example.com/x",
		"ghp_" + strings.Repeat("a", 36),
		"AIza" + strings.Repeat("b", 35),
		"xoxb-1234567890-abc",
		"sk-abcdefghijkl",
		"--gateway-token  value",
		`{\"password\":\"p w\"}`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := Redact(s), redactPatterns(s); got != want {
			t.Fatalf("Redact(%q) = %q, want %q", s, got, want)
		}
	})
}
