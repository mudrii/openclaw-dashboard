package apprefresh

import (
	"context"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func TestRuntimeGatewayNeverUsesHostPIDForContainer(t *testing.T) {
	oldProbe, oldPgrep := healthzProbe, pgrepGateway
	t.Cleanup(func() { healthzProbe, pgrepGateway = oldProbe, oldPgrep })
	healthzProbe = func(context.Context, int) bool { return true }
	pgrepGateway = func(context.Context) ([]byte, error) {
		t.Error("container collection probed host PIDs")
		return []byte("123"), nil
	}
	ctx := appopenclaw.WithTarget(t.Context(), appopenclaw.Target{Mode: "container", Container: "gateway"})
	gw := collectGatewayHealthWithLock(ctx, t.TempDir(), 18789)
	if gw["status"] != "online" || gw["pid"] != nil {
		t.Fatalf("gateway=%v", gw)
	}
}
