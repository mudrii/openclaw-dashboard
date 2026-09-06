package apprefresh

import (
	"context"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// TestRuntimeGatewayNeverUsesHostPIDForContainer pins the container invariant:
// host loopback and host process probes describe the host, not the container,
// so gateway health reports "unknown" with a reason instead of a plausible
// host-derived status.
func TestRuntimeGatewayNeverUsesHostPIDForContainer(t *testing.T) {
	oldProbe, oldPgrep := healthzProbe, pgrepGateway
	t.Cleanup(func() { healthzProbe, pgrepGateway = oldProbe, oldPgrep })
	healthzProbe = func(context.Context, int) bool {
		t.Error("container collection probed the host loopback")
		return true
	}
	pgrepGateway = func(context.Context) ([]byte, error) {
		t.Error("container collection probed host PIDs")
		return []byte("123"), nil
	}
	ctx := appopenclaw.WithTarget(t.Context(), appopenclaw.Target{Mode: "container", Container: "gateway"})

	gw := collectGatewayHealthWithLock(ctx, t.TempDir(), 18789)

	if gw["status"] != "unknown" {
		t.Fatalf("status = %v, want unknown; gateway=%v", gw["status"], gw)
	}
	if gw["processScope"] != "container" {
		t.Fatalf("processScope = %v, want container; gateway=%v", gw["processScope"], gw)
	}
	if gw["statusReason"] != "host_probe_not_applicable" {
		t.Fatalf("statusReason = %v, want host_probe_not_applicable; gateway=%v", gw["statusReason"], gw)
	}
	if gw["pid"] != nil {
		t.Fatalf("pid = %v, want nil; gateway=%v", gw["pid"], gw)
	}
}
