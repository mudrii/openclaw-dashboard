package appserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appconfig"
	"github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

// Leak checks for the server's background goroutines. synctest.Test fails
// with a deadlock report when a goroutine started in the bubble is still
// blocked after the test function returns, so these tests only drive the
// component and shut it down; virtual time makes the 5-minute rate-limit
// cleanup tick and the refresh deadline instant.

// TestServerLifecycle_NoLeakWithRefreshInFlight shuts the server down while a
// refresh worker is blocked in the collector and after the rate-limit cleanup
// ticker has fired, and requires every goroutine NewServer started to exit.
func TestServerLifecycle_NoLeakWithRefreshInFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		dir := t.TempDir()
		writeMinimalData(t, dir)

		refreshStarted := make(chan struct{}, 1)
		refresh := func(ctx context.Context, _, _ string, _ appconfig.Config) error {
			refreshStarted <- struct{}{}
			<-ctx.Done() // a hung collector that honours cancellation
			return ctx.Err()
		}
		cfg := appconfig.Config{
			Refresh: appconfig.RefreshConfig{IntervalSeconds: 1},
			System:  appconfig.SystemConfig{Enabled: false},
		}
		srv := NewServer(dir, "test", cfg, "", []byte("<head><body></body>"), ctx, refresh)

		for _, target := range []string{"/api/refresh", "/api/chat/status", "/api/operations/status", "/api/system", "/"} {
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost"+target, nil))
			if w.Code >= 500 && target != "/api/system" {
				t.Fatalf("GET %s: %d %s", target, w.Code, w.Body.String())
			}
		}
		<-refreshStarted

		// Let the rate-limit cleanup ticker fire at least once.
		time.Sleep(chatRateCleanupInterval + time.Second)
		synctest.Wait()

		cancel()
		if err := srv.WaitRefresh(context.Background()); err != nil {
			t.Fatalf("WaitRefresh after shutdown: %v", err)
		}
		synctest.Wait()

		// Requests after shutdown must not start new background work.
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/api/refresh", nil))
		synctest.Wait()
	})
}

// TestServerRefresh_DeadlineReleasesWorker checks a collector that blocks until
// its context ends is cut off by RefreshDeadline while the server keeps
// running, so a hung OpenClaw call cannot pin the worker goroutine.
func TestServerRefresh_DeadlineReleasesWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		dir := t.TempDir()
		var gotErr error
		refresh := func(ctx context.Context, _, _ string, _ appconfig.Config) error {
			<-ctx.Done()
			gotErr = ctx.Err()
			return gotErr
		}
		srv := NewServer(dir, "test", appconfig.Config{}, "", nil, ctx, refresh)
		done := srv.startRefresh()
		start := time.Now()
		<-done
		if elapsed := time.Since(start); elapsed != RefreshDeadline {
			t.Errorf("worker released after %v, want RefreshDeadline %v", elapsed, RefreshDeadline)
		}
		if !errors.Is(gotErr, context.DeadlineExceeded) {
			t.Errorf("collector ctx err = %v, want DeadlineExceeded", gotErr)
		}
		cancel()
		synctest.Wait()
	})
}

// TestRuntimeLogCache_WaitersDoNotLeak blocks a shared gateway log read, lets
// one waiter give up via its context and another wait for the result, then
// releases the read: every caller must return and no goroutine may remain.
func TestRuntimeLogCache_WaitersDoNotLeak(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache runtimeLogCache
		release := make(chan struct{})
		fetch := func() (apprefresh.RuntimeLogs, error) {
			<-release
			return apprefresh.RuntimeLogs{Truncated: true}, nil
		}

		leaderDone := make(chan apprefresh.RuntimeLogs, 1)
		go func() {
			got, _ := cache.read(context.Background(), fetch)
			leaderDone <- got
		}()
		synctest.Wait() // leader is blocked inside fetch

		waiterCtx, cancelWaiter := context.WithCancel(context.Background())
		abandoned := make(chan error, 1)
		go func() {
			_, err := cache.read(waiterCtx, fetch)
			abandoned <- err
		}()
		patient := make(chan apprefresh.RuntimeLogs, 1)
		go func() {
			got, _ := cache.read(context.Background(), fetch)
			patient <- got
		}()
		synctest.Wait()

		cancelWaiter()
		if err := <-abandoned; !errors.Is(err, context.Canceled) {
			t.Fatalf("abandoned waiter err = %v, want context.Canceled", err)
		}

		close(release)
		if got := <-leaderDone; !got.Truncated {
			t.Error("leader did not receive the fetched snapshot")
		}
		if got := <-patient; !got.Truncated {
			t.Error("waiter did not receive the shared snapshot")
		}
		synctest.Wait()
	})
}
