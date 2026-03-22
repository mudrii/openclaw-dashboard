package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T, dir string) *Server {
	t.Helper()
	cfg := defaultConfig()
	cfg.AI.Enabled = false
	cfg.Refresh.IntervalSeconds = 1
	return NewServer(dir, "test", cfg, "", []byte("<head><body>__VERSION__</body>"), context.Background())
}

func testServerWithConfig(t *testing.T, dir string, cfg Config) *Server {
	t.Helper()
	cfg.AI.Enabled = false
	return NewServer(dir, "test", cfg, "", []byte("<head><body>__VERSION__</body>"), context.Background())
}

// --- Cache coherence ---

func TestCacheCoherence_RawUpdateInvalidatesParsed(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	// Write initial data.json
	data1 := map[string]any{"version": "v1", "totalCostToday": 1.0}
	writeJSON(t, filepath.Join(dir, "data.json"), data1)

	// Prime parsed cache via getDataCached
	parsed, err := srv.getDataCached()
	if err != nil {
		t.Fatal(err)
	}
	if parsed["version"] != "v1" {
		t.Fatalf("expected v1, got %v", parsed["version"])
	}

	// Advance mtime — write new data
	time.Sleep(50 * time.Millisecond)
	data2 := map[string]any{"version": "v2", "totalCostToday": 2.0}
	writeJSON(t, filepath.Join(dir, "data.json"), data2)

	// Simulate /api/refresh reading raw cache (updates cachedDataRaw + mtime)
	raw, err := srv.getDataRawCached()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"v2"`) {
		t.Fatalf("raw cache should contain v2, got: %s", raw[:80])
	}

	// Now getDataCached MUST return v2, not stale v1
	parsed2, err := srv.getDataCached()
	if err != nil {
		t.Fatal(err)
	}
	if parsed2["version"] != "v2" {
		t.Fatalf("cache coherence bug: expected v2, got %v (stale parsed cache)", parsed2["version"])
	}
}

// --- HEAD request handling ---

func TestHandleIndex_HEAD_NoBody(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	req := httptest.NewRequest(http.MethodHead, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD / should have empty body, got %d bytes", w.Body.Len())
	}
	if cl := w.Header().Get("Content-Length"); cl == "" {
		t.Fatal("HEAD / missing Content-Length header")
	}
}

func TestHandleRefresh_HEAD_NoBody(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	// Create data.json so the handler has something to serve
	writeJSON(t, filepath.Join(dir, "data.json"), map[string]any{"ok": true})

	req := httptest.NewRequest(http.MethodHead, "/api/refresh", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// HEAD responses MUST NOT contain a body
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD /api/refresh should have empty body, got %d bytes", w.Body.Len())
	}
	if cl := w.Header().Get("Content-Length"); cl == "" {
		t.Fatal("HEAD /api/refresh missing Content-Length header")
	}
}

func TestHandleRefresh_GET_HasBody(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	writeJSON(t, filepath.Join(dir, "data.json"), map[string]any{"ok": true})

	req := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Fatal("GET /api/refresh should have body")
	}
}

// --- Static file allowlist ---

func TestStaticFile_AllowedFile(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	os.WriteFile(filepath.Join(dir, "themes.json"), []byte(`{"dark":true}`), 0644)

	req := httptest.NewRequest(http.MethodGet, "/themes.json", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for themes.json, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}
}

func TestStaticFile_DisallowedFile(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"secret":true}`), 0644)

	req := httptest.NewRequest(http.MethodGet, "/config.json", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-allowlisted file, got %d", w.Code)
	}
}

func TestStaticFile_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	// This should not reach handleStaticFile (not in allowlist)
	// but test the traversal guard anyway via direct call
	req := httptest.NewRequest(http.MethodGet, "/../etc/passwd", nil)
	w := httptest.NewRecorder()
	srv.handleStaticFile(w, req, "/../etc/passwd", "text/plain")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for path traversal, got %d", w.Code)
	}
}

func TestStaticFile_HEAD_NoBody(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	os.WriteFile(filepath.Join(dir, "themes.json"), []byte(`{"dark":true}`), 0644)

	req := httptest.NewRequest(http.MethodHead, "/themes.json", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD should have empty body, got %d bytes", w.Body.Len())
	}
}

// --- Method not allowed ---

func TestMethodNotAllowed(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// --- Chat disabled ---

func TestChat_DisabledReturns503(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir) // AI disabled by default in test helper

	body := `{"question":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

// --- Chat input validation ---

func TestChat_EmptyQuestion(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.AI.Enabled = true
	srv := NewServer(dir, "test", cfg, "tok", []byte("<head></head>"), context.Background())

	body := `{"question":"   "}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestChat_QuestionTooLong(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.AI.Enabled = true
	srv := NewServer(dir, "test", cfg, "tok", []byte("<head></head>"), context.Background())

	q := strings.Repeat("a", maxQuestionLen+1)
	body := `{"question":"` + q + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestChat_BodyTooLarge(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.AI.Enabled = true
	srv := NewServer(dir, "test", cfg, "tok", []byte("<head></head>"), context.Background())

	body := strings.Repeat("x", maxBodyBytes+100)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", w.Code)
	}
}

func TestChat_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.AI.Enabled = true
	srv := NewServer(dir, "test", cfg, "tok", []byte("<head></head>"), context.Background())

	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestChat_MissingDataJSON_Returns503(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.AI.Enabled = true
	srv := NewServer(dir, "test", cfg, "tok", []byte("<head></head>"), context.Background())

	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"question":"hello"}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when data.json missing, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestChat_InvalidDataJSON_Returns500(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.AI.Enabled = true
	srv := NewServer(dir, "test", cfg, "tok", []byte("<head></head>"), context.Background())

	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"question":"hello"}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for invalid data.json, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestChat_NullDataJSON_Returns500(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.AI.Enabled = true
	srv := NewServer(dir, "test", cfg, "tok", []byte("<head></head>"), context.Background())

	nullData, err := os.ReadFile(filepath.Join("testdata", "dashboard", "data-null.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.json"), nullData, 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"question":"hello"}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for null data.json, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetDataCached_NullDataJSON_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	nullData, err := os.ReadFile(filepath.Join("testdata", "dashboard", "data-null.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.json"), nullData, 0o644); err != nil {
		t.Fatal(err)
	}

	parsed, err := srv.getDataCached()
	if err == nil {
		t.Fatal("expected null data.json to be rejected")
	}
	if parsed != nil {
		t.Fatalf("expected parsed data to be nil, got %#v", parsed)
	}
}

// --- Index rendering ---

func TestIndex_VersionInjected(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(dir, "1.2.3", defaultConfig(), "", []byte("<head><body>__VERSION__</body>"), context.Background())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), "1.2.3") {
		t.Fatal("version not injected into index.html")
	}
}

func TestIndex_RuntimeInjected(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(dir, "1.0", defaultConfig(), "", []byte("<head><body>__RUNTIME__ · v__VERSION__</body>"), context.Background())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "Go") {
		t.Fatal("runtime badge 'Go' not injected into index.html")
	}
	if strings.Contains(body, "__RUNTIME__") {
		t.Fatal("__RUNTIME__ placeholder not replaced")
	}
}

func TestIndex_ThemeMetaInjected(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.Theme.Preset = "solar"
	srv := NewServer(dir, "1.0", cfg, "", []byte("<head><body></body>"), context.Background())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), `content="solar"`) {
		t.Fatal("theme preset not injected into index.html")
	}
}

// --- CORS ---

func TestCORS_LocalhostOriginReflected(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	writeJSON(t, filepath.Join(dir, "data.json"), map[string]any{"ok": true})

	req := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if acao := w.Header().Get("Access-Control-Allow-Origin"); acao != "http://localhost:3000" {
		t.Fatalf("expected origin reflected, got %q", acao)
	}
}

func TestCORS_ExternalOriginDefaulted(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)

	writeJSON(t, filepath.Join(dir, "data.json"), map[string]any{"ok": true})

	req := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
	req.Header.Set("Origin", "http://evil.com")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	acao := w.Header().Get("Access-Control-Allow-Origin")
	if acao == "http://evil.com" {
		t.Fatal("external origin should NOT be reflected")
	}
}

// --- Data missing ---

func TestRefresh_DataMissing_Returns503(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)
	t.Setenv("OPENCLAW_HOME", t.TempDir())

	prev := refreshCollectorFunc
	defer func() { refreshCollectorFunc = prev }()
	refreshCollectorFunc = func(dashboardDir, openclawPath string, cfgOpt ...Config) error {
		return os.ErrNotExist
	}

	req := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when data.json missing, got %d", w.Code)
	}
}

func TestRefresh_DataMissing_WaitsForRefreshAndReturnsFreshData(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)
	openclawHome := t.TempDir()
	t.Setenv("OPENCLAW_HOME", openclawHome)

	prev := refreshCollectorFunc
	defer func() { refreshCollectorFunc = prev }()
	refreshCollectorFunc = func(dashboardDir, openclawPath string, cfgOpt ...Config) error {
		if dashboardDir != dir {
			t.Fatalf("unexpected dashboard dir: %s", dashboardDir)
		}
		if openclawPath != openclawHome {
			t.Fatalf("unexpected openclaw path: %s", openclawPath)
		}
		time.Sleep(20 * time.Millisecond)
		return os.WriteFile(filepath.Join(dashboardDir, "data.json"), []byte(`{"ok":true}`), 0o644)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after waiting for initial refresh, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("expected fresh data.json body, got %s", w.Body.String())
	}
}

// --- Rate limiting ---

func TestChat_RateLimitExceeded(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.AI.Enabled = true
	srv := NewServer(dir, "test", cfg, "tok", []byte("<head></head>"), context.Background())

	// Send chatRateLimit requests — all should be accepted (400 because no gateway, but not 429)
	for i := 0; i < chatRateLimit; i++ {
		body := `{"question":"hello"}`
		req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d should not be rate limited", i+1)
		}
	}

	// Next request should be rate limited
	body := `{"question":"one more"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after %d requests, got %d", chatRateLimit, w.Code)
	}
	if ra := w.Header().Get("Retry-After"); ra != "60" {
		t.Errorf("expected Retry-After: 60, got %q", ra)
	}
}

func TestChat_RateLimitPerIP(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.AI.Enabled = true
	srv := NewServer(dir, "test", cfg, "tok", []byte("<head></head>"), context.Background())

	// Exhaust rate limit for IP A
	for i := 0; i < chatRateLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"question":"hi"}`))
		req.RemoteAddr = "10.0.0.1:1111"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
	}

	// IP B should still be allowed
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"question":"hi"}`))
	req.RemoteAddr = "10.0.0.2:2222"
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code == http.StatusTooManyRequests {
		t.Fatal("different IP should not be rate limited")
	}
}

// --- Helpers ---

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
