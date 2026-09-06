package appserver

import (
	"context"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appconfig"
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func TestRuntimeLogsUseGatewayAndCache(t *testing.T) {
	cfg := appconfig.Default()
	cfg.Openclaw = appopenclaw.Target{Mode: "container", Container: "test"}
	cfg.Logs.Enabled = true
	s := NewServer(t.TempDir(), "test", cfg, "", nil, t.Context(), nil)
	calls := 0
	s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		calls++
		if appopenclaw.TargetFromContext(ctx).Container != "test" || args[2] != "logs.tail" {
			t.Fatal("wrong runtime log source")
		}
		return exec.CommandContext(ctx, "printf", "%s", `{"lines":["2026-09-05T00:00:00Z error token=secret-value"],"truncated":true,"cursor":12}`)
	}}
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "/api/logs?source=gateway&limit=10", nil))
		if w.Code != 200 || strings.Contains(w.Body.String(), "secret-value") || !strings.Contains(w.Body.String(), `"truncated":true`) {
			t.Fatalf("response=%d %s", w.Code, w.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("calls=%d want 1 cached read", calls)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/logs?source=cron", nil))
	if w.Code != 400 {
		t.Fatalf("unsupported source accepted: %d", w.Code)
	}
}
