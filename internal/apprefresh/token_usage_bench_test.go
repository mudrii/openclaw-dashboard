package apprefresh

import (
	"bufio"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tokenFixtureModels mirrors the provider/model ids seen in real OpenClaw
// transcripts, including one delivery-mirror id that must never be counted.
var tokenFixtureModels = []string{
	"claude-opus-4-6",
	"anthropic/claude-sonnet-4-6",
	"openai/gpt-5",
	"google/gemini-2.5-pro",
	"kimi-coding/k2p5",
	"zai/glm-5.2",
	"xai/grok-4-fast",
	"custom-local/qwen3-coder",
	"delivery-mirror",
}

var tokenFixtureAliases = map[string]string{
	"custom-local/qwen3-coder": "Qwen Coder",
	"zai/glm-5.2":              "glm-5.2",
}

// tokenFixtureText is prose with multibyte runes, JSON-escaped quotes and
// newlines, the way assistant text and tool output appear in transcripts.
const tokenFixtureText = `Here is the plan — first I'll check the \"config\" file, then run the tests.\n` +
	`日本語のテキストも含まれます。Ünïcödé ✓ 🚀 données café naïve.\n` +
	"```go\\nfunc main() { fmt.Println(\\\"hello\\\") }\\n```\\n"

// writeTokenUsageFixtureLine appends one synthetic transcript line. Most
// lines are real-shaped assistant/toolResult/user messages; a deterministic
// minority are malformed, carry invalid UTF-8, duplicate keys, or top-level
// usage so the benchmark exercises every branch of the parser.
func writeTokenUsageFixtureLine(w *bufio.Writer, r *rand.Rand, base time.Time, fileIdx, i int, model string) {
	ts := base.Add(time.Duration(i) * 97 * time.Second)
	stamp := ts.UTC().Format("2006-01-02T15:04:05.000Z")
	textReps := 1 + r.IntN(4)
	text := strings.Repeat(tokenFixtureText, textReps)
	inp, out := 500+r.IntN(20000), 50+r.IntN(4000)
	cr, cw := r.IntN(80000), r.IntN(3000)
	tt := inp + out + cr + cw
	cost := float64(tt) * 0.0000031

	switch k := r.IntN(100); {
	case k < 45: // assistant message with thinking + text + tool call
		fmt.Fprintf(w, `{"type":"message","id":"a%d-%d","parentId":"p%d","timestamp":%q,"message":{"role":"assistant","content":[{"type":"thinking","thinking":"%s","thinkingSignature":"EqQBCkYIBRgCKkB%032x"},{"type":"text","text":"%s"},{"type":"toolCall","id":"call_%d","name":"exec","arguments":{"command":"ls -la /tmp","timeout":30}}],"api":"anthropic-messages","provider":"anthropic","model":%q,"usage":{"input":%d,"output":%d,"cacheRead":%d,"cacheWrite":%d,"totalTokens":%d,"cost":{"input":0.01,"output":0.02,"cacheRead":0.001,"cacheWrite":0.002,"total":%g},"contextUsage":{"state":"ok"}},"stopReason":"toolUse","timestamp":%d,"responseId":"msg_%x","responseModel":%q}}`+"\n",
			fileIdx, i, i-1, stamp, text, r.Uint64(), text, i, model, inp, out, cr, cw, tt, cost, ts.UnixMilli(), r.Uint64(), model)
	case k < 70: // tool result with a large output blob
		fmt.Fprintf(w, `{"type":"message","id":"t%d-%d","parentId":"a%d","timestamp":%q,"message":{"role":"toolResult","toolCallId":"call_%d","toolName":"exec","content":[{"type":"text","text":"%s"}],"details":{"ok":true,"exitCode":0,"durationMs":%d},"isError":false,"timestamp":%d}}`+"\n",
			fileIdx, i, i-1, stamp, i, strings.Repeat(tokenFixtureText, 4+r.IntN(12)), r.IntN(5000), ts.UnixMilli())
	case k < 82: // user message
		fmt.Fprintf(w, `{"type":"message","id":"u%d-%d","parentId":"t%d","timestamp":%q,"message":{"role":"user","content":"%s","timestamp":%d,"__openclaw":{"senderIsOwner":true}}}`+"\n",
			fileIdx, i, i-1, stamp, tokenFixtureText, ts.UnixMilli())
	case k < 88: // bookkeeping events
		fmt.Fprintf(w, `{"type":"custom","customType":"model-snapshot","data":{"timestamp":%d,"provider":"anthropic","modelApi":"anthropic-messages","modelId":%q},"id":"c%d","parentId":"u%d","timestamp":%q}`+"\n",
			ts.UnixMilli(), model, i, i-1, stamp)
	case k < 91: // top-level usage (legacy shape) with an offset timestamp
		fmt.Fprintf(w, `{"timestamp":%q,"usage":{"input":%d,"output":%d,"totalTokens":%d,"cost":{"total":%g}},"message":{"role":"assistant","model":%q}}`+"\n",
			ts.In(time.FixedZone("plus8", 8*3600)).Format(time.RFC3339Nano), inp, out, inp+out, cost, model)
	case k < 93: // duplicate keys: v1 map decoding keeps the last value
		fmt.Fprintf(w, `{"timestamp":"not-a-time","timestamp":%q,"message":{"role":"user","role":"assistant","model":"ignored","model":%q,"usage":{"totalTokens":1,"totalTokens":%d,"input":%d}}}`+"\n",
			stamp, model, tt, inp)
	case k < 95: // invalid UTF-8 inside text and model
		fmt.Fprintf(w, `{"timestamp":%q,"message":{"role":"assistant","model":"%s\xff\xfe","content":[{"type":"text","text":"bad \xc3\x28 bytes"}],"usage":{"totalTokens":%d,"input":%d,"output":%d,"cost":{"total":%g}}}}`+"\n",
			stamp, model, tt, inp, out, cost)
	case k < 97: // truncated (malformed) assistant line
		line := fmt.Sprintf(`{"timestamp":%q,"message":{"role":"assistant","model":%q,"usage":{"totalTokens":%d,"input":%d}}}`, stamp, model, tt, inp)
		fmt.Fprintln(w, line[:len(line)/2])
	case k < 98: // zero-token and negative-cost assistant turns
		fmt.Fprintf(w, `{"timestamp":%q,"message":{"role":"assistant","model":%q,"usage":{"totalTokens":0,"input":0}}}`+"\n", stamp, model)
		fmt.Fprintf(w, `{"timestamp":%q,"message":{"role":"assistant","model":%q,"usage":{"totalTokens":%d,"input":%d,"cost":{"total":-1.5}}}}`+"\n", stamp, model, tt, inp)
	default: // wrong-typed fields and blank lines
		fmt.Fprintf(w, `{"timestamp":12345,"message":{"role":"assistant","model":42,"usage":{"totalTokens":%d,"input":"12","cost":{"total":"x"}}}}`+"\n\n", tt)
	}
}

// writeTokenUsageFixture writes files×lines synthetic transcripts under
// root/<agent>/sessions and returns the agents base path. Every eighth file
// is a subagent session and every tenth a deleted recovery copy.
func writeTokenUsageFixture(tb testing.TB, root string, files, lines int) (basePath string, sidToKey map[string]string) {
	tb.Helper()
	basePath = filepath.Join(root, "agents")
	sidToKey = map[string]string{}
	r := rand.New(rand.NewPCG(1, 2))
	base := time.Date(2026, 8, 20, 6, 0, 0, 0, time.UTC)
	for f := range files {
		agent := []string{"main", "work", "research"}[f%3]
		dir := filepath.Join(basePath, agent, "sessions")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			tb.Fatal(err)
		}
		sid := fmt.Sprintf("%08x-4723-9830-12dcd46e%04d", f*7919, f)
		name := sid + ".jsonl"
		if f%10 == 9 {
			name += ".deleted.20260901T101010+0800"
		}
		if f%8 == 0 {
			sidToKey[sid] = "agent:" + agent + ":subagent:" + sid
		} else {
			sidToKey[sid] = "agent:" + agent + ":main"
		}
		fh, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			tb.Fatal(err)
		}
		w := bufio.NewWriterSize(fh, 1<<20)
		model := tokenFixtureModels[f%len(tokenFixtureModels)]
		start := base.Add(time.Duration(f) * 3 * time.Hour)
		for i := range lines {
			if i%500 == 499 { // mid-session model switch
				model = tokenFixtureModels[(f+i)%len(tokenFixtureModels)]
			}
			writeTokenUsageFixtureLine(w, r, start, f, i, model)
		}
		if err := w.Flush(); err != nil {
			tb.Fatal(err)
		}
		if err := fh.Close(); err != nil {
			tb.Fatal(err)
		}
	}
	return basePath, sidToKey
}

func collectTokenFixture(cachePath, basePath string, sidToKey map[string]string) (tokenAggregates, []map[string]any) {
	agg := freshAggregates()
	runs := CollectTokenUsageWithCache(
		cachePath, basePath, time.UTC, "2026-09-10", "2026-09-04", "2026-08-12",
		map[string]string{}, sidToKey, tokenFixtureAliases,
		agg.modelsAll, agg.modelsToday, agg.models7d, agg.models30d,
		agg.subagentAll, agg.subagentToday, agg.subagent7d, agg.subagent30d,
		agg.dailyCosts, agg.dailyTokens, agg.dailyCalls, agg.dailySubagentCosts, agg.dailySubagentCount,
	)
	return agg, runs
}

// BenchmarkTokenUsageParseFile parses one 2,000-line transcript (~3 MB).
func BenchmarkTokenUsageParseFile(b *testing.B) {
	basePath, _ := writeTokenUsageFixture(b, b.TempDir(), 1, 2000)
	path := filepath.Join(basePath, "main", "sessions", "00000000-4723-9830-12dcd46e0000.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(info.Size())
	for b.Loop() {
		if _, err := parseTokenUsageFile(path, info, time.UTC); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTokenUsageCollectCold parses 200 transcripts × 200 lines with no
// cache: the first refresh after start-up or a timezone change.
func BenchmarkTokenUsageCollectCold(b *testing.B) {
	basePath, sidToKey := writeTokenUsageFixture(b, b.TempDir(), 200, 200)
	for b.Loop() {
		collectTokenFixture("", basePath, sidToKey)
	}
}

// BenchmarkTokenUsageCollectWarm is the steady-state refresh: every
// transcript is unchanged, so the cost is cache load + stat + apply + save.
func BenchmarkTokenUsageCollectWarm(b *testing.B) {
	root := b.TempDir()
	basePath, sidToKey := writeTokenUsageFixture(b, root, 200, 200)
	cachePath := filepath.Join(root, "token-cache.json")
	collectTokenFixture(cachePath, basePath, sidToKey)
	for b.Loop() {
		collectTokenFixture(cachePath, basePath, sidToKey)
	}
}

// BenchmarkTokenUsageApply isolates the fold of cached per-file summaries
// into the dashboard aggregates.
func BenchmarkTokenUsageApply(b *testing.B) {
	basePath, sidToKey := writeTokenUsageFixture(b, b.TempDir(), 200, 200)
	type item struct {
		path    string
		summary tokenUsageFileSummary
	}
	var items []item
	paths, _ := filepath.Glob(filepath.Join(basePath, "*/sessions/*.jsonl*"))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			b.Fatal(err)
		}
		s, err := parseTokenUsageFile(p, info, time.UTC)
		if err != nil {
			b.Fatal(err)
		}
		items = append(items, item{p, s})
	}
	for b.Loop() {
		agg := freshAggregates()
		var runs []map[string]any
		for _, it := range items {
			applyTokenUsageSummary(it.path, it.summary, time.UTC, "2026-09-10", "2026-09-04", "2026-08-12",
				map[string]string{}, sidToKey, tokenFixtureAliases,
				agg.modelsAll, agg.modelsToday, agg.models7d, agg.models30d,
				agg.subagentAll, agg.subagentToday, agg.subagent7d, agg.subagent30d,
				agg.dailyCosts, agg.dailyTokens, agg.dailyCalls, agg.dailySubagentCosts, agg.dailySubagentCount,
				&runs)
		}
	}
}
