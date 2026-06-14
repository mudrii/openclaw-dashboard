package appserver

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

func chatTestServer(t *testing.T, dir string, gatewayPort int) *Server {
	t.Helper()
	cfg := appconfig.Default()
	cfg.System.Enabled = false
	cfg.AI.Enabled = true
	cfg.AI.GatewayPort = gatewayPort
	cfg.AI.Model = "test-model"
	cfg.AI.MaxHistory = 3
	indexHTML := []byte("<html><head></head><body></body></html>")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	refreshFn := func(ctx context.Context, d, o string, cfg appconfig.Config) error { return nil }
	return NewServer(dir, "1.0.0-test", cfg, "tok", indexHTML, ctx, refreshFn)
}

func writeMinimalDataJSON(t *testing.T, dir string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"lastRefresh": "2026-05-13", "gateway": map[string]any{"status": "online"}})
	if err := os.WriteFile(filepath.Join(dir, "data.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustPostChat(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func TestHandleChat_Disabled(t *testing.T) {
	dir := t.TempDir()
	cfg := appconfig.Default()
	cfg.AI.Enabled = false
	cfg.System.Enabled = false
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := NewServer(dir, "v", cfg, "", []byte("<html></html>"), ctx,
		func(ctx context.Context, d, o string, cfg appconfig.Config) error { return nil })

	w := mustPostChat(t, s, `{"question":"hi"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleChat_BodyTooLarge(t *testing.T) {
	dir := t.TempDir()
	writeMinimalDataJSON(t, dir)
	s := chatTestServer(t, dir, 1)

	huge := strings.Repeat("a", maxBodyBytes+10)
	w := mustPostChat(t, s, huge)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", w.Code)
	}
}

func TestHandleChat_BadJSON(t *testing.T) {
	dir := t.TempDir()
	writeMinimalDataJSON(t, dir)
	s := chatTestServer(t, dir, 1)

	w := mustPostChat(t, s, `not json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestHandleChat_EmptyQuestion(t *testing.T) {
	dir := t.TempDir()
	writeMinimalDataJSON(t, dir)
	s := chatTestServer(t, dir, 1)

	w := mustPostChat(t, s, `{"question":"   "}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestHandleChat_QuestionTooLong(t *testing.T) {
	dir := t.TempDir()
	writeMinimalDataJSON(t, dir)
	s := chatTestServer(t, dir, 1)

	q := strings.Repeat("é", maxQuestionLen+5)
	body, _ := json.Marshal(map[string]any{"question": q})
	w := mustPostChat(t, s, string(body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleChat_RateLimited(t *testing.T) {
	dir := t.TempDir()
	writeMinimalDataJSON(t, dir)
	s := chatTestServer(t, dir, 1)

	// Burn the bucket
	for i := 0; i < chatRateLimit; i++ {
		s.chatLimiter.allow("10.0.0.5")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"question":"hi"}`))
	req.RemoteAddr = "10.0.0.5:1111"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") != "60" {
		t.Errorf("Retry-After header missing")
	}
}

func TestHandleChat_MissingDataJSON(t *testing.T) {
	dir := t.TempDir()
	// no data.json
	s := chatTestServer(t, dir, 1)
	w := mustPostChat(t, s, `{"question":"hi"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
}

func TestHandleChat_SuccessHitsGateway(t *testing.T) {
	dir := t.TempDir()
	writeMinimalDataJSON(t, dir)

	// Fake gateway via httptest (ready listener — no busy-wait needed). The
	// handler builds http://localhost:<port>/v1/chat/completions, and
	// httptest.Server binds 127.0.0.1:<port> which localhost resolves to.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		// Assert the server forwards the gateway token and configured model so
		// dropping either would fail this test.
		if auth := r.Header.Get("Authorization"); auth != "Bearer tok" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer tok")
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode gateway request body: %v", err)
		}
		if body.Model != "test-model" {
			t.Errorf("request model = %q, want %q", body.Model, "test-model")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello back"}}]}`))
	})
	gw := httptest.NewServer(mux)
	t.Cleanup(gw.Close)
	port := gatewayPort(t, gw)

	s := chatTestServer(t, dir, port)
	w := mustPostChat(t, s, `{"question":"ping"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response body %q: %v", w.Body.String(), err)
	}
	if out["answer"] != "hello back" {
		t.Errorf("want answer 'hello back', got %q", out["answer"])
	}
}

func TestHandleChat_GatewayError502(t *testing.T) {
	dir := t.TempDir()
	writeMinimalDataJSON(t, dir)
	// freePort returns a port we opened then immediately closed — nothing is
	// listening, so the connection is refused fast and deterministically. A
	// refused dial (not a timeout) surfaces as 502 Bad Gateway.
	s := chatTestServer(t, dir, freePort(t))

	w := mustPostChat(t, s, `{"question":"ping"}`)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestChatRateLimiter_PerIPIsolation verifies that exhausting one IP's bucket
// does not affect requests from a different IP address.
func TestChatRateLimiter_PerIPIsolation(t *testing.T) {
	var rl chatRateLimiter
	// Exhaust IP A completely (consume all tokens + one denied).
	for range chatRateLimit {
		rl.allow("192.168.1.1")
	}
	if rl.allow("192.168.1.1") {
		t.Fatal("IP A should be rate-limited after exhausting bucket")
	}
	// IP B must be unaffected.
	if !rl.allow("192.168.1.2") {
		t.Error("IP B should not be affected by IP A's rate limit")
	}
}

// TestChatRateLimiter_WindowReset verifies that a bucket refills after the
// rate window elapses. It back-dates lastReset directly to avoid real sleeps.
func TestChatRateLimiter_WindowReset(t *testing.T) {
	var rl chatRateLimiter
	ip := "10.0.0.1"

	// Exhaust the bucket.
	for range chatRateLimit {
		rl.allow(ip)
	}
	if rl.allow(ip) {
		t.Fatal("bucket should be exhausted")
	}

	// Back-date the bucket's lastReset to simulate window expiry.
	v, ok := rl.entries.Load(ip)
	if !ok {
		t.Fatal("bucket not found in entries map")
	}
	bucket := v.(*rateBucket)
	bucket.mu.Lock()
	bucket.lastReset = time.Now().Add(-chatRateWindow - time.Second)
	bucket.mu.Unlock()

	// First request after window expiry must be allowed.
	if !rl.allow(ip) {
		t.Error("request should be allowed after window reset")
	}
}

// helpers --------------------------------------------------------------

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// gatewayPort extracts the integer port a running httptest.Server is bound to.
func gatewayPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("parse httptest URL %q: %v", srv.URL, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return port
}
