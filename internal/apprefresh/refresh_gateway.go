package apprefresh

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// pgrepGateway shells out to pgrep and returns the matching PID list. Stubbed
// in tests via package-level reassignment.
//
// Pattern targets the npm-installed gateway entry script and subcommand. The
// previous pattern "openclaw-gateway" relied on Node's process.title rewrite,
// which updates /proc/<pid>/cmdline on Linux but is internal-only on macOS,
// so pgrep -f never matched the gateway on macOS and the dashboard reported
// the gateway as permanently offline.
var pgrepGateway = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "pgrep", "-f", "openclaw/dist/index.js gateway").Output()
}

// healthzProbe issues a GET to the gateway's /healthz endpoint and returns
// true if the response status is 2xx. Stubbed in tests.
var healthzProbe = func(ctx context.Context, port int) bool {
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// collectGatewayHealth probes the openclaw gateway and returns a
// status/pid/uptime/memory map for the dashboard.
//
// Source of truth for "status" is the gateway's HTTP /healthz endpoint when a
// port is configured: a 2xx response means status=online regardless of what
// pgrep can or cannot see. This consolidates the dashboard's two previous
// probe paths (pgrep here, HTTP in appsystem) onto a single authoritative
// signal and removes the macOS pgrep failure mode for the banner.
//
// pgrep + ps remain as the source for pid/uptime/rss metadata; if pgrep
// fails to find the gateway PID (e.g. pattern drift), the dashboard reports
// online without metadata rather than masking the real liveness signal.
//
// Best-effort: all failures collapse to status=offline.
func collectGatewayHealth(ctx context.Context, gatewayPort int) map[string]any {
	gw := map[string]any{
		"status": "offline",
		"pid":    nil,
		"uptime": "",
		"memory": "",
		"rss":    0,
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Authoritative liveness signal: gateway HTTP /healthz. Only consulted
	// when a port is configured; port 0 means "skip HTTP, rely on pgrep".
	httpOnline := false
	if gatewayPort > 0 {
		probeCtx, probeCancel := context.WithTimeout(ctx, 2*time.Second)
		httpOnline = healthzProbe(probeCtx, gatewayPort)
		probeCancel()
	}

	pgrepCtx, pgrepCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pgrepCancel()

	out, err := pgrepGateway(pgrepCtx)
	if err != nil {
		// pgrep failed; status determined solely by HTTP probe.
		if httpOnline {
			gw["status"] = "online"
		}
		return gw
	}
	pids := strings.Fields(strings.TrimSpace(string(out)))
	myPid := strconv.Itoa(os.Getpid())
	var pid string
	for _, p := range pids {
		if p != "" && p != myPid {
			pid = p
			break
		}
	}
	if pid == "" {
		// No gateway PID located by pgrep; HTTP probe still owns liveness.
		if httpOnline {
			gw["status"] = "online"
		}
		return gw
	}

	pidInt, err := strconv.Atoi(pid)
	if err != nil {
		if httpOnline {
			gw["status"] = "online"
		}
		return gw
	}
	gw["pid"] = pidInt
	// pgrep located the gateway. status=online unconditionally — even if the
	// HTTP probe was skipped (port==0) or transiently failed, an alive
	// process is sufficient evidence of liveness.
	gw["status"] = "online"

	psCtx, psCancel := context.WithTimeout(ctx, 5*time.Second)
	defer psCancel()
	psOut, err := exec.CommandContext(psCtx, "ps", "-p", pid, "-o", "etime=,rss=").Output()
	if err != nil {
		return gw
	}
	parts := strings.Fields(strings.TrimSpace(string(psOut)))
	if len(parts) >= 2 {
		gw["uptime"] = strings.TrimSpace(parts[0])
		rssKB, _ := strconv.Atoi(parts[1])
		gw["rss"] = rssKB
		switch {
		case rssKB > 1048576:
			gw["memory"] = fmt.Sprintf("%.1f GB", float64(rssKB)/1048576)
		case rssKB > 1024:
			gw["memory"] = fmt.Sprintf("%.0f MB", float64(rssKB)/1024)
		default:
			gw["memory"] = fmt.Sprintf("%d KB", rssKB)
		}
	}
	return gw
}
