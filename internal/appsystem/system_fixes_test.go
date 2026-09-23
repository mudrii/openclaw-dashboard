package appsystem

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// TestGetJSON_ColdPathSingleFlight proves concurrent cold requests share one
// collection instead of each running the full (expensive) refresh.
func TestGetJSON_ColdPathSingleFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewSystemService(appconfig.SystemConfig{Enabled: true, MetricsTTLSeconds: 60}, "test", context.Background())
		release := make(chan struct{})
		var calls atomic.Int32
		s.refresh = func(context.Context) ([]byte, bool) {
			calls.Add(1)
			<-release
			return []byte(`{"ok":true}`), false
		}

		const callers = 10
		var wg sync.WaitGroup
		codes := make([]int, callers)
		bodies := make([][]byte, callers)
		for i := range callers {
			wg.Go(func() { codes[i], bodies[i] = s.GetJSON(context.Background()) })
		}
		synctest.Wait() // every caller is now blocked on the shared collection
		close(release)
		wg.Wait()

		if got := calls.Load(); got != 1 {
			t.Errorf("refresh calls = %d, want 1", got)
		}
		for i := range callers {
			if codes[i] != http.StatusOK || string(bodies[i]) != `{"ok":true}` {
				t.Errorf("caller %d got (%d, %s), want (200, {\"ok\":true})", i, codes[i], bodies[i])
			}
		}
	})
}

// TestGetJSON_ColdPathWaiterHonorsContext: a waiter whose request is cancelled
// returns instead of blocking on another caller's collection.
func TestGetJSON_ColdPathWaiterHonorsContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewSystemService(appconfig.SystemConfig{Enabled: true, MetricsTTLSeconds: 60}, "test", context.Background())
		release := make(chan struct{})
		s.refresh = func(context.Context) ([]byte, bool) {
			<-release
			return []byte(`{"ok":true}`), false
		}
		leaderDone := make(chan struct{})
		go func() {
			defer close(leaderDone)
			s.GetJSON(context.Background())
		}()
		synctest.Wait()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if code, _ := s.GetJSON(ctx); code != http.StatusServiceUnavailable {
			t.Errorf("cancelled waiter code = %d, want 503", code)
		}
		close(release)
		<-leaderDone
	})
}

// TestGetJSON_AllFailedStays503 proves the hard-fail status is cached with the
// payload: the cold call and the following fresh-cache hit both report 503.
func TestGetJSON_AllFailedStays503(t *testing.T) {
	s := NewSystemService(appconfig.SystemConfig{Enabled: true, MetricsTTLSeconds: 60, VersionsTTLSeconds: 3600}, "test", context.Background())
	s.fetchLatest = func(context.Context, int) string { return "" }
	s.binPath = "/nonexistent-openclaw-binary"
	s.binOnce.Do(func() {})
	s.verCached = SystemVersions{Dashboard: "test"}
	s.verAt = time.Now()
	s.collectOpenclaw = func(context.Context, string) SystemOpenclaw { return SystemOpenclaw{} }
	s.collectCPURAMSwap = func(context.Context, int) (SystemCPU, SystemRAM, SystemSwap) {
		e := "boom"
		return SystemCPU{Error: &e}, SystemRAM{Error: &e}, SystemSwap{Error: &e}
	}
	s.collectDisk = func(path string) SystemDisk {
		e := "boom"
		return SystemDisk{Path: path, Error: &e}
	}

	for i := range 2 {
		code, body := s.GetJSON(context.Background())
		if code != http.StatusServiceUnavailable {
			t.Errorf("call %d code = %d, want 503 (body %s)", i+1, code, body)
		}
	}
}

// TestGetJSON_StaleHardFailPayloadStays503 covers the stale branch: a stale
// all-failed payload must not be upgraded to 200.
func TestGetJSON_StaleHardFailPayloadStays503(t *testing.T) {
	s := NewSystemService(appconfig.SystemConfig{Enabled: true, MetricsTTLSeconds: 60}, "test", context.Background())
	s.metricsMu.Lock()
	s.metricsPayload = []byte(`{"ok":false}`)
	s.metricsHardFail = true
	s.metricsAt = time.Now().Add(-time.Hour)
	s.hardFailUntil = time.Now().Add(time.Hour) // suppress the background refresh
	s.metricsMu.Unlock()

	if code, _ := s.GetJSON(context.Background()); code != http.StatusServiceUnavailable {
		t.Errorf("code = %d, want 503", code)
	}
}

// TestRefreshMetrics_DiskCollectorBoundedByColdPath proves a hung statfs (for
// example on a dead NFS mount) cannot block the refresh past the cold-path
// budget, and that a still-pending statfs is not stacked on the next refresh.
func TestRefreshMetrics_DiskCollectorBoundedByColdPath(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := appconfig.SystemConfig{Enabled: true, MetricsTTLSeconds: 60, VersionsTTLSeconds: 3600, ColdPathTimeoutMs: 500, DiskPath: "/mnt/nfs"}
		s := NewSystemService(cfg, "test", context.Background())
		s.fetchLatest = func(context.Context, int) string { return "" }
		s.binPath = "/nonexistent-openclaw-binary"
		s.binOnce.Do(func() {})
		// Pin versions so the synctest bubble never waits on a real subprocess.
		s.verCached = SystemVersions{Dashboard: "test"}
		s.verAt = time.Now()
		s.collectOpenclaw = func(context.Context, string) SystemOpenclaw { return SystemOpenclaw{} }
		s.collectCPURAMSwap = func(context.Context, int) (SystemCPU, SystemRAM, SystemSwap) {
			return SystemCPU{}, SystemRAM{}, SystemSwap{}
		}
		unblock := make(chan struct{})
		var diskCalls atomic.Int32
		s.collectDisk = func(path string) SystemDisk {
			diskCalls.Add(1)
			<-unblock
			return SystemDisk{Path: path}
		}

		start := time.Now()
		body, _ := s.refresh(context.Background())
		if elapsed := time.Since(start); elapsed != 500*time.Millisecond {
			t.Errorf("refresh took %v, want exactly the 500ms cold-path budget", elapsed)
		}
		if !strings.Contains(string(body), `"disk: statfs /mnt/nfs: `) {
			t.Errorf("body lacks disk timeout error: %s", body)
		}

		// The first statfs is still hung: the next refresh must not start another.
		_, _ = s.refresh(context.Background())
		if got := diskCalls.Load(); got != 1 {
			t.Errorf("disk collector calls = %d, want 1 while the first is pending", got)
		}
		close(unblock)
		synctest.Wait()
	})
}

func TestDiskUsage_MatchesDf(t *testing.T) {
	tests := []struct {
		name      string
		st        diskStats
		wantTotal int64
		wantUsed  int64
		wantPct   float64
	}{
		{
			// 1000 blocks, 200 free, 150 available to non-root (50 reserved).
			// df: used = 800 blocks, pct = 800 / (800+150) = 84.2%.
			name:      "reserved blocks are not counted as used",
			st:        diskStats{Blocks: 1000, Bfree: 200, Bavail: 150, BlockSize: 4096},
			wantTotal: 1000 * 4096,
			wantUsed:  800 * 4096,
			wantPct:   84.2,
		},
		{
			name:      "no reserve",
			st:        diskStats{Blocks: 400, Bfree: 100, Bavail: 100, BlockSize: 512},
			wantTotal: 400 * 512,
			wantUsed:  300 * 512,
			wantPct:   75,
		},
		{
			name: "empty filesystem",
			st:   diskStats{BlockSize: 4096},
		},
		{
			name:      "bfree larger than blocks does not underflow",
			st:        diskStats{Blocks: 10, Bfree: 20, Bavail: 20, BlockSize: 1024},
			wantTotal: 10 * 1024,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := diskUsage("/", tc.st)
			if d.TotalBytes != tc.wantTotal || d.UsedBytes != tc.wantUsed || d.Percent != tc.wantPct {
				t.Errorf("diskUsage = total %d used %d pct %v, want total %d used %d pct %v",
					d.TotalBytes, d.UsedBytes, d.Percent, tc.wantTotal, tc.wantUsed, tc.wantPct)
			}
			if d.Error != nil {
				t.Errorf("unexpected error %q", *d.Error)
			}
		})
	}
}

func TestParseGatewayStatusJSON_LoadedButStoppedIsOffline(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "loaded and stopped", input: `{"service":{"loaded":true,"runtime":{"status":"stopped"}}}`, want: "offline"},
		{name: "loaded without runtime status", input: `{"service":{"loaded":true}}`, want: "online"},
		{name: "running", input: `{"service":{"loaded":false,"runtime":{"status":"running"}}}`, want: "online"},
		{name: "not loaded", input: `{"service":{"loaded":false}}`, want: "offline"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseGatewayStatusJSON(context.Background(), tc.input).Status; got != tc.want {
				t.Errorf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecodeJSONObjectFromOutput_TrailingText(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{name: "trailing log line", output: "{\"k\":\"v\"}\n[INFO] done", want: "v"},
		{name: "preamble and trailing text", output: "starting {bad} {\"k\":\"v\"} trailer", want: "v"},
		{name: "two objects keeps the first", output: `{"k":"v"}{"k":"w"}`, want: "v"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			if err := decodeJSONObjectFromOutput(tc.output, &m); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if m["k"] != tc.want {
				t.Errorf("k = %v, want %q", m["k"], tc.want)
			}
		})
	}
}

// TestResolveOpenclawBin_NoHomeSkipsRelativeCandidates: with no resolvable
// home dir, asdf candidates must not be probed relative to the working dir.
func TestResolveOpenclawBin_NoHomeSkipsRelativeCandidates(t *testing.T) {
	cwd := t.TempDir()
	shim := filepath.Join(cwd, ".asdf", "shims", "openclaw")
	if err := os.MkdirAll(filepath.Dir(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shim, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	t.Setenv("HOME", "")
	t.Setenv("PATH", "")

	if got := ResolveOpenclawBin(); strings.Contains(got, ".asdf") {
		t.Errorf("ResolveOpenclawBin() = %q, must not resolve a home candidate without a home dir", got)
	}
}

func TestGetProcessInfo_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	uptime, memory := GetProcessInfo(ctx, os.Getpid())
	if uptime != "" || memory != "" {
		t.Errorf("GetProcessInfo with cancelled ctx = (%q, %q), want empty", uptime, memory)
	}
}

// TestGetJSON_ColdLeaderIgnoresRequestCancellation: the leader collects on
// behalf of every cold caller and caches the result, so a client that aborts
// its request must not cancel (and poison the cache with) the collection.
func TestGetJSON_ColdLeaderIgnoresRequestCancellation(t *testing.T) {
	s := NewSystemService(appconfig.SystemConfig{Enabled: true, MetricsTTLSeconds: 60}, "test", context.Background())
	var sawCancelled atomic.Bool
	s.refresh = func(ctx context.Context) ([]byte, bool) {
		sawCancelled.Store(ctx.Err() != nil)
		return []byte(`{"ok":true}`), false
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.GetJSON(ctx)
	if sawCancelled.Load() {
		t.Fatal("cold collection ran on the cancelled request context")
	}
}
