package appserver

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
