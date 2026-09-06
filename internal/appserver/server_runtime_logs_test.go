package appserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appconfig"
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
	"github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

// TestRuntimeLogCacheRecoversAfterPanic pins that a panicking read leaves the
// cache usable. If the leader's bookkeeping is not deferred, every later poll
// waits on the closed channel and serves an empty log page forever.
func TestRuntimeLogCacheRecoversAfterPanic(t *testing.T) {
	var cache runtimeLogCache
	calls := 0
	fetch := func() (apprefresh.RuntimeLogs, error) {
		calls++
		if calls == 1 {
			panic("runtime log runner exploded")
		}
		return apprefresh.RuntimeLogs{Cursor: 7}, nil
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic did not reach the caller")
			}
		}()
		_, _ = cache.read(t.Context(), fetch)
	}()

	snapshot, err := cache.read(t.Context(), fetch)
	if err != nil || snapshot.Cursor != 7 || calls != 2 {
		t.Fatalf("cache did not recover: snapshot=%+v err=%v calls=%d", snapshot, err, calls)
	}
}

// TestRuntimeLogsWaiterSurvivesLeaderCancellation pins that the client which
// happened to start the shared read cannot cancel it for everyone else: the
// waiter still gets its page, and only one CLI process runs.
func TestRuntimeLogsWaiterSurvivesLeaderCancellation(t *testing.T) {
	cfg := appconfig.Default()
	cfg.Openclaw = appopenclaw.Target{Mode: "container", Container: "test"}
	cfg.Logs.Enabled = true
	s := NewServer(t.TempDir(), "test", cfg, "", nil, t.Context(), nil)
	var mu sync.Mutex
	calls := 0
	started := make(chan struct{})
	s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			close(started)
		}
		return exec.CommandContext(ctx, "sh", "-c", `sleep 0.3; printf '%s' '{"lines":[],"cursor":3}'`)
	}}

	leaderCtx, cancelLeader := context.WithCancel(t.Context())
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "/api/logs?source=gateway", nil).WithContext(leaderCtx))
	}()

	<-started
	cancelLeader()

	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/logs?source=gateway", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("waiter inherited the leader's cancellation: %d %s", w.Code, w.Body.String())
	}
	<-leaderDone
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("calls=%d want a single shared runtime read", calls)
	}
}

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
	for range 2 {
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

// TestRuntimeLogsDoNotCacheCallerCancellation pins that a client that goes away
// mid-poll cannot poison the shared 5 s log cache for every other client.
func TestRuntimeLogsDoNotCacheCallerCancellation(t *testing.T) {
	cfg := appconfig.Default()
	cfg.Openclaw = appopenclaw.Target{Mode: "container", Container: "test"}
	cfg.Logs.Enabled = true
	s := NewServer(t.TempDir(), "test", cfg, "", nil, t.Context(), nil)
	calls := 0
	s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		calls++
		return exec.CommandContext(ctx, "printf", "%s", `{"lines":["2026-09-05T00:00:00Z error boom"],"cursor":7}`)
	}}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	first := httptest.NewRecorder()
	s.ServeHTTP(first, httptest.NewRequest("GET", "/api/logs?source=gateway", nil).WithContext(cancelled))
	if first.Code == http.StatusOK {
		t.Fatalf("cancelled caller served a response: %d %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	s.ServeHTTP(second, httptest.NewRequest("GET", "/api/logs?source=gateway", nil))
	if second.Code != http.StatusOK || calls != 1 {
		t.Fatalf("healthy caller inherited the cancelled read: code=%d calls=%d body=%s", second.Code, calls, second.Body.String())
	}
}

// TestRuntimeLogsSingleFlight pins that concurrent pollers share one CLI exec
// rather than each launching their own.
func TestRuntimeLogsSingleFlight(t *testing.T) {
	cfg := appconfig.Default()
	cfg.Openclaw = appopenclaw.Target{Mode: "container", Container: "test"}
	cfg.Logs.Enabled = true
	s := NewServer(t.TempDir(), "test", cfg, "", nil, t.Context(), nil)
	var mu sync.Mutex
	calls := 0
	s.runtimeClient = appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		mu.Lock()
		calls++
		mu.Unlock()
		return exec.CommandContext(ctx, "sh", "-c", `sleep 0.2; printf '%s' '{"lines":[],"cursor":1}'`)
	}}

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest("GET", "/api/logs?source=gateway", nil))
			if w.Code != http.StatusOK {
				t.Errorf("concurrent poll failed: %d %s", w.Code, w.Body.String())
			}
		})
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("calls=%d want a single shared runtime read", calls)
	}
}
