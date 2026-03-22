package appsystem

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

func TestGetLatestVersionCached_FailureIsNegativelyCached(t *testing.T) {
	prev := fetchLatestVersion
	defer func() { fetchLatestVersion = prev }()

	var calls atomic.Int32
	fetchLatestVersion = func(ctx context.Context, timeoutMs int) string {
		calls.Add(1)
		return ""
	}

	svc := NewSystemService(appconfig.SystemConfig{
		Enabled:            true,
		VersionsTTLSeconds: 60,
		GatewayTimeoutMs:   100,
	}, "test", context.Background())

	_ = svc.getLatestVersionCached()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		svc.latestMu.RLock()
		refreshing := svc.latestRefresh
		cachedAt := svc.latestAt
		svc.latestMu.RUnlock()
		if !refreshing && !cachedAt.IsZero() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected one failed fetch, got %d", got)
	}

	_ = svc.getLatestVersionCached()
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected failed fetch to be cached within TTL, got %d calls", got)
	}
}
