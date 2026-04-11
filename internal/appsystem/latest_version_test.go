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

	// Use a channel to block mock goroutines until we're done with the test,
	// preventing races on the fetchLatestVersion global.
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

	// Let the background goroutine finish
	svc.latestMu.RLock()
	for svc.latestRefresh {
		svc.latestMu.RUnlock()
		time.Sleep(10 * time.Millisecond)
		svc.latestMu.RLock()
	}
	svc.latestMu.RUnlock()

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

	// Let background goroutine finish
	svc.latestMu.RLock()
	for svc.latestRefresh {
		svc.latestMu.RUnlock()
		time.Sleep(10 * time.Millisecond)
		svc.latestMu.RLock()
	}
	svc.latestMu.RUnlock()

	if got := fetchCount.Load(); got != 1 {
		t.Errorf("expected exactly 1 fetch after cache expiry, got %d", got)
	}

	// Restore after all goroutines complete
	fetchLatestVersion = original
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
	time.Sleep(50 * time.Millisecond)

	svc.latestMu.RLock()
	v = svc.latestVer
	svc.latestMu.RUnlock()
	if v != "2026.4.11-new" {
		t.Errorf("expected updated value '2026.4.11-new', got %q", v)
	}

	fetchLatestVersion = original
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
	fetchLatestVersion = func(_ context.Context, _ int) string {
		return "" // simulate failure
	}

	svc := NewSystemService(cfg, "test", context.Background())

	svc.getLatestVersionCached()

	// Wait for goroutine to finish
	svc.latestMu.RLock()
	for svc.latestRefresh {
		svc.latestMu.RUnlock()
		time.Sleep(10 * time.Millisecond)
		svc.latestMu.RLock()
	}
	svc.latestMu.RUnlock()

	svc.latestMu.RLock()
	at := svc.latestAt
	svc.latestMu.RUnlock()

	if at.IsZero() {
		t.Error("expected latestAt to be set even on fetch failure (negative caching)")
	}

	fetchLatestVersion = original
}
