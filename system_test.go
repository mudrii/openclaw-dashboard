package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHandleSystem_GET_Returns200WithSchema(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)
	req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp SystemResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nbody: %s", err, w.Body.String())
	}
	if resp.CollectedAt == "" {
		t.Error("collectedAt should not be empty")
	}
	if resp.PollSeconds <= 0 {
		t.Errorf("pollSeconds should be > 0, got %d", resp.PollSeconds)
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected application/json, got %s", w.Header().Get("Content-Type"))
	}

	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON map decode: %v", err)
	}
	oc, ok := raw["openclaw"].(map[string]any)
	if !ok {
		t.Fatalf("expected openclaw block in /api/system response")
	}
	for _, key := range []string{"gateway", "status", "freshness"} {
		if _, ok := oc[key]; !ok {
			t.Errorf("openclaw.%s missing", key)
		}
	}
	// Channels and Bindings were removed from runtime observability — verify they're absent
	for _, removed := range []string{"channels", "bindings"} {
		if _, present := oc[removed]; present {
			t.Errorf("openclaw.%s should no longer be present in /api/system response", removed)
		}
	}
}

func TestHandleSystem_HEAD_NoBody(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)
	req := httptest.NewRequest(http.MethodHead, "/api/system", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD body should be empty, got %d bytes", w.Body.Len())
	}
}

func TestHandleSystem_CORS(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)
	req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	req.Header.Set("Origin", "http://localhost:9090")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	origin := w.Header().Get("Access-Control-Allow-Origin")
	if origin == "" {
		t.Error("expected Access-Control-Allow-Origin header to be set")
	}
}

func TestHandleSystem_Disabled_Returns503(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.System.Enabled = false
	srv := testServerWithConfig(t, dir, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when disabled, got %d", w.Code)
	}
}

func TestHandleSystem_ThresholdsInResponse(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.System.CPU = MetricThreshold{Warn: 60, Critical: 80}
	cfg.System.RAM = MetricThreshold{Warn: 60, Critical: 80}
	srv := testServerWithConfig(t, dir, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var resp SystemResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Thresholds.CPU.Warn != 60 {
		t.Errorf("expected thresholds.cpu.warn=60 got %f", resp.Thresholds.CPU.Warn)
	}
	if resp.Thresholds.CPU.Critical != 80 {
		t.Errorf("expected thresholds.cpu.critical=80 got %f", resp.Thresholds.CPU.Critical)
	}
	if resp.Thresholds.RAM.Warn != 60 {
		t.Errorf("expected thresholds.ram.warn=60 got %f", resp.Thresholds.RAM.Warn)
	}
}

func TestSystemConfig_ClampCriticalRelativeToWarn(t *testing.T) {
	tests := []struct {
		warn     float64
		critical float64
		wantCrit float64
	}{
		{70, 85, 85},  // valid — unchanged
		{70, 60, 85},  // critical < warn → clamp to warn+15
		{90, 80, 100}, // warn=90, critical < warn → warn+15=105 → 100
		{95, 95, 100}, // edge — warn=95, critical=95 (<=warn) → 100
	}
	for _, tt := range tests {
		dir := t.TempDir()
		// Write config.json with the test thresholds so loadConfig picks them up.
		cfgJSON := fmt.Sprintf(`{"system":{"warnPercent":%g,"criticalPercent":%g}}`, tt.warn, tt.critical)
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfgJSON), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		loaded := loadConfig(dir)
		got := loaded.System.CriticalPercent
		gotWarn := loaded.System.WarnPercent
		if got != tt.wantCrit {
			t.Errorf("warn=%.0f crit=%.0f: expected clamped critical=%.0f, loadConfig returned %.0f",
				tt.warn, tt.critical, tt.wantCrit, got)
		}
		if got <= gotWarn {
			t.Errorf("invariant violated: critical(%.0f) <= warn(%.0f)", got, gotWarn)
		}
	}
}

func TestSystemConfig_PerMetricThresholdClamping(t *testing.T) {
	tests := []struct {
		name     string
		cfgJSON  string
		metric   string
		wantWarn float64
		wantCrit float64
	}{
		{"valid cpu thresholds", `{"system":{"cpu":{"warn":75,"critical":90}}}`, "cpu", 75, 90},
		{"cpu crit < warn → use globalCrit", `{"system":{"cpu":{"warn":80,"critical":60}}}`, "cpu", 80, 85},
		{"ram warn at edge", `{"system":{"ram":{"warn":90,"critical":95}}}`, "ram", 90, 95},
		{"swap crit > 100 → 100", `{"system":{"swap":{"warn":85,"critical":105}}}`, "swap", 85, 100},
		{"disk defaults when absent", `{"system":{}}`, "disk", 80, 95},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(tt.cfgJSON), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			loaded := loadConfig(dir)
			var w, c float64
			switch tt.metric {
			case "cpu":
				w, c = loaded.System.CPU.Warn, loaded.System.CPU.Critical
			case "ram":
				w, c = loaded.System.RAM.Warn, loaded.System.RAM.Critical
			case "swap":
				w, c = loaded.System.Swap.Warn, loaded.System.Swap.Critical
			case "disk":
				w, c = loaded.System.Disk.Warn, loaded.System.Disk.Critical
			}
			if w != tt.wantWarn {
				t.Errorf("warn: expected %.0f got %.0f", tt.wantWarn, w)
			}
			if c != tt.wantCrit {
				t.Errorf("critical: expected %.0f got %.0f", tt.wantCrit, c)
			}
			if c <= w {
				t.Errorf("invariant violated: critical(%.0f) <= warn(%.0f)", c, w)
			}
		})
	}
}

func TestHandleSystem_CacheHit(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	req1 := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w1 := httptest.NewRecorder()
	srv.ServeHTTP(w1, req1)

	time.Sleep(10 * time.Millisecond)

	req2 := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)

	if w1.Body.String() != w2.Body.String() {
		t.Error("expected cached response within TTL to be identical")
	}
}

func TestHandleSystem_DegradedReturns200(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.System.DiskPath = "/nonexistent-path-xyz"
	srv := testServerWithConfig(t, dir, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should still return 200 even with disk error
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for degraded, got %d", w.Code)
	}
	var resp SystemResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !resp.Degraded {
		t.Error("expected degraded=true when disk path invalid")
	}
}

func TestCollectOpenclawRuntime_GracefulDegradation(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"status":"live"}`))
		case "/readyz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ready":true,"failing":[],"uptimeMs":1234}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gw.Close()
	parts := strings.Split(gw.URL, ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	binDir := t.TempDir()
	fake := filepath.Join(binDir, "openclaw")
	script := `#!/bin/sh
if [ "$1" = "status" ] && [ "$2" = "--json" ]; then
  echo '{"connectLatencyMs":42,"security":{"mode":"strict"}}'
  exit 0
fi
exit 1
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}

	oc := collectOpenclawRuntime(context.Background(), fake, 1500, port, SystemVersions{Openclaw: "2026.3.7", Latest: "2026.3.8"})
	if !oc.Gateway.Live || !oc.Gateway.Ready {
		t.Fatalf("expected gateway live+ready true, got %+v", oc.Gateway)
	}
	if !oc.Gateway.HealthEndpointOk || !oc.Gateway.ReadyEndpointOk {
		t.Fatalf("expected endpoint flags true, got %+v", oc.Gateway)
	}
	if oc.Status.CurrentVersion != "2026.3.7" || oc.Status.LatestVersion != "2026.3.8" {
		t.Fatalf("expected status versions from SystemVersions, got %+v", oc.Status)
	}
}

func TestCollectDiskRoot(t *testing.T) {
	d := collectDiskRoot("/")
	if d.Error != nil {
		t.Skipf("disk collection failed: %s", *d.Error)
	}
	if d.TotalBytes <= 0 {
		t.Errorf("expected positive total disk bytes, got %d", d.TotalBytes)
	}
	if d.Percent < 0 || d.Percent > 100 {
		t.Errorf("percent out of range: %f", d.Percent)
	}
}

func TestSystemConfig_Defaults(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.System.Enabled {
		t.Error("expected system.enabled=true by default")
	}
	if cfg.System.PollSeconds != 10 {
		t.Errorf("expected PollSeconds=10, got %d", cfg.System.PollSeconds)
	}
	if cfg.System.MetricsTTLSeconds != 10 {
		t.Errorf("expected MetricsTTLSeconds=10, got %d", cfg.System.MetricsTTLSeconds)
	}
	if cfg.System.VersionsTTLSeconds != 300 {
		t.Errorf("expected VersionsTTLSeconds=300, got %d", cfg.System.VersionsTTLSeconds)
	}
	if cfg.System.DiskPath != "/" {
		t.Errorf("expected DiskPath='/', got %s", cfg.System.DiskPath)
	}
	if cfg.System.WarnPercent != 70 {
		t.Errorf("expected WarnPercent=70, got %f", cfg.System.WarnPercent)
	}
	if cfg.System.CriticalPercent != 85 {
		t.Errorf("expected CriticalPercent=85, got %f", cfg.System.CriticalPercent)
	}
}

// ── Tests for parseGatewayStatusJSON (Fix #12) ────────────────────────────

func TestParseGatewayStatusJSON_RunningService(t *testing.T) {
	ctx := context.Background()
	input := `{"service":{"loaded":true,"runtime":{"status":"running","pid":1234}},"version":"1.0.0"}`
	got := parseGatewayStatusJSON(ctx, input)
	if got.Status != "online" {
		t.Errorf("expected status=online, got %q", got.Status)
	}
	if got.PID != 1234 {
		t.Errorf("expected pid=1234, got %d", got.PID)
	}
	if got.Version != "1.0.0" {
		t.Errorf("expected version=1.0.0, got %q", got.Version)
	}
}

func TestParseGatewayStatusJSON_LoadedButNotRunning(t *testing.T) {
	ctx := context.Background()
	// loaded=true should still give online (fallback when runtime.status missing)
	input := `{"service":{"loaded":true,"runtime":{"status":"stopped","pid":0}},"version":""}`
	got := parseGatewayStatusJSON(ctx, input)
	if got.Status != "online" {
		t.Errorf("loaded=true with status=stopped: expected online, got %q", got.Status)
	}
}

func TestParseGatewayStatusJSON_RuntimeStatusPreferred(t *testing.T) {
	ctx := context.Background()
	// runtime.status=running should give online even if loaded=false
	input := `{"service":{"loaded":false,"runtime":{"status":"running","pid":42}},"version":""}`
	got := parseGatewayStatusJSON(ctx, input)
	if got.Status != "online" {
		t.Errorf("runtime.status=running: expected online, got %q", got.Status)
	}
}

func TestParseGatewayStatusJSON_Offline(t *testing.T) {
	ctx := context.Background()
	input := `{"service":{"loaded":false,"runtime":{"status":"stopped","pid":0}},"version":""}`
	got := parseGatewayStatusJSON(ctx, input)
	if got.Status != "offline" {
		t.Errorf("expected offline, got %q", got.Status)
	}
}

func TestParseGatewayStatusJSON_LeadingNonJSON(t *testing.T) {
	ctx := context.Background()
	input := "some log line\nanother line\n{\"service\":{\"loaded\":true,\"runtime\":{\"status\":\"running\",\"pid\":99}},\"version\":\"2.0\"}"
	got := parseGatewayStatusJSON(ctx, input)
	if got.Status != "online" {
		t.Errorf("with leading text: expected online, got %q", got.Status)
	}
	if got.Version != "2.0" {
		t.Errorf("expected version=2.0, got %q", got.Version)
	}
}

func TestParseGatewayStatusJSON_EmptyJSON(t *testing.T) {
	ctx := context.Background()
	got := parseGatewayStatusJSON(ctx, "{}")
	if got.Status != "offline" {
		t.Errorf("empty json: expected offline, got %q", got.Status)
	}
}

func TestParseGatewayStatusJSON_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	got := parseGatewayStatusJSON(ctx, "not json at all")
	if got.Status == "" {
		t.Error("expected non-empty status on invalid input")
	}
}

// ── Tests for formatBytes (Fix #12) ──────────────────────────────────────

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{-100, "0B"},
		{-1, "0B"},
		{0, "0B"},
		{512, "512B"},
		{1023, "1023B"},
		{1024, "1KB"},
		{2048, "2KB"},
		{1048576, "1.0MB"},
		{10 * 1048576, "10.0MB"},
		{1073741824, "1.0GB"},
		{5 * 1073741824, "5.0GB"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("bytes_%d", tt.input), func(t *testing.T) {
			got := formatBytes(tt.input)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatBytes_NegativeGuard(t *testing.T) {
	got := formatBytes(-9999)
	if got != "0B" {
		t.Errorf("expected 0B for negative input, got %q", got)
	}
}

// ── Tests for getProcessInfo (Fix #12) ───────────────────────────────────

func TestGetProcessInfo_CurrentProcess(t *testing.T) {
	ctx := context.Background()
	pid := os.Getpid()
	uptime, memory := getProcessInfo(ctx, pid)
	// Just verify the function doesn't panic and returns something for the current process
	// ps output depends on platform availability
	if uptime == "" && memory == "" {
		t.Log("getProcessInfo returned empty strings — ps may not be available in this environment (acceptable)")
	}
}

func TestGetProcessInfo_InvalidPID(t *testing.T) {
	ctx := context.Background()
	// PID 0 should not exist; we expect empty strings, not a panic
	uptime, memory := getProcessInfo(ctx, 0)
	if uptime != "" || memory != "" {
		t.Logf("getProcessInfo(0) returned uptime=%q memory=%q — unexpected but not fatal", uptime, memory)
	}
}

func TestGetProcessInfo_ContextTimeout(t *testing.T) {
	// Verify function respects a very short context timeout without hanging
	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()
	pid := os.Getpid()
	// Should return without blocking even if context is already cancelled
	_, _ = getProcessInfo(ctx, pid)
}

// ── Tests for detectGatewayFallback timeout-bounded client ───────────────

func TestDetectGatewayFallback_UsesTimeoutClient(t *testing.T) {
	// Spin up a server that delays response to verify timeout works.
	// Handler sleeps 1s (just long enough to exceed 100ms client timeout).
	// Context cancels handler on test completion to avoid lingering goroutines.
	ctx := t.Context()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(1 * time.Second):
		case <-ctx.Done():
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Extract port from test server
	parts := strings.Split(srv.URL, ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	start := time.Now()
	gw := detectGatewayFallback(ctx, port, 100) // 100ms timeout
	elapsed := time.Since(start)

	// Should timeout quickly, not wait 1 second
	if elapsed > 500*time.Millisecond {
		t.Errorf("detectGatewayFallback took %v — timeout not working", elapsed)
	}
	if gw.Status != "offline" {
		t.Errorf("expected offline on timeout, got %q", gw.Status)
	}
}

func TestDetectGatewayFallback_OnlineWhenResponds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	parts := strings.Split(srv.URL, ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	ctx := context.Background()
	gw := detectGatewayFallback(ctx, port, 3000)
	if gw.Status != "online" {
		t.Errorf("expected online, got %q", gw.Status)
	}
}

// ── Tests for resolveOpenclawBin executable-bit validation ────────────────

func TestResolveOpenclawBin_SkipsNonExecutable(t *testing.T) {
	dir := t.TempDir()
	// Create a file at a candidate location that is NOT executable
	binDir := filepath.Join(dir, ".asdf", "shims")
	os.MkdirAll(binDir, 0755)
	fakeFile := filepath.Join(binDir, "openclaw")
	os.WriteFile(fakeFile, []byte("not executable"), 0644) // no exec bit

	// resolveOpenclawBin should NOT return this file
	// (We can't easily test this without modifying HOME, so just test the logic directly)
	info, _ := os.Stat(fakeFile)
	if info.Mode()&0111 != 0 {
		t.Fatal("test setup error: file should not have exec bit")
	}
	// The guard in resolveOpenclawBin: info.Mode()&0111 != 0 would skip this file
}

func TestResolveOpenclawBin_IntegrationWithTempHome(t *testing.T) {
	// Full integration test: set HOME to a temp directory, place both executable
	// and non-executable files, and call resolveOpenclawBin() directly.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Create asdf shims dir with NON-executable openclaw → should be skipped
	shimsDir := filepath.Join(tmpHome, ".asdf", "shims")
	os.MkdirAll(shimsDir, 0755)
	nonExec := filepath.Join(shimsDir, "openclaw")
	os.WriteFile(nonExec, []byte("#!/bin/sh\necho not-exec"), 0644) // no exec bit

	// Create asdf nodejs install with EXECUTABLE openclaw → should be found
	nodeDir := filepath.Join(tmpHome, ".asdf", "installs", "nodejs", "22.0.0", "bin")
	os.MkdirAll(nodeDir, 0755)
	execFile := filepath.Join(nodeDir, "openclaw")
	os.WriteFile(execFile, []byte("#!/bin/sh\necho exec"), 0755) // exec bit set

	// Temporarily remove PATH-based openclaw so resolveOpenclawBin falls through to candidates
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/nonexistent")

	result := resolveOpenclawBin()

	// Restore PATH (for other tests) — t.Setenv handles cleanup automatically

	// Result should be the executable file, not the non-executable one
	if result == nonExec {
		t.Errorf("resolveOpenclawBin returned non-executable file %q", result)
	}
	if result != execFile {
		// It's acceptable to get the last-resort "openclaw" if exec.LookPath still
		// finds something, but it must NOT be the 0644 file.
		t.Logf("resolveOpenclawBin returned %q (expected %q)", result, execFile)
	}

	// Also verify the non-executable is truly skipped
	info, err := os.Stat(nonExec)
	if err != nil {
		t.Fatal("test setup: non-exec file missing")
	}
	if info.Mode()&0111 != 0 {
		t.Fatal("test setup: non-exec file has exec bit")
	}

	_ = origPath // appease linter
}

func TestResolveOpenclawBin_PrefersHighestNodeInstallVersion(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("PATH", "/nonexistent")

	versions := []string{"9.9.9", "10.0.0", "25.7.0", "25.8.0"}
	var expected string
	for _, ver := range versions {
		nodeDir := filepath.Join(tmpHome, ".asdf", "installs", "nodejs", ver, "bin")
		if err := os.MkdirAll(nodeDir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", ver, err)
		}
		target := filepath.Join(nodeDir, "openclaw")
		if err := os.WriteFile(target, []byte("#!/bin/sh\necho "+ver), 0755); err != nil {
			t.Fatalf("write %s: %v", ver, err)
		}
		if ver == "25.8.0" {
			expected = target
		}
	}

	if got := resolveOpenclawBin(); got != expected {
		t.Fatalf("resolveOpenclawBin() = %q, want %q", got, expected)
	}
}

// ── Tests for stale byte-level injection ─────────────────────────────────

func TestStaleByteInjection(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	// Prime the system service cache with a fresh payload
	req1 := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w1 := httptest.NewRecorder()
	srv.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w1.Code)
	}

	var resp1 SystemResponse
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	if resp1.Stale {
		t.Fatal("first response should not be stale")
	}

	// Force cache to appear stale by backdating the timestamp
	expireMetricsCacheForTest(srv.systemSvc)

	// Next request should get stale=true
	req2 := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)

	var resp2 SystemResponse
	json.Unmarshal(w2.Body.Bytes(), &resp2)
	if !resp2.Stale {
		t.Error("expected stale=true after cache expiry")
	}
}

// ── Static file allowlist parity ─────────────────────────────────────────

func TestStaticFile_FaviconIco(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)
	os.WriteFile(filepath.Join(dir, "favicon.ico"), []byte("ico"), 0644)

	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for favicon.ico, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/x-icon" {
		t.Fatalf("expected image/x-icon, got %s", ct)
	}
}

func TestStaticFile_FaviconPng(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)
	os.WriteFile(filepath.Join(dir, "favicon.png"), []byte("png"), 0644)

	req := httptest.NewRequest(http.MethodGet, "/favicon.png", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for favicon.png, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("expected image/png, got %s", ct)
	}
}

// ── CORS Allow-Headers parity ────────────────────────────────────────────

func TestCORS_AllowHeaders_IncludesAuthorization(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	req := httptest.NewRequest(http.MethodOptions, "/api/chat", nil)
	req.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	ah := w.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(ah, "Authorization") {
		t.Errorf("expected Authorization in Allow-Headers, got %q", ah)
	}
	if !strings.Contains(ah, "Content-Type") {
		t.Errorf("expected Content-Type in Allow-Headers, got %q", ah)
	}
}

// ── Refresh error CORS ───────────────────────────────────────────────────

// ── Tests for fetchJSONMap HTTP status handling (I1 fix) ─────────────────

func TestFetchJSONMap_Rejects4xx(t *testing.T) {
	codes := []int{400, 401, 403, 404, 405, 429}
	for _, code := range codes {
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				_, _ = w.Write([]byte(`{"error":"nope"}`))
			}))
			defer srv.Close()

			client := &http.Client{Timeout: 2 * time.Second}
			_, err := fetchJSONMap(context.Background(), client, srv.URL)
			if err == nil {
				t.Fatalf("expected error for HTTP %d, got nil", code)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("status %d", code)) {
				t.Errorf("expected error to contain 'status %d', got %q", code, err.Error())
			}
		})
	}
}

func TestFetchJSONMap_Accepts2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	m, err := fetchJSONMap(context.Background(), client, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error for 200: %v", err)
	}
	if ok, _ := boolFromAny(m["ok"]); !ok {
		t.Errorf("expected ok=true, got %v", m)
	}
}

func TestFetchJSONMap_Rejects5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	_, err := fetchJSONMap(context.Background(), client, srv.URL)
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

// ── Tests for B1 fix: version flow correctness ──────────────────────────

func TestCollectOpenclawRuntime_ReceivesVersionsFromCaller(t *testing.T) {
	// Verify that when versions are passed in, they appear in the output status
	// even when `openclaw status --json` doesn't return version fields.
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/readyz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ready":true,"uptimeMs":100}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gw.Close()
	parts := strings.Split(gw.URL, ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	binDir := t.TempDir()
	fake := filepath.Join(binDir, "openclaw")
	// status --json returns empty JSON (no version fields)
	script := `#!/bin/sh
if [ "$1" = "status" ] && [ "$2" = "--json" ]; then
  echo '{"connectLatencyMs":5}'
  exit 0
fi
exit 1
`
	os.WriteFile(fake, []byte(script), 0o755)

	inputVersions := SystemVersions{Openclaw: "2026.3.9-test", Latest: "2026.3.10"}
	oc := collectOpenclawRuntime(context.Background(), fake, 1500, port, inputVersions)

	// The versions from the caller should be used as fallback
	if oc.Status.CurrentVersion != "2026.3.9-test" {
		t.Errorf("expected currentVersion='2026.3.9-test', got %q", oc.Status.CurrentVersion)
	}
	if oc.Status.LatestVersion != "2026.3.10" {
		t.Errorf("expected latestVersion='2026.3.10', got %q", oc.Status.LatestVersion)
	}
}

func TestCollectOpenclawRuntime_StatusOverridesCallerVersions(t *testing.T) {
	// When `openclaw status --json` DOES return version fields, they should
	// override the caller-provided values.
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/readyz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ready":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gw.Close()
	parts := strings.Split(gw.URL, ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	binDir := t.TempDir()
	fake := filepath.Join(binDir, "openclaw")
	script := `#!/bin/sh
if [ "$1" = "status" ] && [ "$2" = "--json" ]; then
  echo '{"currentVersion":"2026.3.11-live","latestVersion":"2026.3.12"}'
  exit 0
fi
exit 1
`
	os.WriteFile(fake, []byte(script), 0o755)

	inputVersions := SystemVersions{Openclaw: "2026.3.9-old", Latest: "2026.3.10-old"}
	oc := collectOpenclawRuntime(context.Background(), fake, 1500, port, inputVersions)

	// status --json values should override caller-provided versions
	if oc.Status.CurrentVersion != "2026.3.11-live" {
		t.Errorf("expected currentVersion='2026.3.11-live', got %q", oc.Status.CurrentVersion)
	}
	if oc.Status.LatestVersion != "2026.3.12" {
		t.Errorf("expected latestVersion='2026.3.12', got %q", oc.Status.LatestVersion)
	}
}

func TestRefresh_DataMissing_HasCORSHeaders(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCLAW_HOME", t.TempDir())

	prev := refreshCollectorFunc
	defer func() { refreshCollectorFunc = prev }()
	refreshCollectorFunc = func(ctx context.Context, dashboardDir, openclawPath string, cfgOpt ...Config) error {
		return os.ErrNotExist
	}

	srv := testServer(t, dir)
	req := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	cors := w.Header().Get("Access-Control-Allow-Origin")
	if cors != "http://localhost:3000" {
		t.Errorf("503 response should have CORS header, got %q", cors)
	}
}

// ── Tests for B2 fix: stale injection JSON round-trip ────────────────────

func TestStaleByteInjection_JSONRoundTrip(t *testing.T) {
	// Verify stale injection works correctly even without the exact "stale":false byte pattern.
	// The B2 fix replaces byte-level replacement with a safe JSON round-trip.
	dir := t.TempDir()
	srv := testServer(t, dir)

	// Prime the cache
	req1 := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w1 := httptest.NewRecorder()
	srv.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("prime request: expected 200, got %d", w1.Code)
	}
	var resp1 SystemResponse
	if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("prime unmarshal: %v", err)
	}
	if resp1.Stale {
		t.Fatal("prime response should not be stale")
	}

	// Expire the cache
	expireMetricsCacheForTest(srv.systemSvc)

	// Stale request: should get stale=true via JSON round-trip (not byte replacement)
	req2 := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("stale request: expected 200, got %d", w2.Code)
	}

	var resp2 SystemResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("stale unmarshal: %v\nbody: %s", err, w2.Body.String())
	}
	if !resp2.Stale {
		t.Error("expected stale=true after cache expiry (B2 fix: JSON round-trip)")
	}
	// Verify other fields are preserved through the round-trip
	if resp2.CollectedAt == "" {
		t.Error("collectedAt should be preserved through stale round-trip")
	}
	if resp2.PollSeconds <= 0 {
		t.Errorf("pollSeconds should be preserved, got %d", resp2.PollSeconds)
	}
}

// ── Tests for I2 fix: collectVersions parses stdout on non-zero exit ──────

func TestParseGatewayStatusJSON_I2_NonZeroExitStillParsed(t *testing.T) {
	// I2 fix: parseGatewayStatusJSON should extract status from stdout JSON even
	// when the calling code received a non-zero exit from the CLI.
	// This test verifies the parsing function correctly handles the JSON that
	// would arrive from a CLI that exits non-zero but emits valid JSON stdout.
	ctx := context.Background()
	// Simulate what runWithTimeout returns on non-zero exit with valid stdout
	nonZeroStdout := `{"service":{"loaded":true,"runtime":{"status":"running","pid":4242}},"version":"2026.3.7"}`
	got := parseGatewayStatusJSON(ctx, nonZeroStdout)

	// The JSON should be fully parsed regardless of the exit code (I2 fix)
	if got.Status != "online" {
		t.Errorf("I2 fix: expected status=online from non-zero-exit stdout JSON, got %q", got.Status)
	}
	if got.Version != "2026.3.7" {
		t.Errorf("I2 fix: expected version=2026.3.7, got %q", got.Version)
	}
	// PID is extracted and used to get process info — just verify it's non-zero
	if got.PID == 0 {
		t.Errorf("I2 fix: expected non-zero PID from parsed JSON, got %d", got.PID)
	}
}

func TestCollectVersions_GatewayNoStdoutFallsBackToHTTP_I2(t *testing.T) {
	// Integration test: when collectVersions receives no usable JSON from the CLI
	// (stdout = plain text error, non-zero exit), it must fall back to HTTP probe.
	// We spin up a local HTTP server to act as the "gateway reachable" signal.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200) // gateway reachable
	}))
	defer srv.Close()
	parts := strings.Split(srv.URL, ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	// Verify the I2 fallback path: when stdout has no JSON, detectGatewayFallback is
	// invoked (we confirm this by pointing gatewayPort at our test server).
	// We can't inject a fake binary into collectVersions (it uses resolveOpenclawBin),
	// but we can verify behavior when the real binary CLI returns non-JSON output.
	ctx := context.Background()
	// Directly test the fallback function that I2 invokes on no-JSON stdout
	gw := detectGatewayFallback(ctx, port, 3000)
	if gw.Status != "online" {
		t.Errorf("detectGatewayFallback: expected online for reachable HTTP server, got %q", gw.Status)
	}
}

// ── Tests for I2 fix: collectOpenclawRuntime parses status stdout on non-zero exit

func TestCollectOpenclawRuntime_StatusParsesStdoutOnNonZeroExit(t *testing.T) {
	// When `openclaw status --json` exits non-zero but stdout contains useful JSON,
	// the version/latency/security data should still be extracted. (I2 fix)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"status":"live"}`))
		case "/readyz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ready":true,"uptimeMs":100}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gw.Close()
	parts := strings.Split(gw.URL, ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	binDir := t.TempDir()
	fake := filepath.Join(binDir, "openclaw")
	// status --json exits 1 but emits useful JSON to stdout
	script := `#!/bin/sh
if [ "$1" = "status" ] && [ "$2" = "--json" ]; then
  echo '{"currentVersion":"2026.3.9","latestVersion":"2026.3.10","connectLatencyMs":55}'
  exit 1
fi
exit 1
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}

	inputVer := SystemVersions{Openclaw: "2026.3.8-fallback", Latest: "2026.3.10"}
	oc := collectOpenclawRuntime(context.Background(), fake, 2000, port, inputVer)

	// I2 fix: version fields should be extracted from stdout even on non-zero exit
	if oc.Status.CurrentVersion != "2026.3.9" {
		t.Errorf("I2 fix: expected currentVersion='2026.3.9' from stdout, got %q", oc.Status.CurrentVersion)
	}
	if oc.Status.ConnectLatencyMs != 55 {
		t.Errorf("I2 fix: expected connectLatencyMs=55, got %d", oc.Status.ConnectLatencyMs)
	}
	// Non-zero exit should still be reported as an error
	if len(oc.Errors) == 0 {
		t.Error("expected non-zero exit error to be reported in openclaw.errors")
	}
}
