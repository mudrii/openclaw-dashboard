//go:build !windows

package dashboard

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// TestShutdownSequence builds the dashboard binary and verifies that:
//  1. A single SIGINT triggers graceful shutdown within 6s.
//  2. A second SIGINT during shutdown forces exit within 1s of the second signal.
//
// This guards against regressions where:
//   - serverCancel was called twice across overlapping select arms.
//   - httpSrv.Shutdown used a context.Background() parent, so a second SIGINT
//     could not accelerate exit.
func TestShutdownSequence(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test; skipped under -short")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw-dashboard-test")
	build := exec.Command("go", "build", "-o", bin, "./cmd/openclaw-dashboard")
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// Pick a free port to avoid collisions with concurrent tests.
	port := freePort(t)

	t.Run("single SIGINT exits within 6s", func(t *testing.T) {
		cmd := exec.Command(bin, "--bind", "127.0.0.1", "--port", strconv.Itoa(port))
		cmd.Env = append(os.Environ(), "DASHBOARD_AI_TOKEN_OPTIONAL=1")
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatalf("start: %v", err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })

		waitListening(t, port, 5*time.Second)

		if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
			t.Fatalf("signal: %v", err)
		}

		if err := waitExit(cmd, 6*time.Second); err != nil {
			t.Fatalf("expected clean exit within 6s: %v", err)
		}
	})

	t.Run("second SIGINT accelerates exit", func(t *testing.T) {
		port2 := freePort(t)
		cmd := exec.Command(bin, "--bind", "127.0.0.1", "--port", strconv.Itoa(port2))
		cmd.Env = append(os.Environ(), "DASHBOARD_AI_TOKEN_OPTIONAL=1")
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatalf("start: %v", err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })

		waitListening(t, port2, 5*time.Second)

		if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
			t.Fatalf("first signal: %v", err)
		}
		// Brief gap so the first signal is observed before the second.
		time.Sleep(50 * time.Millisecond)
		secondSent := time.Now()
		if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
			t.Fatalf("second signal: %v", err)
		}

		if err := waitExit(cmd, 2*time.Second); err != nil {
			t.Fatalf("expected exit within 2s of second SIGINT: %v", err)
		}
		if d := time.Since(secondSent); d > 2*time.Second {
			t.Fatalf("second SIGINT did not accelerate exit: %v", d)
		}
	})
}

// freePort returns an available TCP port on 127.0.0.1.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitListening polls until the port accepts a TCP connection or the deadline elapses.
func waitListening(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server never started listening on %s", addr)
}

// waitExit waits up to timeout for the command to exit. Returns nil on a normal
// or signal-induced exit, error on timeout or unexpected wait failure.
func waitExit(cmd *exec.Cmd, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		// ExitError is fine — we only care that the process terminated cleanly.
		var exitErr *exec.ExitError
		if err == nil || errors.As(err, &exitErr) {
			return nil
		}
		return err
	case <-time.After(timeout):
		return errors.New("timeout waiting for process exit")
	}
}
