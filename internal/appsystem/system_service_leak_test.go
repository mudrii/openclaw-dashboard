package appsystem

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// Leak checks for SystemService's background goroutines. synctest.Test fails
// with a deadlock report when any goroutine started in the bubble is still
// blocked after the test function returns, so each test only has to drive the
// component and shut it down.

// TestSystemService_BackgroundWorkExitsOnShutdown kicks both background
// goroutines (the stale-hit metrics refresh and the npm latest-version probe)
// while their collectors are blocked, then cancels the lifecycle context and
// requires both to exit.
func TestSystemService_BackgroundWorkExitsOnShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		s := NewSystemService(appconfig.SystemConfig{Enabled: true, MetricsTTLSeconds: 10, VersionsTTLSeconds: 300}, "test", ctx)
		s.refresh = func(ctx context.Context) ([]byte, bool) {
			<-ctx.Done()
			return nil, true
		}
		s.fetchLatest = func(ctx context.Context, _ int) string {
			<-ctx.Done()
			return ""
		}

		s.metricsMu.Lock()
		s.metricsPayload = []byte(`{"ok":true}`)
		s.metricsAt = time.Now().Add(-time.Hour)
		s.metricsMu.Unlock()

		if code, _ := s.GetJSON(context.Background()); code != 200 {
			t.Fatalf("stale GetJSON code = %d, want 200", code)
		}
		_ = s.getLatestVersionCached()
		synctest.Wait() // both background goroutines are now blocked on ctx

		s.metricsMu.RLock()
		refreshing := s.metricsRefresh
		s.metricsMu.RUnlock()
		if !refreshing {
			t.Fatal("stale hit did not start a background refresh")
		}

		cancel()
		synctest.Wait()

		s.metricsMu.RLock()
		refreshing = s.metricsRefresh
		s.metricsMu.RUnlock()
		if refreshing {
			t.Error("background refresh still marked in flight after shutdown")
		}
	})
}

// TestSystemService_ColdPathBoundedWithHungCollectors runs the real
// refreshMetrics with every collector hung. The cold-path timeout must return
// a degraded payload; collectors that honour ctx exit with it, and the one
// statfs goroutine that cannot be cancelled exits once the syscall returns.
func TestSystemService_ColdPathBoundedWithHungCollectors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cfg := appconfig.SystemConfig{
			Enabled: true, MetricsTTLSeconds: 10, VersionsTTLSeconds: 300,
			ColdPathTimeoutMs: 500, CPUTimeoutMs: 1000, DiskPath: "/",
		}
		s := NewSystemService(cfg, "test", ctx)
		// Warm version caches so refreshMetrics never shells out.
		s.verCached, s.verAt = SystemVersions{Dashboard: "test"}, time.Now()
		s.latestAt = time.Now()
		s.binOnce.Do(func() { s.binPath = "openclaw" })

		statfsReturns := make(chan struct{})
		s.collectDisk = func(path string) SystemDisk {
			<-statfsReturns // a hung statfs ignores ctx
			return SystemDisk{Path: path}
		}
		s.collectCPURAMSwap = func(ctx context.Context, _ int) (SystemCPU, SystemRAM, SystemSwap) {
			<-ctx.Done()
			e := ctx.Err().Error()
			return SystemCPU{Error: &e}, SystemRAM{Error: &e}, SystemSwap{Error: &e}
		}
		s.collectOpenclaw = func(ctx context.Context, _ string) SystemOpenclaw {
			<-ctx.Done()
			return SystemOpenclaw{Errors: []string{ctx.Err().Error()}}
		}

		start := time.Now()
		data, hardFail := s.refreshMetrics(context.Background())
		if elapsed := time.Since(start); elapsed != 500*time.Millisecond {
			t.Errorf("cold path took %v, want the 500ms budget", elapsed)
		}
		if data == nil || !hardFail {
			t.Fatalf("refreshMetrics = (%d bytes, hardFail %v), want a hard-fail payload", len(data), hardFail)
		}

		// A second refresh while statfs is still hung must not stack another
		// statfs goroutine.
		_, _ = s.refreshMetrics(context.Background())

		close(statfsReturns)
		synctest.Wait()
		if s.diskInFlight.Load() {
			t.Error("diskInFlight still set after statfs returned")
		}
	})
}
