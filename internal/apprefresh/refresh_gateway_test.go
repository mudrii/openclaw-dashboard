package apprefresh

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
)

// stubPgrep replaces pgrepGateway with a deterministic fake and returns a
// restore func. Use t.Cleanup to undo the override after each test.
func stubPgrep(t *testing.T, output string, err error) {
	t.Helper()
	prev := pgrepGateway
	pgrepGateway = func(_ context.Context) ([]byte, error) {
		return []byte(output), err
	}
	t.Cleanup(func() { pgrepGateway = prev })
}

func TestCollectGatewayHealth_PgrepNoMatchReturnsOffline(t *testing.T) {
	// pgrep exits 1 with empty output when no process matches the pattern.
	// exec.Cmd surfaces this as *exec.ExitError. We model both fields.
	stubPgrep(t, "", errors.New("exit status 1"))
	gw := collectGatewayHealth(context.Background(), 0)
	if gw["status"] != "offline" {
		t.Fatalf("status: want offline, got %v", gw["status"])
	}
	if gw["pid"] != nil {
		t.Fatalf("pid: want nil, got %v", gw["pid"])
	}
}

func TestCollectGatewayHealth_PgrepEmptyOutputReturnsOffline(t *testing.T) {
	// pgrep succeeded but returned no PIDs (unusual but possible).
	stubPgrep(t, "", nil)
	gw := collectGatewayHealth(context.Background(), 0)
	if gw["status"] != "offline" {
		t.Fatalf("status: want offline, got %v", gw["status"])
	}
}

func TestCollectGatewayHealth_OnlySelfPidIsFiltered(t *testing.T) {
	// When pgrep returns only our own PID, the self-match filter should
	// produce an empty effective PID list and the function should report
	// offline. This guards against the dashboard accidentally matching
	// itself if the pgrep pattern ever became too broad.
	selfPid := strconv.Itoa(os.Getpid())
	stubPgrep(t, selfPid+"\n", nil)
	gw := collectGatewayHealth(context.Background(), 0)
	if gw["status"] != "offline" {
		t.Fatalf("status: want offline, got %v", gw["status"])
	}
}

func TestCollectGatewayHealth_GatewayPidProducesOnline(t *testing.T) {
	// pgrep returns a non-self PID. Function should select it, mark status
	// online, and surface the pid as int. The downstream ps call may fail
	// for our fake PID — that is fine; uptime/rss are best-effort metadata
	// and an absent ps result must not flip status back to offline.
	stubPgrep(t, "99999\n", nil)
	gw := collectGatewayHealth(context.Background(), 0)
	if gw["status"] != "online" {
		t.Fatalf("status: want online, got %v", gw["status"])
	}
	if gw["pid"] != 99999 {
		t.Fatalf("pid: want 99999, got %v", gw["pid"])
	}
}

func TestCollectGatewayHealth_SelfPidMixedWithGatewayPidSkipsSelf(t *testing.T) {
	// pgrep returns the dashboard's own PID first and the real gateway
	// second. The loop must skip self and pick the gateway.
	selfPid := strconv.Itoa(os.Getpid())
	stubPgrep(t, selfPid+"\n88888\n", nil)
	gw := collectGatewayHealth(context.Background(), 0)
	if gw["status"] != "online" {
		t.Fatalf("status: want online, got %v", gw["status"])
	}
	if gw["pid"] != 88888 {
		t.Fatalf("pid: want 88888, got %v", gw["pid"])
	}
}

func TestCollectGatewayHealth_NonNumericPidReturnsOffline(t *testing.T) {
	// Defensive guard for the strconv.Atoi failure path.
	stubPgrep(t, "not-a-pid\n", nil)
	gw := collectGatewayHealth(context.Background(), 0)
	if gw["status"] != "offline" {
		t.Fatalf("status: want offline, got %v", gw["status"])
	}
}

func TestCollectGatewayHealth_NilContextDoesNotPanic(t *testing.T) {
	// The function explicitly substitutes context.Background() when ctx is
	// nil. This case proves the substitution path does not panic on the
	// pgrep timeout setup.
	stubPgrep(t, "", nil)
	gw := collectGatewayHealth(nil, 0) //nolint:staticcheck // intentional nil ctx
	if gw["status"] != "offline" {
		t.Fatalf("status: want offline, got %v", gw["status"])
	}
}

// stubHealthz replaces the HTTP probe with a deterministic fake.
func stubHealthz(t *testing.T, ok bool) {
	t.Helper()
	prev := healthzProbe
	healthzProbe = func(_ context.Context, _ int) bool { return ok }
	t.Cleanup(func() { healthzProbe = prev })
}

// TestCollectGatewayHealth_HTTPOnlinePgrepEmpty proves that a successful
// /healthz response promotes status to online even when pgrep finds no PID —
// this is the SMELL-2 consolidation: HTTP is the authoritative liveness
// signal. Pid/uptime/rss remain unset because pgrep produced nothing.
func TestCollectGatewayHealth_HTTPOnlinePgrepEmpty(t *testing.T) {
	stubHealthz(t, true)
	stubPgrep(t, "", nil)
	gw := collectGatewayHealth(context.Background(), 18789)
	if gw["status"] != "online" {
		t.Fatalf("status: want online, got %v", gw["status"])
	}
	if gw["pid"] != nil {
		t.Fatalf("pid: want nil (pgrep empty), got %v", gw["pid"])
	}
}

// TestCollectGatewayHealth_HTTPOfflinePgrepFinds proves the reverse path:
// when HTTP fails but pgrep finds the gateway PID, status is still online.
// Process liveness is a sufficient secondary signal so a transient HTTP
// hiccup does not flip the banner red.
func TestCollectGatewayHealth_HTTPOfflinePgrepFinds(t *testing.T) {
	stubHealthz(t, false)
	stubPgrep(t, "77777\n", nil)
	gw := collectGatewayHealth(context.Background(), 18789)
	if gw["status"] != "online" {
		t.Fatalf("status: want online, got %v", gw["status"])
	}
	if gw["pid"] != 77777 {
		t.Fatalf("pid: want 77777, got %v", gw["pid"])
	}
}

// TestCollectGatewayHealth_HTTPOnlinePgrepError covers the case where pgrep
// errors entirely (binary missing, permission denied) but HTTP confirms the
// gateway is up. Status must still be online.
func TestCollectGatewayHealth_HTTPOnlinePgrepError(t *testing.T) {
	stubHealthz(t, true)
	stubPgrep(t, "", errors.New("exit status 127"))
	gw := collectGatewayHealth(context.Background(), 18789)
	if gw["status"] != "online" {
		t.Fatalf("status: want online, got %v", gw["status"])
	}
}

// TestCollectGatewayHealth_PortZeroSkipsHTTP proves that port==0 disables
// the HTTP probe entirely — the previous pgrep-only behavior is preserved
// for callers that have not yet plumbed a port through.
func TestCollectGatewayHealth_PortZeroSkipsHTTP(t *testing.T) {
	// healthzProbe must NOT be called when port is 0; trip a test failure
	// if it is.
	prev := healthzProbe
	healthzProbe = func(_ context.Context, _ int) bool {
		t.Fatalf("healthzProbe must not be called when port is 0")
		return false
	}
	t.Cleanup(func() { healthzProbe = prev })

	stubPgrep(t, "", nil)
	gw := collectGatewayHealth(context.Background(), 0)
	if gw["status"] != "offline" {
		t.Fatalf("status: want offline, got %v", gw["status"])
	}
}
