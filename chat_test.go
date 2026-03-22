package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCallGateway_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing or wrong auth header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing content-type")
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hello back"}}]}`)
	}))
	defer ts.Close()

	// Extract port from test server URL
	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")
	portInt := 0
	fmt.Sscanf(port, "%d", &portInt)

	client := &http.Client{}
	answer, err := callGateway(context.Background(), "system prompt", nil, "hi", portInt, "test-token", "test-model", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "hello back" {
		t.Fatalf("expected 'hello back', got %q", answer)
	}
}

func TestCallGateway_EmptyChoices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[]}`)
	}))
	defer ts.Close()

	port := extractPort(t, ts.URL)
	client := &http.Client{}
	answer, err := callGateway(context.Background(), "sys", nil, "hi", port, "tok", "model", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "(empty response)" {
		t.Fatalf("expected '(empty response)', got %q", answer)
	}
}

func TestCallGateway_EmptyContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":""}}]}`)
	}))
	defer ts.Close()

	port := extractPort(t, ts.URL)
	client := &http.Client{}
	answer, err := callGateway(context.Background(), "sys", nil, "hi", port, "tok", "model", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "(empty response)" {
		t.Fatalf("expected '(empty response)', got %q", answer)
	}
}

func TestCallGateway_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"boom"}`)
	}))
	defer ts.Close()

	port := extractPort(t, ts.URL)
	client := &http.Client{}
	_, err := callGateway(context.Background(), "sys", nil, "hi", port, "tok", "model", client)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should mention status code, got: %v", err)
	}
}

func TestCallGateway_ResponseTooLarge(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write 2MB of data — should be rejected by the 1MB limit
		w.Write([]byte(`{"choices":[{"message":{"content":"`))
		for i := 0; i < 2*1024*1024; i++ {
			w.Write([]byte("a"))
		}
		w.Write([]byte(`"}}]}`))
	}))
	defer ts.Close()

	port := extractPort(t, ts.URL)
	client := &http.Client{}
	_, err := callGateway(context.Background(), "sys", nil, "hi", port, "tok", "model", client)
	if err == nil {
		t.Fatal("expected error for oversized gateway response")
	}
}

func TestCallGateway_HistoryIncluded(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Just verify we got the request
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer ts.Close()

	port := extractPort(t, ts.URL)
	client := &http.Client{}
	history := []chatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "reply"},
	}
	answer, err := callGateway(context.Background(), "sys", history, "second", port, "tok", "model", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "ok" {
		t.Fatalf("expected 'ok', got %q", answer)
	}
}

func TestCallGateway_Timeout_Returns504(t *testing.T) {
	// Test server that sleeps longer than the client timeout
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"too late"}}]}`)
	}))
	defer ts.Close()

	port := extractPort(t, ts.URL)
	client := &http.Client{Timeout: 100 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := callGateway(ctx, "sys", nil, "hi", port, "tok", "model", client)
	if err == nil {
		t.Fatal("expected error on timeout")
	}

	ge, ok := err.(*gatewayError)
	if !ok {
		t.Fatalf("expected *gatewayError, got %T: %v", err, err)
	}
	if ge.Status != http.StatusGatewayTimeout {
		t.Fatalf("expected status 504, got %d", ge.Status)
	}
}

func TestCallGateway_HTTPError_Returns502(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"boom"}`)
	}))
	defer ts.Close()

	port := extractPort(t, ts.URL)
	client := &http.Client{}
	_, err := callGateway(context.Background(), "sys", nil, "hi", port, "tok", "model", client)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}

	ge, ok := err.(*gatewayError)
	if !ok {
		t.Fatalf("expected *gatewayError, got %T: %v", err, err)
	}
	if ge.Status != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", ge.Status)
	}
}

func TestCallGateway_Unreachable_Returns502(t *testing.T) {
	client := &http.Client{}
	_, err := callGateway(context.Background(), "sys", nil, "hi", 1, "tok", "model", client)
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}

	ge, ok := err.(*gatewayError)
	if !ok {
		t.Fatalf("expected *gatewayError, got %T: %v", err, err)
	}
	if ge.Status != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", ge.Status)
	}
}

func TestBuildSystemPrompt_EmptyData(t *testing.T) {
	prompt := buildSystemPrompt(map[string]any{})
	if !strings.Contains(prompt, "OpenClaw Dashboard") {
		t.Fatal("system prompt should mention OpenClaw Dashboard")
	}
	if !strings.Contains(prompt, "GATEWAY") {
		t.Fatal("system prompt should have GATEWAY section")
	}
}

func TestBuildSystemPrompt_WithData(t *testing.T) {
	data := map[string]any{
		"lastRefresh":    "2026-03-03 12:00:00 UTC",
		"totalCostToday": 5.1234,
		"gateway":        map[string]any{"status": "online", "pid": float64(1234)},
		"sessions":       []any{},
		"crons":          []any{},
		"alerts":         []any{},
		"agentConfig":    map[string]any{"primaryModel": "claude-opus"},
	}
	prompt := buildSystemPrompt(data)
	if !strings.Contains(prompt, "2026-03-03 12:00:00 UTC") {
		t.Fatal("prompt should include lastRefresh timestamp")
	}
	if !strings.Contains(prompt, "online") {
		t.Fatal("prompt should include gateway status")
	}
	if !strings.Contains(prompt, "claude-opus") {
		t.Fatal("prompt should include primary model")
	}
}

func extractPort(t *testing.T, url string) int {
	t.Helper()
	port := 0
	parts := strings.Split(url, ":")
	fmt.Sscanf(parts[len(parts)-1], "%d", &port)
	if port == 0 {
		t.Fatalf("could not extract port from %s", url)
	}
	return port
}
