package appserver

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	refreshFn := func(ctx context.Context, d, o string, c ...appconfig.Config) error { return nil }
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
		func(ctx context.Context, d, o string, c ...appconfig.Config) error { return nil })

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

	// Spin a fake gateway on a free localhost port matching the handler's URL shape.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"hello back"}}]}`))
	})
	port := freePort(t)
	srv := &http.Server{Addr: "127.0.0.1:" + itoa(port), Handler: mux}
	go func() { _ = srv.ListenAndServe() }()
	t.Cleanup(func() { _ = srv.Close() })
	waitListening(t, port)

	s := chatTestServer(t, dir, port)
	w := mustPostChat(t, s, `{"question":"ping"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["answer"] != "hello back" {
		t.Errorf("want answer 'hello back', got %q", out["answer"])
	}
}

func TestHandleChat_GatewayError504(t *testing.T) {
	dir := t.TempDir()
	writeMinimalDataJSON(t, dir)
	// Point at a port that nothing listens on → connection refused, fast.
	s := chatTestServer(t, dir, 1) // port 1 is privileged, refused for unprivileged user

	w := mustPostChat(t, s, `{"question":"ping"}`)
	if w.Code != http.StatusBadGateway && w.Code != http.StatusGatewayTimeout {
		t.Fatalf("want 502/504, got %d body=%s", w.Code, w.Body.String())
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

func itoa(n int) string {
	return strings.TrimSpace(formatInt(n))
}

func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func waitListening(t *testing.T, port int) {
	t.Helper()
	for i := 0; i < 50; i++ {
		c, err := net.Dial("tcp", "127.0.0.1:"+itoa(port))
		if err == nil {
			_ = c.Close()
			return
		}
	}
	t.Fatalf("gateway did not start listening on port %d", port)
}
