package appservice

import (
	"context"
	"os/exec"
	"time"
)

// execRun runs name with args using a 10 s timeout, returns combined output.
func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok {
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
	}
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
