package appserver

import (
	"context"
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
	cfg := appconfig.Default()
	cfg.AI.Enabled = true
	s := NewServer(t.TempDir(), "test", cfg, "private-token", nil, t.Context(), nil)
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
