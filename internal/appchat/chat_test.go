package appchat

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildSystemPrompt_WithData(t *testing.T) {
	data := map[string]any{
		"lastRefresh":    "2025-01-01T00:00:00Z",
		"totalCostToday": 1.23,
		"gateway": map[string]any{
			"status": "online",
			"pid":    float64(1234),
			"uptime": "2d 3h",
			"memory": "128MB",
		},
		"sessions": []any{
			map[string]any{
				"name":       "test-session",
				"model":      "opus",
				"type":       "chat",
				"contextPct": 45.5,
			},
		},
		"crons": []any{
			map[string]any{
				"name":       "daily-backup",
				"schedule":   "0 0 * * *",
				"lastStatus": "ok",
			},
		},
		"agentConfig": map[string]any{
			"primaryModel": "opus-4",
		},
	}

	prompt := BuildSystemPrompt(data)
	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
	for _, want := range []string{
		"OpenClaw Dashboard",
		"2025-01-01T00:00:00Z",
		"=== GATEWAY ===",
		"online",
		"=== COSTS ===",
		"=== SESSIONS",
		"test-session",
		"=== CRON JOBS",
		"daily-backup",
		"=== ALERTS ===",
		"None",
		"=== CONFIGURATION ===",
		"opus-4",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildSystemPrompt_EmptyData(t *testing.T) {
	// Should not panic with nil or empty map
	prompt := BuildSystemPrompt(nil)
	if prompt == "" {
		t.Error("prompt should not be empty even with nil data")
	}

	prompt = BuildSystemPrompt(map[string]any{})
	if prompt == "" {
		t.Error("prompt should not be empty with empty map")
	}
}

func TestCallGateway_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "Hello from gateway"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Extract port from test server URL
	port := getPort(t, srv.URL)
	client := srv.Client()

	answer, err := CallGateway(context.Background(), "system prompt", nil, "hello", port, "token", "test-model", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "Hello from gateway" {
		t.Errorf("expected 'Hello from gateway', got %q", answer)
	}
}

func TestCallGateway_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	port := getPort(t, srv.URL)
	client := srv.Client()

	_, err := CallGateway(context.Background(), "sys", nil, "hello", port, "tok", "model", client)
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
	var ge *GatewayError
	if !errors.As(err, &ge) {
		t.Fatalf("expected GatewayError, got %T", err)
	}
	if ge.Status != http.StatusBadGateway {
		t.Errorf("expected 502 status, got %d", ge.Status)
	}
}

func TestCallGateway_Timeout(t *testing.T) {
	// Block the handler on a channel instead of sleeping a fixed duration; the
	// client-side ctx timeout (below) is what we exercise. The channel is
	// closed before srv.Close() so the in-flight handler unblocks and Close
	// does not deadlock waiting on the active connection.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(release)

	port := getPort(t, srv.URL)
	client := srv.Client()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := CallGateway(ctx, "sys", nil, "hello", port, "tok", "model", client)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	var ge *GatewayError
	if !errors.As(err, &ge) {
		t.Fatalf("expected GatewayError, got %T", err)
	}
	if ge.Status != http.StatusGatewayTimeout {
		t.Errorf("expected 504 status, got %d", ge.Status)
	}
}

func TestCallGateway_EmptyModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload CompletionPayload
		json.NewDecoder(r.Body).Decode(&payload)
		if payload.Model != "" {
			t.Errorf("expected empty model, got %q", payload.Model)
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "ok"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	port := getPort(t, srv.URL)
	client := srv.Client()

	answer, err := CallGateway(context.Background(), "sys", nil, "hi", port, "tok", "", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "ok" {
		t.Errorf("expected 'ok', got %q", answer)
	}
}

func TestGatewayError_Error(t *testing.T) {
	ge := &GatewayError{Status: 502, Msg: "gateway down"}
	if ge.Error() != "gateway down" {
		t.Errorf("expected 'gateway down', got %q", ge.Error())
	}
}

func getPort(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url %q: %v", rawURL, err)
	}
	_, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host:port from %q: %v", u.Host, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("atoi port %q: %v", portStr, err)
	}
	return port
}
