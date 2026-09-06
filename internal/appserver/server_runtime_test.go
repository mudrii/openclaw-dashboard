package appserver

import (
	"context"
	"encoding/json"
	"errors"
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

// TestRuntimeReadErrorMapsCodesToStatuses pins the shared translation from an
// OpenClaw read failure to an HTTP status plus a machine-readable errorCode.
func TestRuntimeReadErrorMapsCodesToStatuses(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		err        error
		status     int
	}{
		{"permission denied", "permission_denied", appopenclaw.CommandError([]byte("missing scope"), errors.New("exit 1")), http.StatusForbidden},
		{"unsupported", "unsupported", appopenclaw.CommandError([]byte("unknown method"), errors.New("exit 1")), http.StatusNotImplemented},
		{"timeout", "timeout", context.DeadlineExceeded, http.StatusBadGateway},
		{"storage busy", "storage_busy", appopenclaw.CommandError([]byte("database is locked"), errors.New("exit 1")), http.StatusBadGateway},
		{"unclassified", "unavailable", errors.New("broken pipe"), http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer(t.TempDir(), "test", appconfig.Default(), "", nil, t.Context(), nil)
			w := httptest.NewRecorder()
			s.runtimeReadError(w, httptest.NewRequest(http.MethodGet, "/api/workboard", nil), tc.err)
			if w.Code != tc.status {
				t.Fatalf("status=%d want %d", w.Code, tc.status)
			}
			var payload struct{ State, ErrorCode string }
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.State != "unavailable" || payload.ErrorCode != tc.code {
				t.Fatalf("payload=%+v want unavailable/%s", payload, tc.code)
			}
			if strings.Contains(w.Body.String(), "exit 1") || strings.Contains(w.Body.String(), "broken pipe") {
				t.Fatalf("leaked runtime diagnostics: %s", w.Body.String())
			}
		})
	}
}

// TestWorkboardSummaryReportsReadyAndFailure covers both /api/workboard exits:
// a loaded plugin returns its boards, a refused read returns the runtime code.
func TestWorkboardSummaryReportsReadyAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		fail       bool
		status     int
	}{
		{name: "ready", want: `"state":"ready"`, status: http.StatusOK},
		{name: "refused", want: `"errorCode":"permission_denied"`, fail: true, status: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := appconfig.Default()
			cfg.Openclaw = appopenclaw.Target{Mode: "container", Container: "test"}
			s := NewServer(t.TempDir(), "test", cfg, "", nil, t.Context(), nil)
			s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				if tc.fail {
					return exec.CommandContext(ctx, "sh", "-c", "printf '%s' 'missing scope' >&2; exit 1")
				}
				body := `{"plugins":[{"id":"workboard","status":"loaded"}]}`
				if args[0] == "gateway" {
					body = `{"boards":[{"id":"b1","name":"Delivery","total":3,"secret":"nope"}]}`
				}
				return exec.CommandContext(ctx, "printf", "%s", body)
			}}

			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/workboard", nil))

			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "nope") {
				t.Fatalf("unprojected board field leaked: %s", w.Body.String())
			}
		})
	}
}
