package appserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appconfig"
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func TestRuntimeDetailRoutesAreBoundedReads(t *testing.T) {
	for _, tc := range []struct {
		url  string
		code int
	}{
		{"/api/automation/runs?id=job&offset=50", 200},
		{"/api/automation/runs", 400},
		{"/api/automation/runs?id=job&offset=-1", 400},
		{"/api/session/context?key=session", 200},
		{"/api/session/context?key=%00", 400},
	} {
		t.Run(tc.url, func(t *testing.T) {
			s := NewServer(t.TempDir(), "test", appconfig.Default(), "", nil, t.Context(), nil)
			s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				if tc.code != 200 {
					t.Error("invalid request executed CLI")
				}
				body := `{"entries":[],"hasMore":false}`
				if args[2] == "progressCard.get" {
					body = `{"card":null}`
				}
				if args[2] == "board.get" {
					body = `{"widgets":[]}`
				}
				return exec.CommandContext(ctx, "printf", "%s", body)
			}}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if w.Code != tc.code {
				t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatal("unexpected secret")
			}
		})
	}
}
