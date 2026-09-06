package apprefresh

import (
	"os"
	"testing"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// Opt-in because the normal test suite must not require or contact a gateway.
func TestRuntimeLiveReadContracts(t *testing.T) {
	if os.Getenv("OPENCLAW_DASHBOARD_LIVE") != "1" {
		t.Skip("set OPENCLAW_DASHBOARD_LIVE=1 for read-only runtime integration")
	}
	client := appopenclaw.Client{}
	ctx := appopenclaw.WithTarget(t.Context(), appopenclaw.Target{Container: os.Getenv("OPENCLAW_CONTAINER")})
	if appopenclaw.TargetFromContext(ctx).IsContainer() {
		if _, _, err := readSelectedConfig(ctx, client, ""); err != nil {
			t.Fatalf("selected container configuration: %v", err)
		}
	}
	sessions, err := collectRuntimeSessions(ctx, client, time.UTC, nil)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := collectRuntimeTasks(ctx, client, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := collectRuntimeUsage(ctx, client, "30d", "", "", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("read contracts passed: sessions=%d tasks=%d tokens=%d pricingMissing=%d cache=%s", len(sessions.Rows), len(tasks), usage.Totals.TotalTokens, usage.Totals.MissingCostEntries, usage.CacheStatus.Status)
}
