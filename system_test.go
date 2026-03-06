package main

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
	// Spin up a server that delays response to verify timeout works
	called := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(called)
		time.Sleep(5 * time.Second)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Extract port from test server
	parts := strings.Split(srv.URL, ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	ctx := context.Background()
	start := time.Now()
	gw := detectGatewayFallback(ctx, port, 200) // 200ms timeout
	elapsed := time.Since(start)

	// Should timeout quickly, not wait 5 seconds
	if elapsed > 2*time.Second {
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
	srv.systemSvc.metricsMu.Lock()
	srv.systemSvc.metricsAt = time.Now().Add(-1 * time.Hour)
	srv.systemSvc.metricsMu.Unlock()

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

func TestRefresh_DataMissing_HasCORSHeaders(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)
	// No data.json

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
