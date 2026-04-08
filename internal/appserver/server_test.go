package appserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := appconfig.Default()
	cfg.AI.Enabled = false
	cfg.System.Enabled = false
	indexHTML := []byte("<html><head></head><body>__VERSION__ __RUNTIME__</body></html>")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	refreshFn := func(dir, openclawPath string, cfgs ...appconfig.Config) error { return nil }
	return NewServer(dir, "1.0.0-test", cfg, "test-token", indexHTML, ctx, refreshFn)
}

func TestHandleStaticFile_Allowlisted(t *testing.T) {
	dir := t.TempDir()
	cfg := appconfig.Default()
	cfg.System.Enabled = false
	indexHTML := []byte("<html><head></head><body></body></html>")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	refreshFn := func(d, o string, c ...appconfig.Config) error { return nil }
	s := NewServer(dir, "1.0.0", cfg, "", indexHTML, ctx, refreshFn)

	// Create themes.json in dir
	if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/themes.json", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
}

func TestHandleStaticFile_NotAllowlisted(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/secret.txt", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleStaticFile_PathTraversal(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/../../../etc/passwd", nil)
	w := httptest.NewRecorder()
	s.HandleStaticFile(w, req, "/../../../etc/passwd", "text/plain")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for path traversal, got %d", w.Code)
	}
}

func TestServeHTTP_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestServeHTTP_CORS(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodOptions, "/api/chat", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
	acao := w.Header().Get("Access-Control-Allow-Origin")
	if acao != "http://localhost:3000" {
		t.Errorf("expected CORS origin http://localhost:3000, got %q", acao)
	}
}

func TestServeHTTP_UnknownRoute(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestSendJSON_ValidData(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.sendJSON(w, req, http.StatusOK, map[string]string{"hello": "world"})

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if result["hello"] != "world" {
		t.Errorf("expected hello=world, got %v", result)
	}
}

func TestSendJSON_MarshalError(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	// Channels cannot be marshalled to JSON
	s.sendJSON(w, req, http.StatusOK, make(chan int))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on marshal error, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "internal server error") {
		t.Errorf("expected internal server error message, got %q", w.Body.String())
	}
}

func TestChatRateLimiter_Allow(t *testing.T) {
	rl := &chatRateLimiter{}
	for i := range chatRateLimit {
		if !rl.allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
}

func TestChatRateLimiter_Block(t *testing.T) {
	rl := &chatRateLimiter{}
	for range chatRateLimit {
		rl.allow("1.2.3.4")
	}
	if rl.allow("1.2.3.4") {
		t.Error("request after limit should be blocked")
	}
	// Different IP should still be allowed
	if !rl.allow("5.6.7.8") {
		t.Error("different IP should be allowed")
	}
}
