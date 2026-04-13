package appsystem

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

func TestGetLatestVersionCached_ConcurrentCalls_NoRace(t *testing.T) {
	cfg := appconfig.SystemConfig{
		Enabled:            true,
		VersionsTTLSeconds: 1,
		GatewayTimeoutMs:   100,
		MetricsTTLSeconds:  10,
		PollSeconds:        10,
	}

	var fetchCount atomic.Int32
	original := fetchLatestVersion
	t.Cleanup(func() { fetchLatestVersion = original })

	fetchLatestVersion = func(_ context.Context, _ int) string {
		fetchCount.Add(1)
		return "2026.4.11"
	}

	svc := NewSystemService(cfg, "test", context.Background())

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			svc.getLatestVersionCached()
		}()
	}
	wg.Wait()

	// Poll until the background goroutine finishes
	waitForLatestRefreshDone(t, svc)

	if got := fetchCount.Load(); got != 1 {
		t.Errorf("expected exactly 1 fetch, got %d", got)
	}

	// Second batch: expire cache and fire again
	svc.latestMu.Lock()
	svc.latestAt = time.Time{}
	svc.latestMu.Unlock()
	fetchCount.Store(0)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			svc.getLatestVersionCached()
		}()
	}
	wg.Wait()
	waitForLatestRefreshDone(t, svc)

	if got := fetchCount.Load(); got != 1 {
		t.Errorf("expected exactly 1 fetch after cache expiry, got %d", got)
	}
}

func TestGetLatestVersionCached_ReturnsCachedValueWhileRefreshing(t *testing.T) {
	cfg := appconfig.SystemConfig{
		Enabled:            true,
		VersionsTTLSeconds: 300,
		GatewayTimeoutMs:   100,
		MetricsTTLSeconds:  10,
		PollSeconds:        10,
	}

	original := fetchLatestVersion
	t.Cleanup(func() { fetchLatestVersion = original })

	fetched := make(chan struct{})
	fetchLatestVersion = func(_ context.Context, _ int) string {
		<-fetched
		return "2026.4.11-new"
	}

	svc := NewSystemService(cfg, "test", context.Background())

	// Pre-seed expired cache
	svc.latestMu.Lock()
	svc.latestVer = "2026.4.10-old"
	svc.latestAt = time.Now().Add(-time.Hour)
	svc.latestMu.Unlock()

	v := svc.getLatestVersionCached()
	if v != "2026.4.10-old" {
		t.Errorf("expected stale cached value '2026.4.10-old', got %q", v)
	}

	close(fetched)
	waitForLatestRefreshDone(t, svc)

	svc.latestMu.RLock()
	v = svc.latestVer
	svc.latestMu.RUnlock()
	if v != "2026.4.11-new" {
		t.Errorf("expected updated value '2026.4.11-new', got %q", v)
	}
}

func TestGetLatestVersionCached_NegativeCaching(t *testing.T) {
	cfg := appconfig.SystemConfig{
		Enabled:            true,
		VersionsTTLSeconds: 1,
		GatewayTimeoutMs:   100,
		MetricsTTLSeconds:  10,
		PollSeconds:        10,
	}

	original := fetchLatestVersion
	t.Cleanup(func() { fetchLatestVersion = original })

	fetchLatestVersion = func(_ context.Context, _ int) string {
		return "" // simulate failure
	}

	svc := NewSystemService(cfg, "test", context.Background())

	svc.getLatestVersionCached()
	waitForLatestRefreshDone(t, svc)

	svc.latestMu.RLock()
	at := svc.latestAt
	svc.latestMu.RUnlock()

	if at.IsZero() {
		t.Error("expected latestAt to be set even on fetch failure (negative caching)")
	}
}

// waitForLatestRefreshDone polls until the background goroutine finishes.
func waitForLatestRefreshDone(t *testing.T, svc *SystemService) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		svc.latestMu.RLock()
		running := svc.latestRefresh
		svc.latestMu.RUnlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for background refresh to complete")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
