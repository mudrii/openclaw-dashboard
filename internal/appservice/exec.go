package appservice

import (
	"context"
	"os"
	"os/exec"
	"time"
)

// execRun runs name with args using a 10 s timeout, returns combined output.
// LC_ALL=C is forced so external tools (ps, systemctl, launchctl) emit
// English/C-locale dates and field labels, keeping timestamp parsing
// (e.g. ps lstart, ActiveEnterTimestamp) deterministic regardless of the
// host's locale.
func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok {
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
	}
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd.CombinedOutput()
}
