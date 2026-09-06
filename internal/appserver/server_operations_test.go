package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appconfig"
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func TestOperationsAuthorizationIdentityAndExactTargets(t *testing.T) {
	for _, tc := range []struct {
		name, action, origin, auth, revision, run string
		enabled                                   bool
		code, writes                              int
	}{
		{"disabled", "automation.disable", "", "valid", "rev", "", false, 403, 0},
		{"unauthenticated", "automation.disable", "", "", "rev", "", true, 403, 0},
		{"cross-origin", "automation.disable", "http://localhost:9999", "valid", "rev", "", true, 403, 0},
		{"dns-rebinding", "automation.disable", "http://evil.test", "valid", "rev", "", true, 403, 0},
		{"revision changed", "automation.disable", "", "valid", "old", "", true, 409, 0},
		{"disable", "automation.disable", "http://localhost:8080", "valid", "rev", "", true, 200, 1},
		{"enable", "automation.enable", "", "valid", "rev", "", true, 200, 1},
		{"run", "automation.run", "", "valid", "rev", "", true, 200, 1},
		{"wrong run", "session.abort", "", "valid", "", "other", true, 409, 0},
		{"abort exact", "session.abort", "", "valid", "", "active", true, 200, 1},
		{"abort all refused", "session.abort", "", "valid", "", "", true, 400, 0},
		{"arbitrary write refused", "config.set", "", "valid", "rev", "", true, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := appconfig.Default()
			cfg.Operations.Enabled = tc.enabled
			s := NewServer(t.TempDir(), "test", cfg, "gateway-secret", nil, t.Context(), nil)
			s.operatorToken = strings.Repeat("x", 32)
			writes := 0
			s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				body := `{"ok":true}`
				switch args[2] {
				case "cron.get":
					body = `{"id":"job","configRevision":"rev","enabled":true}`
				case "sessions.list":
					body = `{"sessions":[{"key":"session","hasActiveRun":true,"activeRunIds":["active"]}],"hasMore":false}`
				default:
					writes++
				}
				return exec.CommandContext(ctx, "printf", "%s", body)
			}}
			body := fmt.Sprintf(`{"operationId":"12345678-1234-1234-1234-123456789012","action":%q,"id":"job","configRevision":%q,"sessionKey":"session","runId":%q}`, tc.action, tc.revision, tc.run)
			r := httptest.NewRequest("POST", "http://localhost:8080/api/operations", strings.NewReader(body))
			r.RemoteAddr = "127.0.0.1:10000"
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", tc.origin)
			if tc.auth == "valid" {
				r.Header.Set("Authorization", "Bearer "+s.operatorToken)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.code || writes != tc.writes {
				t.Fatalf("status=%d writes=%d body=%s", w.Code, writes, w.Body)
			}
			if tc.writes > 0 {
				data, err := os.ReadFile(filepath.Join(s.dir, "operations", "12345678-1234-1234-1234-123456789012.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "secret") || strings.Contains(string(data), s.operatorToken) || !strings.Contains(string(data), `"outcome":"applied"`) {
					t.Fatalf("unsafe/incomplete audit: %s", data)
				}
				r2 := httptest.NewRequest("POST", "http://localhost:8080/api/operations", strings.NewReader(body))
				r2.RemoteAddr = r.RemoteAddr
				r2.Header = r.Header.Clone()
				w2 := httptest.NewRecorder()
				restarted := NewServer(s.dir, "test", cfg, "", nil, t.Context(), nil)
				restarted.operatorToken = s.operatorToken
				restarted.runtimeClient = s.runtimeClient
				restarted.ServeHTTP(w2, r2)
				if w2.Code != 409 || writes != 1 {
					t.Fatalf("duplicate executed: %d %d", w2.Code, writes)
				}
			}
		})
	}
}

func TestLocalOperationsRejectRemoteAndForgedHosts(t *testing.T) {
	for _, tc := range []struct {
		host, peer string
		want       bool
	}{{"localhost:8080", "127.0.0.1:10", true}, {"127.0.0.1:8080", "127.0.0.1:10", true}, {"evil.test", "127.0.0.1:10", false}, {"localhost:8080", "192.0.2.1:10", false}, {"localhost:8080", "bad", false}} {
		r := httptest.NewRequest("POST", "http://"+tc.host+"/api/operations", nil)
		r.RemoteAddr = tc.peer
		if localOperationRequest(r) != tc.want {
			t.Fatalf("host=%s peer=%s", tc.host, tc.peer)
		}
	}
}

// operationRequestFor builds an authorized loopback operation request so the
// outcome tests only vary the runtime behaviour under test.
func operationRequestFor(t *testing.T, s *Server, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/operations", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:10000"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+s.operatorToken)
	return r
}

func operationsTestServer(t *testing.T, dir string) *Server {
	t.Helper()
	cfg := appconfig.Default()
	cfg.Operations.Enabled = true
	s := NewServer(dir, "test", cfg, "gateway-secret", nil, t.Context(), nil)
	s.operatorToken = strings.Repeat("x", 32)
	return s
}

// TestOperationOutcomesReflectRuntimeResult pins the audited outcome for each
// way a mutation can end: applied, refused by the runtime, lost mid-flight, or
// rejected before execution.
func TestOperationOutcomesReflectRuntimeResult(t *testing.T) {
	for _, tc := range []struct {
		name, action, job, write, outcome, code string
		status                                  int
	}{
		{
			name: "write failure is unknown", action: "automation.disable",
			job: `{"id":"job","configRevision":"rev","enabled":true}`, write: "fail",
			outcome: "unknown", code: "unavailable", status: http.StatusBadGateway,
		},
		{
			name: "runtime refused to run", action: "automation.run",
			job: `{"id":"job","configRevision":"rev","enabled":true}`, write: `{"ran":false}`,
			outcome: "not_applied", code: "ok", status: http.StatusOK,
		},
		{
			name: "disabled job is a conflict", action: "automation.run",
			job: `{"id":"job","configRevision":"rev","enabled":false}`, write: `{"ran":true}`,
			outcome: "not_applied", code: "target_changed_or_inactive", status: http.StatusConflict,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s := operationsTestServer(t, dir)
			s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				if args[2] == "cron.get" {
					return exec.CommandContext(ctx, "printf", "%s", tc.job)
				}
				if tc.write == "fail" {
					return exec.CommandContext(ctx, "sh", "-c", "printf '%s' 'gateway closed' >&2; exit 1")
				}
				return exec.CommandContext(ctx, "printf", "%s", tc.write)
			}}
			body := fmt.Sprintf(`{"operationId":"12345678-1234-1234-1234-123456789012","action":%q,"id":"job","configRevision":"rev"}`, tc.action)

			w := httptest.NewRecorder()
			s.ServeHTTP(w, operationRequestFor(t, s, body))

			if w.Code != tc.status {
				t.Fatalf("status=%d want %d body=%s", w.Code, tc.status, w.Body.String())
			}
			var payload struct{ Outcome, ErrorCode string }
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Outcome != tc.outcome || payload.ErrorCode != tc.code {
				t.Fatalf("payload=%+v want %s/%s", payload, tc.outcome, tc.code)
			}
			audit, err := os.ReadFile(filepath.Join(dir, "operations", "12345678-1234-1234-1234-123456789012.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(audit), `"outcome":"`+tc.outcome+`"`) {
				t.Fatalf("audit missing final outcome: %s", audit)
			}
		})
	}
}

// TestOperationRequestGuards pins the pre-execution rejections that must never
// reach the runtime.
func TestOperationRequestGuards(t *testing.T) {
	const validBody = `{"operationId":"12345678-1234-1234-1234-123456789012","action":"automation.disable","id":"job","configRevision":"rev"}`
	for _, tc := range []struct {
		name, contentType, body, want string
		burnLimiter                   bool
		status                        int
	}{
		{name: "wrong content type", contentType: "text/plain", body: validBody, want: "json_required", status: http.StatusUnsupportedMediaType},
		{name: "unknown field", contentType: "application/json", body: `{"operationId":"12345678-1234-1234-1234-123456789012","action":"automation.disable","id":"job","configRevision":"rev","extra":1}`, want: "invalid_operation", status: http.StatusBadRequest},
		{name: "rate limited", contentType: "application/json", body: validBody, want: "rate_limited", burnLimiter: true, status: http.StatusTooManyRequests},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := operationsTestServer(t, t.TempDir())
			s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				t.Errorf("rejected request reached the runtime: %v", args)
				return exec.CommandContext(ctx, "printf", "%s", "{}")
			}}
			if tc.burnLimiter {
				for range chatRateLimit {
					s.operationLimiter.allow("operator")
				}
			}
			r := operationRequestFor(t, s, tc.body)
			r.Header.Set("Content-Type", tc.contentType)

			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)

			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("status=%d body=%s want %d/%s", w.Code, w.Body.String(), tc.status, tc.want)
			}
		})
	}
}

// TestOperationAuditPruningKeepsNewest pins the retention bound on the audit
// directory: unbounded growth would fill the dashboard volume over time.
func TestOperationAuditPruningKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	auditDir := filepath.Join(dir, "operations")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldest := filepath.Join(auditDir, "0000-oldest.jsonl")
	base := time.Now().Add(-24 * time.Hour)
	for i := range maxOperationAuditFiles + 5 {
		name := filepath.Join(auditDir, fmt.Sprintf("%04d-old.jsonl", i))
		if i == 0 {
			name = oldest
		}
		if err := os.WriteFile(name, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(name, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}

	s := operationsTestServer(t, dir)
	s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		if args[2] == "cron.get" {
			return exec.CommandContext(ctx, "printf", "%s", `{"id":"job","configRevision":"rev","enabled":true}`)
		}
		return exec.CommandContext(ctx, "printf", "%s", `{"ok":true}`)
	}}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, operationRequestFor(t, s, `{"operationId":"12345678-1234-1234-1234-123456789012","action":"automation.disable","id":"job","configRevision":"rev"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("operation failed: %d %s", w.Code, w.Body.String())
	}

	entries, err := os.ReadDir(auditDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxOperationAuditFiles {
		t.Fatalf("audit files=%d want %d", len(entries), maxOperationAuditFiles)
	}
	if _, err := os.Stat(oldest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oldest audit file survived pruning: %v", err)
	}
	if _, err := os.Stat(filepath.Join(auditDir, "12345678-1234-1234-1234-123456789012.jsonl")); err != nil {
		t.Fatalf("current operation audit pruned: %v", err)
	}
}
