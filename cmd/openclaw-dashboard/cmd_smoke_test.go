//go:build !windows

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Smoke tests for the cmd/openclaw-dashboard entrypoint. The cmd package itself
// is a 3-line shim around dashboard.Main(); these tests build the actual binary
// and exercise CLI surface paths not covered by the root-package subprocess test
// (main_shutdown_test.go), which focuses on SIGINT shutdown semantics.
//
// Each subtest builds a single binary in a per-test TempDir to keep them
// hermetic and avoid races with other test binaries.

var (
	binOnce sync.Once
	binPath string
	binErr  error
)

// buildBinary builds the cmd binary exactly once per test run and returns the
// resulting path. Subsequent calls reuse it. The binary is written under
// os.TempDir() since t.TempDir() would scope it to a single test.
func buildBinary(t *testing.T) string {
	t.Helper()
	binOnce.Do(func() {
		dir, err := os.MkdirTemp("", "openclaw-dashboard-cmd-smoke-")
		if err != nil {
			binErr = err
			return
		}
		path := filepath.Join(dir, "openclaw-dashboard")
		// Build from the cmd directory's perspective: go test runs with
		// CWD = the package being tested. The cmd package lives at
		// ./cmd/openclaw-dashboard relative to repo root; from inside that
		// dir, "." is the right import path.
		cmd := exec.Command("go", "build", "-o", path, ".")
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			binErr = err
			return
		}
		binPath = path
	})
	if binErr != nil {
		t.Fatalf("build binary: %v", binErr)
	}
	return binPath
}

// runBin executes the dashboard binary with args and a timeout, returning the
// combined output and exit code. Inherits a clean environment with
// DASHBOARD_AI_TOKEN_OPTIONAL=1 so AI-config gating cannot cause spurious
// non-zero exits in serve-mode tests.
func runBin(t *testing.T, args []string, timeout time.Duration) (string, int) {
	t.Helper()
	bin := buildBinary(t)
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "DASHBOARD_AI_TOKEN_OPTIONAL=1")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("wait: %v", err)
		}
		return buf.String(), code
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("binary did not exit within %v; output:\n%s", timeout, buf.String())
		return "", -1
	}
}

func TestCmdVersionFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test; skipped under -short")
	}
	out, code := runBin(t, []string{"--version"}, 10*time.Second)
	if code != 0 {
		t.Fatalf("--version exit code = %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "openclaw-dashboard") {
		t.Fatalf("--version output missing program name; got:\n%s", out)
	}
}

func TestCmdShortVersionFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test; skipped under -short")
	}
	out, code := runBin(t, []string{"-V"}, 10*time.Second)
	if code != 0 {
		t.Fatalf("-V exit code = %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "openclaw-dashboard") {
		t.Fatalf("-V output missing program name; got:\n%s", out)
	}
}

func TestCmdUnknownSubcommand(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test; skipped under -short")
	}
	out, code := runBin(t, []string{"bogus-subcommand"}, 10*time.Second)
	if code == 0 {
		t.Fatalf("unknown subcommand should exit non-zero; output:\n%s", out)
	}
	if !strings.Contains(out, "unknown command") {
		t.Fatalf("expected 'unknown command' in output; got:\n%s", out)
	}
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("expected usage line in output; got:\n%s", out)
	}
}

func TestCmdBareServicePrintsUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test; skipped under -short")
	}
	out, code := runBin(t, []string{"service"}, 10*time.Second)
	if code == 0 {
		t.Fatalf("bare 'service' should exit non-zero; output:\n%s", out)
	}
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "install") {
		t.Fatalf("expected service usage in output; got:\n%s", out)
	}
}

// TestCmdServiceStatusNoPanic verifies that `service status` exits cleanly
// (either 0 if installed or non-zero if not) without panicking. The status
// path is admin-free, so it can run in any test environment.
func TestCmdServiceStatusNoPanic(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test; skipped under -short")
	}
	out, _ := runBin(t, []string{"service", "status"}, 10*time.Second)
	// Either backend-not-available, or status-ok, or status-fail. None should
	// panic. We assert absence of the Go runtime panic banner.
	if strings.Contains(out, "panic:") || strings.Contains(out, "goroutine ") && strings.Contains(out, "runtime.gopanic") {
		t.Fatalf("service status panicked; output:\n%s", out)
	}
}

// TestCmdInvalidFlag verifies that an unknown CLI flag exits non-zero with a
// flag-usage message rather than starting the server.
func TestCmdInvalidFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test; skipped under -short")
	}
	out, code := runBin(t, []string{"--definitely-not-a-flag"}, 10*time.Second)
	if code == 0 {
		t.Fatalf("invalid flag should exit non-zero; output:\n%s", out)
	}
	if !strings.Contains(out, "flag provided but not defined") && !strings.Contains(out, "Usage") {
		t.Fatalf("expected flag error in output; got:\n%s", out)
	}
}
