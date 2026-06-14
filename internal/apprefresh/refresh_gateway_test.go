package apprefresh

import (
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"
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
	var ctx context.Context
	gw := collectGatewayHealth(ctx, 0)
	if gw["status"] != "offline" {
		t.Fatalf("status: want offline, got %v", gw["status"])
	}
}

// TestParseReadyzFailing covers the /readyz body parser used by INT-1: it must
// extract failing[] from the gateway's readiness payload regardless of the
// "ready" flag, tolerate a missing failing key, and return nil on malformed
// JSON so the caller can fall back to the activity heuristic. A 503 response
// still carries a JSON body, so the same parse path applies.
func TestParseReadyzFailing(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "failing channels extracted",
			body: `{"ready":false,"failing":["telegram","startup-sidecars"],"uptimeMs":12}`,
			want: []string{"telegram", "startup-sidecars"},
		},
		{
			name: "ready with no failing key",
			body: `{"ready":true,"uptimeMs":12}`,
			want: nil,
		},
		{
			name: "empty failing array",
			body: `{"ready":true,"failing":[]}`,
			want: nil,
		},
		{
			name: "malformed json yields nil",
			body: `{not json`,
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseReadyzFailing([]byte(tc.body))
			if !slices.Equal(got, tc.want) {
				t.Errorf("parseReadyzFailing(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

// TestReadyzProbe_PortZeroReturnsFalse proves the probe-failure contract used by
// the INT-1 fallback: with no configured port the real readyzProbe returns
// (nil, false) without any network call, so collectDashboardData passes nil
// failing[] to backfillChannelConnectivity and the activity heuristic stands.
func TestReadyzProbe_PortZeroReturnsFalse(t *testing.T) {
	failing, ok := readyzProbe(context.Background(), 0)
	if ok {
		t.Errorf("ok = true for port 0, want false")
	}
	if failing != nil {
		t.Errorf("failing = %v for port 0, want nil", failing)
	}
}

// TestFormatUptimeSince covers the INT-3 lock-derived uptime formatting across
// its day/hour/minute branches and the future-timestamp clamp.
func TestFormatUptimeSince(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		when time.Time
		want string
	}{
		{"minutes", now.Add(-30 * time.Minute), "30m"},
		{"hours", now.Add(-90 * time.Minute), "1h 30m"},
		{"days", now.Add(-25 * time.Hour), "1d 1h"},
		{"future clamps to zero", now.Add(2 * time.Hour), "0m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatUptimeSince(tc.when); got != tc.want {
				t.Errorf("formatUptimeSince = %q, want %q", got, tc.want)
			}
		})
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

// TestCollectGatewayHealth_PortFieldPopulated pins the dashboard wire-format
// contract: the gateway block in data.json must carry the gateway port so
// the UI's Gateway Panel can render it without round-tripping through
// agentConfig.gateway.port. Prior releases (≤ v2026.5.21) omitted the field
// entirely; the panel displayed an empty port column even when the
// configured value was correct.
func TestCollectGatewayHealth_PortFieldPopulated(t *testing.T) {
	stubHealthz(t, true)
	stubPgrep(t, "", nil)
	gw := collectGatewayHealth(context.Background(), 18789)
	port, ok := gw["port"].(int)
	if !ok {
		t.Fatalf("port: want int field, got %T (%v)", gw["port"], gw["port"])
	}
	if port != 18789 {
		t.Fatalf("port: want 18789, got %d", port)
	}

	// Port must mirror the caller's value even when the gateway is offline,
	// so the UI can show "configured port 18789, status offline" instead of
	// a blank cell.
	stubHealthz(t, false)
	stubPgrep(t, "", nil)
	gw = collectGatewayHealth(context.Background(), 12345)
	if gw["status"] != "offline" {
		t.Fatalf("status: want offline, got %v", gw["status"])
	}
	if gw["port"] != 12345 {
		t.Fatalf("port: want 12345 even when offline, got %v", gw["port"])
	}
}
