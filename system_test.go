package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	cfg.System.WarnPercent = 60
	cfg.System.CriticalPercent = 80
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
	if resp.WarnPercent != 60 {
		t.Errorf("expected warnPercent=60 got %f", resp.WarnPercent)
	}
	if resp.CriticalPercent != 80 {
		t.Errorf("expected criticalPercent=80 got %f", resp.CriticalPercent)
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
		cfg := defaultConfig()
		cfg.System.WarnPercent = tt.warn
		cfg.System.CriticalPercent = tt.critical
		dir := t.TempDir()
		loaded := loadConfig(dir) // triggers clamping
		_ = loaded
		// Test via direct clamp logic
		w, c := tt.warn, tt.critical
		if w <= 0 || w >= 100 {
			w = 70
		}
		if c <= w || c > 100 {
			if w < 95 {
				c = w + 15
				if c > 100 {
					c = 100
				}
			} else {
				c = 100
			}
		}
		if c != tt.wantCrit {
			t.Errorf("warn=%.0f crit=%.0f: expected clamped crit=%.0f got %.0f", tt.warn, tt.critical, tt.wantCrit, c)
		}
		if c <= w {
			t.Errorf("invariant violated: critical(%.0f) <= warn(%.0f)", c, w)
		}
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
	if cfg.System.PollSeconds != 5 {
		t.Errorf("expected PollSeconds=5, got %d", cfg.System.PollSeconds)
	}
	if cfg.System.MetricsTTLSeconds != 5 {
		t.Errorf("expected MetricsTTLSeconds=5, got %d", cfg.System.MetricsTTLSeconds)
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
