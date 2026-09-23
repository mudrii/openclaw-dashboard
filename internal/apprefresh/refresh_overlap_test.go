package apprefresh

import (
	"context"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestCollectDashboardDataOverlapsLegacyCLIReads pins that the legacy live
// session model, channel status, subagent task, and /readyz reads run
// concurrently: each stub blocks until all four have started, so a
// sequential collector would time out instead.
func TestCollectDashboardDataOverlapsLegacyCLIReads(t *testing.T) {
	stubCollectorSeams(t)
	root := t.TempDir()
	openclawPath := filepath.Join(root, "openclaw")
	benchWriteJSON(t, filepath.Join(root, "openclaw.json"), map[string]any{})

	const reads = 4
	var started sync.WaitGroup
	started.Add(reads)
	allStarted := make(chan struct{})
	go func() { started.Wait(); close(allStarted) }()
	var mu sync.Mutex
	var timedOut []string
	rendezvous := func(name string) {
		started.Done()
		select {
		case <-allStarted:
		case <-time.After(5 * time.Second):
			mu.Lock()
			timedOut = append(timedOut, name)
			mu.Unlock()
		}
	}

	stubExec := execCommandContext
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if len(args) > 0 && args[0] == "tasks" {
			rendezvous("tasks")
		}
		return stubExec(ctx, name, args...)
	}
	fetchLiveSessionModels = func(context.Context) map[string]string {
		rendezvous("sessions")
		return map[string]string{}
	}
	channelStatusCollector = func(context.Context, func(context.Context, string, ...string) *exec.Cmd, func() string) (map[string]any, bool) {
		rendezvous("channels")
		return nil, false
	}
	readyzProbe = func(context.Context, int) ([]string, bool) {
		rendezvous("readyz")
		return nil, false
	}

	cfg := benchCollectorConfig()
	_ = collectDashboardData(t.Context(), filepath.Join(root, "dashboard"), openclawPath, cfg)
	if len(timedOut) > 0 {
		t.Fatalf("reads did not overlap; timed out waiting in %v", timedOut)
	}
}
