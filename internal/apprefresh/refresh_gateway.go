package apprefresh

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// collectGatewayHealth probes the openclaw-gateway process via pgrep + ps and
// returns a status/pid/uptime/memory map for the dashboard. Best-effort: all
// failures collapse to status=offline.
func collectGatewayHealth(ctx context.Context) map[string]any {
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
	pgrepCtx, pgrepCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pgrepCancel()

	out, err := exec.CommandContext(pgrepCtx, "pgrep", "-f", "openclaw-gateway").Output()
	if err != nil {
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
		return gw
	}

	pidInt, err := strconv.Atoi(pid)
	if err != nil {
		return gw
	}
	gw["pid"] = pidInt
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
