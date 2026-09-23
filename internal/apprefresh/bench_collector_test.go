package apprefresh

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// Collector benchmarks run against a generated legacy (pre-SQLite) state
// directory: several agents with sessions.json stores and JSONL transcripts,
// a cron jobs.json, and a large mixed-format gateway log. Every subprocess and
// network seam is stubbed so no real openclaw, git, pgrep, or HTTP call runs.

const (
	benchSessionsPerAgent = 100
	benchTranscriptLines  = 120
	benchCronJobs         = 30
	benchGatewayLogLines  = 50_000
)

var benchAgents = []string{"main", "work", "ops"}

// stubCollectorSeams replaces every subprocess/network seam with an immediate
// failure so the collectors take their offline fallbacks.
func stubCollectorSeams(tb testing.TB) {
	tb.Helper()
	missing := filepath.Join(tb.TempDir(), "missing-openclaw")
	origResolve, origExec := resolveOpenclawBin, execCommandContext
	origFetch, origChannel := fetchLiveSessionModels, channelStatusCollector
	origPgrep, origHealthz, origReadyz, origLock := pgrepGateway, healthzProbe, readyzProbe, readGatewayLockMeta
	resolveOpenclawBin = func() string { return missing }
	execCommandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, missing, args...)
	}
	fetchLiveSessionModels = func(context.Context) map[string]string { return map[string]string{} }
	channelStatusCollector = func(context.Context, func(context.Context, string, ...string) *exec.Cmd, func() string) (map[string]any, bool) {
		return nil, false
	}
	pgrepGateway = func(context.Context) ([]byte, error) { return nil, errors.New("stub") }
	healthzProbe = func(context.Context, int) bool { return false }
	readyzProbe = func(context.Context, int) ([]string, bool) { return nil, false }
	readGatewayLockMeta = func(string) (gatewayLock, bool) { return gatewayLock{}, false }
	resetModelCatalogForTest()
	resetLiveSessionModelCacheForTest()
	tb.Cleanup(func() {
		resolveOpenclawBin, execCommandContext = origResolve, origExec
		fetchLiveSessionModels, channelStatusCollector = origFetch, origChannel
		pgrepGateway, healthzProbe, readyzProbe, readGatewayLockMeta = origPgrep, origHealthz, origReadyz, origLock
		resetModelCatalogForTest()
		resetLiveSessionModelCacheForTest()
	})
}

func benchWriteJSON(tb testing.TB, path string, v any) {
	tb.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		tb.Fatal(err)
	}
}

// buildLegacyBenchState writes a realistic legacy state dir under root and
// returns the openclaw state path. Timestamps are anchored at now so sessions
// fall inside the 24h activity window.
func buildLegacyBenchState(tb testing.TB, root string, now time.Time) string {
	tb.Helper()
	openclawPath := filepath.Join(root, "openclaw")
	for _, dir := range []string{"cron", "logs"} {
		if err := os.MkdirAll(filepath.Join(openclawPath, dir), 0o755); err != nil {
			tb.Fatal(err)
		}
	}
	benchWriteJSON(tb, filepath.Join(openclawPath, "openclaw.json"), map[string]any{
		"agents": map[string]any{
			"defaults": map[string]any{
				"model": map[string]any{
					"primary":   "anthropic/claude-opus-4-6",
					"fallbacks": []any{"openai/gpt-5", "google/gemini-2.5-pro"},
				},
				"models": map[string]any{
					"anthropic/claude-opus-4-6": map[string]any{"alias": "opus"},
					"openai/gpt-5":              map[string]any{"alias": "gpt5"},
					"google/gemini-2.5-pro":     map[string]any{},
				},
				"compaction": map[string]any{"mode": "safeguard"},
			},
			"list": []any{
				map[string]any{"id": "main"},
				map[string]any{"id": "work", "model": "openai/gpt-5"},
			},
		},
		"channels": map[string]any{"telegram": map[string]any{"enabled": true}},
		"bindings": []any{
			map[string]any{"agentId": "work", "match": map[string]any{"channel": "telegram", "peer": map[string]any{"kind": "group", "id": "-1001"}}},
		},
	})

	models := []struct{ provider, id string }{
		{"anthropic", "claude-opus-4-6"},
		{"openai", "gpt-5"},
		{"google", "gemini-2.5-pro"},
		{"kimi-coding", "k2p5"},
	}
	for ai, agent := range benchAgents {
		sessDir := filepath.Join(openclawPath, "agents", agent, "sessions")
		if err := os.MkdirAll(sessDir, 0o755); err != nil {
			tb.Fatal(err)
		}
		store := map[string]any{}
		for i := range benchSessionsPerAgent {
			sid := fmt.Sprintf("%08x-0000-4000-8000-%012x", ai, i)
			var key string
			switch i % 5 {
			case 0:
				key = fmt.Sprintf("agent:%s:cron:job-%d", agent, i)
			case 1:
				key = fmt.Sprintf("agent:%s:subagent:%s", agent, sid)
			case 2:
				key = fmt.Sprintf("agent:%s:telegram:group:-100%d", agent, i)
			case 3:
				key = fmt.Sprintf("agent:%s:telegram:direct:%d", agent, 5000+i)
			default:
				key = fmt.Sprintf("agent:%s:main-%d", agent, i)
			}
			// Spread activity over ~26h so a slice falls outside the 24h window;
			// offsets avoid the 30-minute "active" boundary, and the per-agent
			// second keeps updatedAt unique so the session order is deterministic.
			updated := now.Add(-time.Duration(i*16)*time.Minute - 7*time.Minute - time.Duration(ai)*time.Second)
			entry := map[string]any{
				"sessionId":     sid,
				"updatedAt":     float64(updated.UnixMilli()),
				"contextTokens": float64(200000),
				"totalTokens":   float64(1000 * (i + 1)),
				"label":         fmt.Sprintf("Session %d id:%d", i, 90000+i),
				"subject":       fmt.Sprintf("Topic %d", i),
				"origin":        map[string]any{"label": fmt.Sprintf("origin-%d", i)},
			}
			if i%3 == 0 {
				m := models[i%len(models)]
				entry["model"] = m.provider + "/" + m.id
			}
			store[key] = entry
			writeBenchTranscript(tb, filepath.Join(sessDir, sid+".jsonl"), now, i, models[(i+ai)%len(models)], i%7 != 0)
		}
		benchWriteJSON(tb, filepath.Join(sessDir, "sessions.json"), store)
	}

	jobs := make([]any, 0, benchCronJobs)
	for i := range benchCronJobs {
		jobs = append(jobs, map[string]any{
			"id":       fmt.Sprintf("job-%d", i),
			"name":     fmt.Sprintf("Job %d", i),
			"enabled":  i%4 != 0,
			"schedule": map[string]any{"kind": "cron", "expr": "*/15 * * * *", "tz": "UTC"},
			"payload":  map[string]any{"kind": "agentTurn", "model": "anthropic/claude-opus-4-6"},
			"state": map[string]any{
				"lastRunAtMs":       float64(now.Add(-time.Hour).UnixMilli()),
				"nextRunAtMs":       float64(now.Add(time.Hour).UnixMilli()),
				"lastRunStatus":     "ok",
				"lastDurationMs":    float64(1234),
				"consecutiveErrors": float64(i % 5),
			},
		})
	}
	benchWriteJSON(tb, filepath.Join(openclawPath, "cron", "jobs.json"), map[string]any{"jobs": jobs})
	writeBenchGatewayLog(tb, filepath.Join(openclawPath, "logs", "gateway.log"), now, benchGatewayLogLines)
	return openclawPath
}

// writeBenchTranscript writes one JSONL transcript. The model_change record
// sits at the top, like real transcripts, so a backwards scan must walk the
// whole file; withModel=false omits it entirely.
func writeBenchTranscript(tb testing.TB, path string, now time.Time, seed int, m struct{ provider, id string }, withModel bool) {
	tb.Helper()
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	w := bufio.NewWriter(f)
	_, _ = fmt.Fprintf(w, `{"type":"session","version":3,"id":"s-%d","timestamp":%q}`+"\n", seed, now.Add(-48*time.Hour).UTC().Format(time.RFC3339Nano))
	if withModel {
		_, _ = fmt.Fprintf(w, `{"type":"model_change","id":"mc-%d","provider":%q,"modelId":%q}`+"\n", seed, m.provider, m.id)
	}
	for j := range benchTranscriptLines {
		ts := now.Add(-time.Duration(benchTranscriptLines-j) * 13 * time.Minute).UTC().Format(time.RFC3339Nano)
		if j%2 == 0 {
			_, _ = fmt.Fprintf(w, `{"type":"message","id":"u-%d","timestamp":%q,"message":{"role":"user","content":[{"type":"text","text":"please look at item %d and summarise the result in detail"}]}}`+"\n", j, ts, j)
			continue
		}
		_, _ = fmt.Fprintf(w, `{"type":"message","id":"a-%d","timestamp":%q,"message":{"role":"assistant","content":[{"type":"text","text":"Here is the summary for item %d with several sentences of output text."}],"provider":%q,"model":%q,"usage":{"input":%d,"output":%d,"cacheRead":%d,"cacheWrite":0,"totalTokens":%d,"cost":{"input":0.01,"output":0.02,"cacheRead":0.001,"cacheWrite":0,"total":%.4f}},"stopReason":"stop"}}`+"\n",
			j, ts, j, m.provider, m.id, 1000+j, 300+j, 5000+j, 6300+3*j, 0.031+float64(j)*0.0001+float64(len(m.provider+m.id))*0.001)
	}
	if err := w.Flush(); err != nil {
		tb.Fatal(err)
	}
	if err := f.Close(); err != nil {
		tb.Fatal(err)
	}
}

// writeBenchGatewayLog writes n mixed-format gateway lines: bracketed plain
// text, JSON-ish payloads, stack-trace continuations, and error lines carrying
// UUIDs, ids, and numbers for error-signature grouping.
func writeBenchGatewayLog(tb testing.TB, path string, now time.Time, n int) {
	tb.Helper()
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	w := bufio.NewWriter(f)
	start := now.Add(-time.Duration(n) * time.Second)
	for i := range n {
		ts := start.Add(time.Duration(i) * time.Second).UTC().Format("2006-01-02T15:04:05.000Z")
		switch i % 10 {
		case 0:
			_, _ = fmt.Fprintf(w, "%s [gateway] [error] request %08x-1234-4abc-8def-%012x failed: timeout after %dms session=%x\n", ts, i, i, 3000+i%7, i)
		case 1:
			_, _ = fmt.Fprintf(w, "    at handler (/usr/lib/node_modules/openclaw/dist/index.js:%d:%d)\n", 1000+i%50, i%80)
		case 2:
			_, _ = fmt.Fprintf(w, "%s [telegram] warn: stale update id:%d skipped (no retry)\n", ts, 700000+i)
		case 3:
			_, _ = fmt.Fprintf(w, "%s [ws] client connected pid=%d from 127.0.0.1:%d\n", ts, 4000+i%13, 50000+i%1000)
		default:
			_, _ = fmt.Fprintf(w, "%s [agent] run %d completed in %dms tokens=%d model=anthropic/claude-opus-4-6\n", ts, i, 200+i%900, 1000+i)
		}
	}
	if err := w.Flush(); err != nil {
		tb.Fatal(err)
	}
	if err := f.Close(); err != nil {
		tb.Fatal(err)
	}
}

func benchCollectorConfig() appconfig.Config {
	cfg := appconfig.Default()
	cfg.Bot.Name = "Bench"
	cfg.Timezone = "UTC"
	cfg.Refresh.IntervalSeconds = 30
	cfg.AI.GatewayPort = 0
	return cfg
}

type benchCollectorEnv struct {
	dashboardDir, openclawPath string
	cfg                        appconfig.Config
}

func newBenchCollectorEnv(tb testing.TB) benchCollectorEnv {
	tb.Helper()
	stubCollectorSeams(tb)
	root := tb.TempDir()
	env := benchCollectorEnv{
		dashboardDir: filepath.Join(root, "dashboard"),
		openclawPath: buildLegacyBenchState(tb, root, time.Now()),
		cfg:          benchCollectorConfig(),
	}
	if err := os.MkdirAll(env.dashboardDir, 0o755); err != nil {
		tb.Fatal(err)
	}
	// Warm the token-usage cache and previous snapshot so iterations measure
	// the steady-state refresh, not the first cold parse.
	if err := RunRefreshCollector(context.Background(), env.dashboardDir, env.openclawPath, env.cfg); err != nil {
		tb.Fatal(err)
	}
	return env
}

func BenchmarkRunRefreshCollectorLegacy(b *testing.B) {
	env := newBenchCollectorEnv(b)
	ctx := context.Background()
	for b.Loop() {
		if err := RunRefreshCollector(ctx, env.dashboardDir, env.openclawPath, env.cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCollectDashboardDataLegacy(b *testing.B) {
	env := newBenchCollectorEnv(b)
	ctx := context.Background()
	for b.Loop() {
		_ = collectDashboardData(ctx, env.dashboardDir, env.openclawPath, env.cfg)
	}
}

// BenchmarkCollectDashboardDataLegacySlowCLI gives every CLI and probe seam a
// fixed latency (subprocesses run /bin/sleep) and drops the live session model
// cache each iteration, as a refresh does once its TTL lapses, so the result
// shows how much subprocess latency the collector overlaps.
func BenchmarkCollectDashboardDataLegacySlowCLI(b *testing.B) {
	env := newBenchCollectorEnv(b)
	const latency = 30 * time.Millisecond
	sleep := func() { time.Sleep(latency) }
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sleep", strconv.FormatFloat(latency.Seconds(), 'f', -1, 64))
	}
	fetchLiveSessionModels = func(context.Context) map[string]string { sleep(); return map[string]string{} }
	channelStatusCollector = func(context.Context, func(context.Context, string, ...string) *exec.Cmd, func() string) (map[string]any, bool) {
		sleep()
		return nil, false
	}
	readyzProbe = func(context.Context, int) ([]string, bool) { sleep(); return nil, false }
	ctx := context.Background()
	for b.Loop() {
		resetLiveSessionModelCacheForTest()
		_ = collectDashboardData(ctx, env.dashboardDir, env.openclawPath, env.cfg)
	}
}

func BenchmarkCollectSessionsLegacy(b *testing.B) {
	env := newBenchCollectorEnv(b)
	ctx := context.Background()
	basePath := filepath.Join(env.openclawPath, "agents")
	stores := loadSessionStores(basePath)
	now := time.Now()
	for b.Loop() {
		_ = collectSessions(ctx, stores, basePath, time.UTC, now, map[string]string{}, map[string]string{}, 30*time.Second)
	}
}

func BenchmarkMarshalDataJSON(b *testing.B) {
	env := newBenchCollectorEnv(b)
	data := collectDashboardData(context.Background(), env.dashboardDir, env.openclawPath, env.cfg)
	for b.Loop() {
		if _, err := json.MarshalIndent(data, "", "  "); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadMergedLogs(b *testing.B) {
	root := b.TempDir()
	openclawPath := filepath.Join(root, "openclaw")
	if err := os.MkdirAll(filepath.Join(openclawPath, "logs"), 0o755); err != nil {
		b.Fatal(err)
	}
	writeBenchGatewayLog(b, filepath.Join(openclawPath, "logs", "gateway.log"), time.Now(), benchGatewayLogLines)
	for _, limit := range []int{200, 1000} {
		b.Run(fmt.Sprintf("limit=%d", limit), func(b *testing.B) {
			for b.Loop() {
				if _, err := ReadMergedLogsWithUnit(openclawPath, []string{"logs/gateway.log"}, limit, ""); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkNormalizeErrorSignature(b *testing.B) {
	msgs := []string{
		"request 0000abcd-1234-4abc-8def-00000000abcd failed: timeout after 3005ms session=1f4",
		"stale update id:700123 skipped (no retry)",
		"Error: connect ECONNREFUSED 127.0.0.1:18789 job=deadbeef trace:abc-123",
		"   Provider   returned 429   rate limit\tretry in 30s   ",
		"2026-09-23T10:11:12.345Z nested timestamp with pid 4321",
	}
	for b.Loop() {
		for _, m := range msgs {
			_ = NormalizeErrorSignature(m)
		}
	}
}

// TestBenchFixtureDataJSONDump writes the collector output for the bench
// fixture to $APPREFRESH_GOLDEN_OUT (skipped otherwise). The fixture state is
// generated once into $APPREFRESH_GOLDEN_STATE (reused when it exists) so two
// builds can be compared byte-for-byte on identical input.
func TestBenchFixtureDataJSONDump(t *testing.T) {
	out := os.Getenv("APPREFRESH_GOLDEN_OUT")
	stateRoot := os.Getenv("APPREFRESH_GOLDEN_STATE")
	if out == "" || stateRoot == "" {
		t.Skip("set APPREFRESH_GOLDEN_OUT and APPREFRESH_GOLDEN_STATE to dump the fixture snapshot")
	}
	stubCollectorSeams(t)
	openclawPath := filepath.Join(stateRoot, "openclaw")
	if _, err := os.Stat(openclawPath); err != nil {
		buildLegacyBenchState(t, stateRoot, time.Now())
	}
	dashboardDir := t.TempDir()
	cfg := benchCollectorConfig()
	// Two passes: the second exercises the warm token cache and the
	// previous-snapshot retention path.
	for range 2 {
		if err := RunRefreshCollector(context.Background(), dashboardDir, openclawPath, cfg); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dashboardDir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for line := range strings.Lines(string(data)) {
		if strings.Contains(line, `"lastRefresh`) || strings.Contains(line, `"collectedAt"`) || strings.Contains(line, `"attemptedAt"`) {
			continue
		}
		lines = append(lines, line)
	}
	if err := os.WriteFile(out, []byte(strings.Join(lines, "")), 0o600); err != nil {
		t.Fatal(err)
	}
}
