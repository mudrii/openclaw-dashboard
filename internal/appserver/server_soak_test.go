package appserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

const (
	// soakEnv opts in to the load soak; it never runs in CI by default.
	soakEnv = "OPENCLAW_SOAK"
	// soakDuration is how long concurrent clients hammer the server.
	soakDuration = 20 * time.Second
	// soakClients is the number of concurrent request loops.
	soakClients = 16
	// soakGoroutineSlack is how many goroutines above the pre-start baseline
	// may remain once the server and client have shut down.
	soakGoroutineSlack = 5
)

// soakGETPaths is every read route, including query variants of the log feeds.
var soakGETPaths = []string{
	"/", "/index.html", "/api/system", "/api/refresh", "/api/logs",
	"/api/logs?source=runtime&limit=50", "/api/errors", "/api/automation/runs",
	"/api/session/context", "/api/workboard", "/api/chat/status",
	"/api/operations/status", "/themes.json", "/favicon.ico", "/missing",
}

// TestServerSoak fires concurrent GETs at every read endpoint through a real
// HTTP listener for soakDuration, then shuts down and checks that no request
// failed at the transport level, no handler returned 5xx other than the
// degraded 502/503 answers of gateway-backed routes, and the goroutine count
// returns to its baseline. Run with:
//
//	OPENCLAW_SOAK=1 CGO_ENABLED=1 go test -race -run TestServerSoak ./internal/appserver/
func TestServerSoak(t *testing.T) {
	if testing.Short() || os.Getenv(soakEnv) != "1" {
		t.Skip("set " + soakEnv + "=1 (and omit -short) to run the load soak")
	}
	// Hermetic runtime: no openclaw binary or state on PATH/HOME, so runtime
	// endpoints exercise their degraded paths instead of a live gateway.
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	t.Setenv("HOME", empty)

	runtime.GC()
	baseline := runtime.NumGoroutine()

	dir := t.TempDir()
	writeMinimalData(t, dir)
	var refreshes atomic.Int64
	refresh := func(ctx context.Context, dir, _ string, _ appconfig.Config) error {
		refreshes.Add(1)
		// Write-then-rename like the real collector, so readers never see a
		// truncated data.json.
		tmp := filepath.Join(dir, "data.json.tmp")
		if err := os.WriteFile(tmp, []byte(`{"version":"soak","sessions":[]}`), 0o600); err != nil {
			return err
		}
		return os.Rename(tmp, filepath.Join(dir, "data.json"))
	}
	cfg := appconfig.Default()
	cfg.Refresh.IntervalSeconds = 1
	cfg.System.MetricsTTLSeconds = 2

	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(dir, "soak", cfg, "", []byte("<head><body>__VERSION__</body>"), ctx, refresh)
	ts := httptest.NewServer(srv)
	client := ts.Client()
	client.Timeout = 30 * time.Second
	// Keep one idle connection per client so the soak reuses connections
	// instead of exhausting ephemeral ports with TIME_WAIT sockets.
	client.Transport.(*http.Transport).MaxIdleConnsPerHost = soakClients

	var (
		requests  atomic.Int64
		mu        sync.Mutex
		failures  []string
		wg        sync.WaitGroup
		stopAfter = time.Now().Add(soakDuration)
	)
	fail := func(msg string) {
		mu.Lock()
		defer mu.Unlock()
		if len(failures) < 20 {
			failures = append(failures, msg)
		}
	}
	for i := range soakClients {
		wg.Go(func() {
			for n := i; time.Now().Before(stopAfter); n++ {
				path := soakGETPaths[n%len(soakGETPaths)]
				method := http.MethodGet
				if n%7 == 0 {
					method = http.MethodHead
				}
				req, err := http.NewRequestWithContext(ctx, method, ts.URL+path, nil)
				if err != nil {
					fail(err.Error())
					return
				}
				resp, err := client.Do(req)
				if err != nil {
					fail(method + " " + path + ": " + err.Error())
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				requests.Add(1)
				// Without an OpenClaw runtime the gateway-backed routes answer
				// 502/503 by design; a 500 is always a server bug.
				degraded := resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable
				if resp.StatusCode >= 500 && !degraded {
					fail(method + " " + path + ": status " + resp.Status)
				}
			}
		})
	}
	wg.Wait()

	cancel()
	if err := srv.WaitRefresh(context.Background()); err != nil {
		t.Errorf("WaitRefresh: %v", err)
	}
	ts.Close()
	client.CloseIdleConnections()

	t.Logf("soak: %d requests, %d refreshes in %v", requests.Load(), refreshes.Load(), soakDuration)
	for _, f := range failures {
		t.Error(f)
	}

	// Background work (metric probes, the rate-limit ticker) winds down
	// asynchronously after cancel; allow a bounded settle period.
	deadline := time.Now().Add(15 * time.Second)
	for {
		runtime.GC()
		n := runtime.NumGoroutine()
		if n <= baseline+soakGoroutineSlack {
			break
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("goroutines %d after shutdown, baseline %d\n%s", n, baseline, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(100 * time.Millisecond)
	}
}
