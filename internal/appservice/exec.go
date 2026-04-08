package appservice

import (
	"context"
	"os/exec"
	"time"
)

// execRun runs name with args using a 10 s timeout, returns combined output.
func execRun(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
