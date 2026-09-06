package appserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type chatOriginTransport struct{ calls int }

func (transport *chatOriginTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls++
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"fixture answer"}}]}`)), Header: make(http.Header)}, nil
}

func TestChatRejectsUntrustedOriginsBeforeGatewayWork(t *testing.T) {
	for _, tc := range []struct {
		origin  string
		allowed bool
	}{
		{"https://attacker.example", false},
		{"null", false},
		{"http://localhost:8080@attacker.example", false},
		{"http://localhost:8080/path", false},
		{"http://localhost:8080?query", false},
		{"http://localhost:8080#", false},
		{"https://dashboard.example:444", false},
		{"", true},
		{"http://localhost:5173", true},
		{"http://127.0.0.1:5173", true},
		{"http://[::1]:5173", true},
		{"https://dashboard.example", true},
	} {
		t.Run(tc.origin, func(t *testing.T) {
			dir := t.TempDir()
			writeMinimalDataJSON(t, dir)
			s := chatTestServer(t, dir, 1)
			transport := &chatOriginTransport{}
			s.httpClient = &http.Client{Transport: transport}
			req := httptest.NewRequest(http.MethodPost, "https://dashboard.example/api/chat", strings.NewReader(`{"question":"hello"}`))
			req.TLS = nil // TLS-terminating proxy preserves the public Host.
			req.Header.Set("Origin", tc.origin)
			// A simple cross-origin POST does not require a CORS preflight.
			req.Header.Set("Content-Type", "text/plain")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			want := http.StatusForbidden
			if tc.allowed {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Fatalf("status=%d body=%s, want %d", w.Code, w.Body.String(), want)
			}
			if (transport.calls == 1) != tc.allowed {
				t.Fatalf("gateway calls=%d, allowed=%v", transport.calls, tc.allowed)
			}
		})
	}
}
