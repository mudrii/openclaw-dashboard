package appserver

import (
	"context"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appconfig"
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func TestSelectedRuntimeChatRejectsDisabledEndpoint(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "openclaw.json"), []byte(`{"gateway":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data.json"), []byte(`{"lastRefresh":"2026-09-06"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := appconfig.Default()
	cfg.AI.Enabled = true
	cfg.Openclaw = appopenclaw.Target{Mode: "container", Container: "test"}
	s := NewServer(root, "test", cfg, "private-token", nil, t.Context(), nil)
	s.openclawPath = root
	s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		if args[2] != "config.get" || appopenclaw.TargetFromContext(ctx).Container != "test" {
			t.Fatalf("wrong runtime request %v", args)
		}
		return exec.CommandContext(ctx, "printf", "%s", `{"valid":true,"exists":true,"config":{"gateway":{}}}`)
	}}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/api/chat", strings.NewReader(`{"question":"hello"}`)))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "endpoint_disabled") || strings.Contains(w.Body.String(), "private-token") {
		t.Fatalf("response=%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/chat/status", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatalf("status=%d %s", w.Code, w.Body.String())
	}
}

func TestContainerChatCapabilityUsesGatewayConfig(t *testing.T) {
	for _, tc := range []struct{ name, body, state string }{
		{"enabled", `{"valid":true,"exists":true,"config":{"gateway":{"http":{"endpoints":{"chatCompletions":{"enabled":true}}}}}}`, "configured"},
		{"unavailable", `{}`, "configuration_unavailable"},
		{"null config", `{"valid":true,"exists":true,"config":null}`, "configuration_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := appconfig.Default()
			cfg.Openclaw = appopenclaw.Target{Mode: "container", Container: "podman-target"}
			s := NewServer(t.TempDir(), "test", cfg, "private-token", nil, t.Context(), nil)
			s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				if args[2] != "config.get" {
					t.Fatalf("unexpected action %v", args)
				}
				return exec.CommandContext(ctx, "printf", "%s", tc.body)
			}}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest("GET", "/api/chat/status", nil))
			if !strings.Contains(w.Body.String(), `"state":"`+tc.state+`"`) || strings.Contains(w.Body.String(), "private-token") {
				t.Fatal(w.Body.String())
			}
		})
	}
}

func TestInheritedContainerChatChecksEndpointBeforeTransport(t *testing.T) {
	t.Setenv("OPENCLAW_CONTAINER", "fixture")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.json"), []byte(`{"lastRefresh":"2026-09-06"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := appconfig.Default()
	cfg.AI.Enabled = true
	s := NewServer(root, "test", cfg, "private-token", nil, t.Context(), nil)
	reads := 0
	s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		reads++
		if args[2] != "config.get" || !appopenclaw.TargetFromContext(ctx).IsContainer() {
			t.Fatalf("unexpected request %v", args)
		}
		return exec.CommandContext(ctx, "printf", "%s", `{"valid":true,"exists":true,"config":{"gateway":{}}}`)
	}}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/api/chat", strings.NewReader(`{"question":"fixture"}`)))
	if reads != 1 || w.Code != 503 || !strings.Contains(w.Body.String(), "endpoint_disabled") {
		t.Fatalf("config reads=%d response=%d %s", reads, w.Code, w.Body)
	}
}

// TestNativeChatGateRefusesUnusableCredentials pins the native (non-container)
// install: the capability gate must run there too, so a missing gateway token
// is reported as credentials_missing instead of being discovered by a failed
// dial to the gateway.
func TestNativeChatGateRefusesUnusableCredentials(t *testing.T) {
	for _, tc := range []struct{ name, runtimeConfig, token, state string }{
		{"missing credentials", `{"gateway":{"http":{"endpoints":{"chatCompletions":{"enabled":true}}}}}`, "", "credentials_missing"},
		{"endpoint disabled", `{"gateway":{}}`, "private-token", "endpoint_disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENCLAW_CONTAINER", "")
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "openclaw.json"), []byte(tc.runtimeConfig), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "data.json"), []byte(`{"lastRefresh":"2026-09-06"}`), 0600); err != nil {
				t.Fatal(err)
			}
			port, dialed := refusingGateway(t)
			cfg := appconfig.Default()
			cfg.AI.Enabled = true
			cfg.System.Enabled = false
			cfg.AI.GatewayPort = port
			s := NewServer(root, "test", cfg, tc.token, nil, t.Context(), nil)
			s.openclawPath = root
			s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				t.Errorf("native install used the runtime CLI: %v", args)
				return exec.CommandContext(ctx, "printf", "%s", "{}")
			}}

			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest("POST", "/api/chat", strings.NewReader(`{"question":"hello"}`)))

			if w.Code != 503 || !strings.Contains(w.Body.String(), `"errorCode":"`+tc.state+`"`) {
				t.Fatalf("response=%d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private-token") {
				t.Fatalf("leaked token: %s", w.Body.String())
			}
			select {
			case <-dialed:
				t.Fatal("refused chat still dialed the gateway")
			default:
			}
		})
	}
}

// refusingGateway starts a loopback listener that records connections instead
// of speaking HTTP, so a test can prove no gateway dial happened.
func refusingGateway(t *testing.T) (int, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	dialed := make(chan struct{}, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
			select {
			case dialed <- struct{}{}:
			default:
			}
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, dialed
}
