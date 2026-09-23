package appsystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// ErrCommandTimeout is returned when runWithTimeout's context deadline fired.
var ErrCommandTimeout = errors.New("command timeout")

// ErrCommandNotFound is returned when the binary itself could not be located.
var ErrCommandNotFound = errors.New("command not found")

// FormatBytes formats bytes into a human-readable string (KB/MB/GB).
func FormatBytes(b int64) string {
	if b < 0 {
		return "0B"
	}
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1fGB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.0fKB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// GetProcessInfo returns uptime and memory usage for a PID using ps.
// Uses a 3-second context timeout to avoid hanging on unresponsive ps.
func GetProcessInfo(ctx context.Context, pid int) (uptime string, memory string) {
	// C9b: reject non-positive PIDs early — ps would either fail or, worse on
	// some kernels, treat 0 as "all processes" and return ambiguous output.
	if pid <= 0 {
		return "", ""
	}
	tctx, cancel := context.WithTimeout(ctx, processInfoTimeout)
	defer cancel()
	// Get elapsed time and RSS via ps
	out, err := exec.CommandContext(tctx, "ps", "-o", "etime=,rss=", "-p", fmt.Sprintf("%d", pid)).Output()
	if err != nil {
		return "", ""
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) >= 1 {
		uptime = strings.TrimSpace(fields[0])
	}
	if len(fields) >= 2 {
		if rssKB, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			memory = FormatBytes(rssKB * 1024)
		}
	}
	return
}

// runWithTimeout runs an external command with a context deadline.
// On failure, stderr is appended to the error message for better diagnostics.
func runWithTimeout(ctx context.Context, timeoutMs int, name string, args ...string) (string, error) {
	if timeoutMs <= 0 {
		timeoutMs = defaultCommandTimeoutMs
	}
	tctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(tctx, name, args...)
	if env := OpenclawCLIEnv(name); env != nil {
		cmd.Env = env
	}
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(tctx.Err(), context.DeadlineExceeded) {
			return strings.TrimSpace(string(out)), fmt.Errorf("%w: %s", ErrCommandTimeout, name)
		}
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return strings.TrimSpace(string(out)), fmt.Errorf("%w: %s", ErrCommandNotFound, name)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return strings.TrimSpace(string(out)), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return strings.TrimSpace(string(out)), err
	}
	return strings.TrimSpace(string(out)), nil
}

func runOpenclawWithTimeout(ctx context.Context, timeoutMs int, name string, args ...string) (string, error) {
	if timeoutMs <= 0 {
		timeoutMs = defaultCommandTimeoutMs
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	out, err := appopenclaw.Output(appopenclaw.CommandContext(ctx, name, args...), appopenclaw.MaxOutputBytes)
	if err := ctx.Err(); err != nil {
		// Wrap the context error too: callers classify the failure with
		// errors.Is, and "timeout" must not degrade to a generic "unavailable".
		return string(out), fmt.Errorf("%w: %w", ErrCommandTimeout, err)
	}
	return strings.TrimSpace(string(out)), err
}
