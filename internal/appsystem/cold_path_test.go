package appsystem

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// hangingGateway returns an httptest server that blocks until the client
// cancels its request (or `maxDelay` elapses, whichever comes first), then
// closes. The handler honours r.Context() so srv.Close() returns promptly
// once the cold-path deadline cancels the in-flight request.
func hangingGateway(t *testing.T, maxDelay time.Duration) (port int, close func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(maxDelay):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"ready":true,"uptimeMs":0}`))
	}))
	parts := strings.Split(srv.URL, ":")
	p, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		srv.Close()
		t.Fatalf("parse port from %q: %v", srv.URL, err)
	}
	return p, srv.Close
}

func coldPathCfg(port, coldMs int) appconfig.SystemConfig {
	return appconfig.SystemConfig{
		Enabled:            true,
		PollSeconds:        10,
		MetricsTTLSeconds:  10,
		VersionsTTLSeconds: 60,
		// GatewayTimeoutMs is intentionally very generous; the cold-path
		// budget is what should bound the wall time, not per-probe timeouts.
		GatewayTimeoutMs:  10000,
		GatewayPort:       port,
		ColdPathTimeoutMs: coldMs,
		DiskPath:          "/",
		WarnPercent:       70,
		CriticalPercent:   85,
		CPU:               appconfig.MetricThreshold{Warn: 80, Critical: 95},
		RAM:               appconfig.MetricThreshold{Warn: 80, Critical: 95},
		Swap:              appconfig.MetricThreshold{Warn: 80, Critical: 95},
		Disk:              appconfig.MetricThreshold{Warn: 80, Critical: 95},
	}
}

// newColdPathTestService returns a SystemService configured for cold-path
// tests with the openclaw binary lookup forced to a non-existent path so
// that runWithTimeout fails fast instead of probing a real installation.
// This guarantees gateway HTTP probes (against the test httptest server) are
// the only slow path.
func newColdPathTestService(cfg appconfig.SystemConfig) *SystemService {
	svc := NewSystemService(cfg, "test", context.Background())
	svc.binPath = "/nonexistent-openclaw-binary-for-cold-path-test"
	svc.binOnce.Do(func() {})
	return svc
}

// TestRefresh_ColdPath_Deadline asserts that when subcollectors hang
// indefinitely, refresh() honours ColdPathTimeoutMs and returns within the
// budget plus a small grace window — never blocking the request thread for
// the multi-second worst case described in #26.
func TestRefresh_ColdPath_Deadline(t *testing.T) {
	port, closeSrv := hangingGateway(t, 5*time.Second)
	defer closeSrv()

	cfg := coldPathCfg(port, 500)
	svc := newColdPathTestService(cfg)

	start := time.Now()
	body, _ := svc.refresh(context.Background())
	elapsed := time.Since(start)

	if body == nil {
		t.Fatal("refresh returned nil body even though host metrics are available")
	}
	// Budget 500ms + 1.5s slack for slow CI; the bug version takes ~10–12s.
	if elapsed > 2*time.Second {
		t.Fatalf("cold-path refresh took %v, want <= 2s (budget=500ms)", elapsed)
	}
}

// TestRefresh_ColdPath_DegradedFlag asserts that hitting the cold-path
// deadline produces a payload the frontend can recognise as degraded:
// Degraded=true and an error string mentioning the deadline.
func TestRefresh_ColdPath_DegradedFlag(t *testing.T) {
	port, closeSrv := hangingGateway(t, 5*time.Second)
	defer closeSrv()

	cfg := coldPathCfg(port, 500)
	svc := newColdPathTestService(cfg)

	body, _ := svc.refresh(context.Background())
	var resp SystemResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Degraded {
		t.Errorf("expected Degraded=true after cold-path deadline; resp=%+v", resp)
	}
	hasDeadline := false
	for _, e := range resp.Errors {
		if strings.Contains(strings.ToLower(e), "cold path") || strings.Contains(strings.ToLower(e), "deadline") {
			hasDeadline = true
			break
		}
	}
	if !hasDeadline {
		t.Errorf("expected cold-path/deadline message in resp.Errors, got %v", resp.Errors)
	}
}

// TestRefresh_ColdPath_HostMetricsAlwaysShipped asserts that host metrics
// (disk in particular — collected via syscall.Statfs, no I/O) are always
// in the response even when gateway probes hang. This is the contract that
// keeps the system card useful while the gateway is offline.
func TestRefresh_ColdPath_HostMetricsAlwaysShipped(t *testing.T) {
	port, closeSrv := hangingGateway(t, 5*time.Second)
	defer closeSrv()

	cfg := coldPathCfg(port, 1500)
	svc := newColdPathTestService(cfg)

	body, _ := svc.refresh(context.Background())
	var resp SystemResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Disk.Error != nil {
		t.Errorf("disk error should be nil even when gateway hangs; got %q", *resp.Disk.Error)
	}
	if resp.Disk.TotalBytes <= 0 {
		t.Errorf("expected disk totalBytes > 0; got %d", resp.Disk.TotalBytes)
	}
}

// TestRefresh_ColdPath_PoisonsNoCache asserts that when cold-path collection
// is cut short by the deadline, the version cache is NOT updated with the
// partial/empty result. Otherwise the next request would hit a poisoned
// "warm" cache and skip a real collection silently.
func TestRefresh_ColdPath_PoisonsNoCache(t *testing.T) {
	port, closeSrv := hangingGateway(t, 5*time.Second)
	defer closeSrv()

	cfg := coldPathCfg(port, 500)
	svc := newColdPathTestService(cfg)

	_, _ = svc.refresh(context.Background())

	svc.verMu.RLock()
	cachedAt := svc.verAt
	svc.verMu.RUnlock()

	if !cachedAt.IsZero() {
		t.Errorf("version cache must not be populated after cold-path deadline; verAt=%v", cachedAt)
	}
}
