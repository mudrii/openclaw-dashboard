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
