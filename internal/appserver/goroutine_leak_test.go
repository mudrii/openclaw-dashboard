package appserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
)

// goroutineLeakSnapshot returns the current goroutineleak profile count and its
// text rendering. Profile.WriteTo for goroutineleak runs its own leak-detecting
// GC; the explicit cycles below only make sure test-owned garbage from the
// previous step is gone first, so the baseline diff is stable.
func goroutineLeakSnapshot(tb testing.TB, prof *pprof.Profile) (int, string) {
	tb.Helper()
	runtime.GC()
	runtime.GC()
	var buf strings.Builder
	if err := prof.WriteTo(&buf, 1); err != nil {
		tb.Fatalf("write goroutineleak profile: %v", err)
	}
	return prof.Count(), buf.String()
}

// TestNoGoroutineLeakAfterShutdown locks in the shutdown contract: every
// goroutine NewServer starts must exit when the lifecycle context is cancelled.
// The goroutineleak profile is process-global and cumulative across the tests
// in this binary, so the assertion is a delta against a baseline rather than a
// bare "zero leaks".
func TestNoGoroutineLeakAfterShutdown(t *testing.T) {
	prof := pprof.Lookup("goroutineleak")
	if prof == nil {
		t.Skip("goroutineleak profile not available in this runtime")
	}

	baseline, _ := goroutineLeakSnapshot(t, prof)

	// Scoped so the Server and its context become unreachable before the
	// post-shutdown snapshot.
	func() {
		ctx, cancel := context.WithCancel(t.Context())
		dir := t.TempDir()
		srv := testServerWithCtxAndMockRefresh(t, dir, ctx)
		writeMinimalData(t, dir)

		// Drive one full cycle: the first /api/refresh spawns the refresh
		// worker, the second reads back through the data cache.
		for _, target := range []string{"/api/refresh", "/api/refresh", "/api/chat/status"} {
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("GET %s: got %d, want 200: %s", target, w.Code, w.Body.String())
			}
		}

		cancel()
	}()

	count, text := goroutineLeakSnapshot(t, prof)
	if count > baseline {
		t.Errorf("goroutine leak after server shutdown: profile count %d, baseline %d\n%s", count, baseline, text)
	}
}
