package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// TestServeHTTP_RejectsNonLoopbackHost guards against DNS rebinding: a page on
// a hostile domain that resolves to 127.0.0.1 reaches the server with its own
// Host header and must not read the API or spend the gateway credential.
func TestServeHTTP_RejectsNonLoopbackHost(t *testing.T) {
	for _, tc := range []struct {
		name, host, allow, hosts string
		want                     bool
	}{
		{name: "foreign host", host: "evil.example:8080", want: false},
		{name: "foreign host without port", host: "evil.example", want: false},
		{name: "localhost lookalike", host: "localhost.evil.example:8080", want: false},
		{name: "empty host", host: "", want: false},
		{name: "localhost", host: "localhost:8080", want: true},
		{name: "ipv4 loopback", host: "127.0.0.1:8080", want: true},
		{name: "ipv6 loopback", host: "[::1]:8080", want: true},
		{name: "override allows any host", host: "evil.example:8080", allow: "1", want: true},
		{name: "override needs exact value", host: "evil.example:8080", allow: "true", want: false},
		{name: "localhost trailing dot", host: "localhost.:8080", want: true},
		{name: "allowed proxy host", host: "Dash.Example.com", hosts: "dash.example.com, other.example", want: true},
		{name: "allowed proxy host with port", host: "dash.example.com:443", hosts: "dash.example.com", want: true},
		{name: "allowed list does not match others", host: "evil.example:8080", hosts: "dash.example.com", want: false},
		{name: "allowed list ignores empty entries", host: "", hosts: " , ", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(allowNonLoopbackEnv, tc.allow)
			t.Setenv(allowedHostsEnv, tc.hosts)

			t.Run("refresh", func(t *testing.T) {
				dir := t.TempDir()
				writeMinimalData(t, dir)
				var calls atomic.Int64
				s := newTestServerForRefresh(t, dir, func(context.Context, string, string, appconfig.Config) error {
					calls.Add(1)
					return nil
				})
				req := httptest.NewRequest(http.MethodGet, "http://localhost/api/refresh", nil)
				req.Host = tc.host
				w := httptest.NewRecorder()
				s.ServeHTTP(w, req)
				if got := w.Code != http.StatusMisdirectedRequest; got != tc.want {
					t.Fatalf("Host %q: status=%d, want allowed=%v", tc.host, w.Code, tc.want)
				}
				waitRefreshIdle(t, s)
				if !tc.want && calls.Load() != 0 {
					t.Fatalf("rejected request ran refreshFn %d times", calls.Load())
				}
			})

			t.Run("chat", func(t *testing.T) {
				dir := t.TempDir()
				writeMinimalDataJSON(t, dir)
				s := chatTestServer(t, dir, 1)
				transport := &chatOriginTransport{}
				s.httpClient = &http.Client{Transport: transport}
				req := httptest.NewRequest(http.MethodPost, "http://localhost/api/chat", strings.NewReader(`{"question":"hello"}`))
				req.Host = tc.host
				w := httptest.NewRecorder()
				s.ServeHTTP(w, req)
				if got := w.Code != http.StatusMisdirectedRequest; got != tc.want {
					t.Fatalf("Host %q: status=%d, want allowed=%v", tc.host, w.Code, tc.want)
				}
				if !tc.want && transport.calls != 0 {
					t.Fatalf("rejected chat reached the gateway %d times", transport.calls)
				}
			})
		})
	}
}

// TestOperationsResponsesAreNoStore pins Cache-Control: no-store on every
// /api/operations answer; sendJSON must not downgrade it to no-cache.
func TestOperationsResponsesAreNoStore(t *testing.T) {
	const body = `{"operationId":"12345678-1234-1234-1234-123456789012","action":"automation.disable","id":"job","configRevision":"rev"}`
	for _, tc := range []struct {
		name    string
		enabled bool
		status  int
	}{
		{name: "disabled", enabled: false, status: http.StatusForbidden},
		{name: "applied", enabled: true, status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := operationsTestServer(t, t.TempDir())
			s.cfg.Operations.Enabled = tc.enabled
			s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				out := `{"ok":true}`
				if args[2] == "cron.get" {
					out = `{"id":"job","configRevision":"rev","enabled":true}`
				}
				return exec.CommandContext(ctx, "printf", "%s", out)
			}}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, operationRequestFor(t, s, body))
			if w.Code != tc.status {
				t.Fatalf("status=%d want %d body=%s", w.Code, tc.status, w.Body.String())
			}
			if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", cc)
			}
		})
	}
}

// waitRefreshIdle blocks until any in-flight refresh worker has finished.
func waitRefreshIdle(t *testing.T, s *Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := s.WaitRefresh(ctx); err != nil {
		t.Fatalf("refresh did not finish: %v", err)
	}
}

// TestHandleRefresh_FailedRefreshIsDebounced guards against a failing collector
// being re-run on every /api/refresh: the debounce applies to the last attempt,
// not the last success, whether or not data.json exists.
func TestHandleRefresh_FailedRefreshIsDebounced(t *testing.T) {
	for _, tc := range []struct {
		name     string
		haveData bool
		status   int
	}{
		{name: "stale data present", haveData: true, status: http.StatusOK},
		{name: "data missing", haveData: false, status: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.haveData {
				writeMinimalData(t, dir)
			}
			var calls atomic.Int64
			s := newTestServerForRefresh(t, dir, func(context.Context, string, string, appconfig.Config) error {
				calls.Add(1)
				return errors.New("collector failed")
			})
			s.cfg.Refresh.IntervalSeconds = 60

			for i := range 3 {
				w := httptest.NewRecorder()
				s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/api/refresh", nil))
				if w.Code != tc.status {
					t.Fatalf("request %d: status=%d want %d body=%s", i, w.Code, tc.status, w.Body.String())
				}
				waitRefreshIdle(t, s)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("refreshFn called %d times within the debounce window, want 1", got)
			}
		})
	}
}

// TestRunRefresh_HasDeadline guards against a hung collector pinning the
// refresh worker forever: each server refresh runs under RefreshDeadline.
func TestRunRefresh_HasDeadline(t *testing.T) {
	deadlines := make(chan time.Time, 1)
	s := newTestServerForRefresh(t, t.TempDir(), func(ctx context.Context, _, _ string, _ appconfig.Config) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Time{}
		}
		deadlines <- deadline
		return nil
	})
	s.PreWarm()
	deadline := <-deadlines
	if deadline.IsZero() {
		t.Fatal("refresh context has no deadline")
	}
	if left := time.Until(deadline); left <= 0 || left > RefreshDeadline {
		t.Fatalf("refresh deadline in %v, want within (0, %v]", left, RefreshDeadline)
	}
	waitRefreshIdle(t, s)
}

// TestLoadData_ReloadsWhenMtimeMovesBackwards guards the data cache against
// serving stale bytes when data.json is replaced by a file with an older or
// identical mtime (restores, atomic renames, coarse filesystem clocks).
func TestLoadData_ReloadsWhenMtimeMovesBackwards(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(t)
	s.dir = dir
	path := filepath.Join(dir, "data.json")

	if err := os.WriteFile(path, []byte(`{"v":"first"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cached := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, cached, cached); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDataRawCached(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, body string
		mtime      time.Time
	}{
		{name: "older mtime", body: `{"v":"second!"}`, mtime: cached.Add(-time.Hour)},
		{name: "same mtime, new size", body: `{"v":"third!!!"}`, mtime: cached.Add(-time.Hour)},
	} {
		if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, tc.mtime, tc.mtime); err != nil {
			t.Fatal(err)
		}
		raw, err := s.GetDataRawCached()
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != tc.body {
			t.Fatalf("%s: GetDataRawCached = %s, want %s", tc.name, raw, tc.body)
		}
	}
}

// TestHandleErrors_LegacyBoundedTail pins boundedTail for file-based logs: the
// reader stops at errorLimitDefault records, so a full read must say the feed
// may be missing older entries, and a short read must not.
func TestHandleErrors_LegacyBoundedTail(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines int
		want  bool
	}{
		{name: "under cap", lines: 3, want: false},
		{name: "cap reached", lines: errorLimitDefault + 50, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			openclawDir := t.TempDir()
			stamp := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
			lines := make([]string, tc.lines)
			for i := range lines {
				lines[i] = stamp + " gateway error: failure " + fmt.Sprint(i%5)
			}
			writeLines(t, filepath.Join(openclawDir, "logs", "gateway.log"), lines...)
			cfg := appconfig.Default()
			cfg.System.Enabled = false
			cfg.Logs.Enabled = true
			cfg.Logs.Sources = []string{"logs/gateway.log"}
			s := newTestServerWithOpenclawHome(t, cfg, openclawDir)

			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/api/errors", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var payload struct {
				BoundedTail bool `json:"boundedTail"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.BoundedTail != tc.want {
				t.Fatalf("boundedTail = %v, want %v", payload.BoundedTail, tc.want)
			}
		})
	}
}

// TestHandleErrors_RejectedRequests covers the /api/errors exits that never
// read logs: logs disabled, and a legacy source asked of a runtime target.
func TestHandleErrors_RejectedRequests(t *testing.T) {
	t.Run("logs disabled", func(t *testing.T) {
		s := newTestServer(t)
		s.cfg.Logs.Enabled = false
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/api/errors", nil))
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "logs disabled") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("runtime target with legacy source", func(t *testing.T) {
		cfg := appconfig.Default()
		cfg.Logs.Enabled = true
		cfg.Openclaw = appopenclaw.Target{Mode: "container", Container: "test"}
		s := NewServer(t.TempDir(), "test", cfg, "", nil, t.Context(), nil)
		s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			t.Errorf("rejected request reached the runtime: %v", args)
			return exec.CommandContext(ctx, "printf", "%s", "{}")
		}}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/api/errors?source=cron", nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

// TestServeHTTP_WrongMethod pins 405 + Allow for known routes and 404 for
// unknown paths regardless of method.
func TestServeHTTP_WrongMethod(t *testing.T) {
	s := newTestServer(t)
	for _, tc := range []struct {
		method, path string
		status       int
		allow        string
	}{
		{http.MethodDelete, "/api/system", http.StatusMethodNotAllowed, "GET, HEAD, OPTIONS"},
		{http.MethodPost, "/", http.StatusMethodNotAllowed, "GET, HEAD, OPTIONS"},
		{http.MethodPost, "/themes.json", http.StatusMethodNotAllowed, "GET, HEAD, OPTIONS"},
		{http.MethodPut, "/api/chat", http.StatusMethodNotAllowed, "OPTIONS, POST"},
		{http.MethodDelete, "/api/operations", http.StatusMethodNotAllowed, "OPTIONS, POST"},
		{http.MethodPost, "/nonexistent", http.StatusNotFound, ""},
		{http.MethodDelete, "/nonexistent", http.StatusNotFound, ""},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(tc.method, "http://localhost"+tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d want %d", w.Code, tc.status)
			}
			if got := w.Header().Get("Allow"); got != tc.allow {
				t.Fatalf("Allow = %q, want %q", got, tc.allow)
			}
		})
	}
}

// TestHandleChat_HugeMaxHistoryDoesNotPanic guards the history buffer sizing:
// it must follow the request, not a configured maxHistory that was never
// clamped (e.g. a Config built in code rather than by appconfig.Load).
func TestHandleChat_HugeMaxHistoryDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	writeMinimalDataJSON(t, dir)
	s := chatTestServer(t, dir, 1)
	s.cfg.AI.MaxHistory = math.MaxInt
	transport := &chatOriginTransport{}
	s.httpClient = &http.Client{Transport: transport}

	w := mustPostChat(t, s, `{"question":"hello","history":[{"role":"user","content":"hi"}]}`)
	if w.Code != http.StatusOK || transport.calls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, transport.calls, w.Body.String())
	}
}

// TestHandleChat_InvalidDashboardData covers the 500 answer when data.json
// exists but cannot be used to build the prompt.
func TestHandleChat_InvalidDashboardData(t *testing.T) {
	for _, body := range []string{`null`, `{"broken":`} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			s := chatTestServer(t, dir, 1)
			if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			transport := &chatOriginTransport{}
			s.httpClient = &http.Client{Transport: transport}

			w := mustPostChat(t, s, `{"question":"hello"}`)
			if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "dashboard data is invalid") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if transport.calls != 0 {
				t.Fatalf("invalid data reached the gateway %d times", transport.calls)
			}
		})
	}
}

// TestOperationsStatusRoute pins when the SPA may offer mutations: operations
// enabled in config and an operator token of at least minOperatorTokenLen.
func TestOperationsStatusRoute(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		token   string
		want    bool
	}{
		{"enabled with token", true, strings.Repeat("x", minOperatorTokenLen), true},
		{"short token", true, strings.Repeat("x", minOperatorTokenLen-1), false},
		{"disabled in config", false, strings.Repeat("x", minOperatorTokenLen), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := operationsTestServer(t, t.TempDir())
			s.cfg.Operations.Enabled = tc.enabled
			s.operatorToken = tc.token
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/api/operations/status", nil))
			var payload struct{ Enabled bool }
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || payload.Enabled != tc.want {
				t.Fatalf("status=%d enabled=%v want %v", w.Code, payload.Enabled, tc.want)
			}
		})
	}
}

// TestOperationRuntimeErrorStatuses pins the HTTP status of a mutation the
// runtime refused, and the pre-execution 400 for a missing revision.
func TestOperationRuntimeErrorStatuses(t *testing.T) {
	for _, tc := range []struct {
		name, revision, stderr, want string
		status                       int
	}{
		{name: "permission denied", revision: "rev", stderr: "missing scope", want: "permission_denied", status: http.StatusForbidden},
		{name: "unsupported", revision: "rev", stderr: "unknown method", want: "unsupported", status: http.StatusNotImplemented},
		{name: "missing revision", revision: "", want: "exact_automation_revision_required", status: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := operationsTestServer(t, t.TempDir())
			s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				if tc.revision == "" {
					t.Errorf("rejected request reached the runtime: %v", args)
				}
				if args[2] == "cron.get" {
					return exec.CommandContext(ctx, "printf", "%s", `{"id":"job","configRevision":"rev","enabled":true}`)
				}
				return exec.CommandContext(ctx, "sh", "-c", "printf '%s' '"+tc.stderr+"' >&2; exit 1")
			}}
			body := fmt.Sprintf(`{"operationId":"12345678-1234-1234-1234-123456789012","action":"automation.disable","id":"job","configRevision":%q}`, tc.revision)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, operationRequestFor(t, s, body))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("status=%d body=%s want %d/%s", w.Code, w.Body.String(), tc.status, tc.want)
			}
		})
	}
}
