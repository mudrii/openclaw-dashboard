package dashboard

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestMainRefreshCLI(t *testing.T) {
	t.Run("missing OpenClaw exits non-zero", func(t *testing.T) {
		setMainArgs(t, []string{"openclaw-dashboard", "--refresh"})
		resetMainFlags(t)
		t.Setenv("OPENCLAW_HOME", filepath.Join(t.TempDir(), "missing-openclaw"))

		_, stderr, code := captureMainStdio(t, Main)
		if code == 0 {
			t.Fatalf("--refresh with missing OpenClaw exited 0; stderr=%q", stderr)
		}
		if !strings.Contains(stderr, "OpenClaw not found") {
			t.Fatalf("stderr = %q, want OpenClaw not found", stderr)
		}
	})

	t.Run("collector failure exits non-zero", func(t *testing.T) {
		setMainArgs(t, []string{"openclaw-dashboard", "--refresh"})
		resetMainFlags(t)
		openclawHome := t.TempDir()
		t.Setenv("OPENCLAW_HOME", openclawHome)
		prev := refreshCollectorFunc
		t.Cleanup(func() { refreshCollectorFunc = prev })
		refreshCollectorFunc = func(ctx context.Context, dashboardDir, openclawPath string, cfg Config) error {
			if openclawPath != openclawHome {
				t.Fatalf("openclawPath = %q, want %q", openclawPath, openclawHome)
			}
			return errors.New("collector failed")
		}

		_, stderr, code := captureMainStdio(t, Main)
		if code == 0 {
			t.Fatalf("--refresh collector failure exited 0; stderr=%q", stderr)
		}
		if !strings.Contains(stderr, "refresh failed") || !strings.Contains(stderr, "collector failed") {
			t.Fatalf("stderr = %q, want refresh failure", stderr)
		}
	})

	t.Run("success exits zero", func(t *testing.T) {
		setMainArgs(t, []string{"openclaw-dashboard", "--refresh"})
		resetMainFlags(t)
		openclawHome := t.TempDir()
		t.Setenv("OPENCLAW_HOME", openclawHome)
		called := false
		prev := refreshCollectorFunc
		t.Cleanup(func() { refreshCollectorFunc = prev })
		refreshCollectorFunc = func(ctx context.Context, dashboardDir, openclawPath string, cfg Config) error {
			called = true
			if openclawPath != openclawHome {
				t.Fatalf("openclawPath = %q, want %q", openclawPath, openclawHome)
			}
			return nil
		}

		stdout, stderr, code := captureMainStdio(t, Main)
		if code != 0 {
			t.Fatalf("--refresh success exit code = %d; stdout=%q stderr=%q", code, stdout, stderr)
		}
		if !called {
			t.Fatal("refreshCollectorFunc was not called")
		}
		if !strings.Contains(stdout, "data.json refreshed") {
			t.Fatalf("stdout = %q, want refreshed message", stdout)
		}
	})
}

func TestMainInvalidPortExitsBeforeListen(t *testing.T) {
	setMainArgs(t, []string{"openclaw-dashboard", "--bind", "127.0.0.1", "--port", "0"})
	resetMainFlags(t)
	t.Setenv("DASHBOARD_AI_TOKEN_OPTIONAL", "1")

	_, stderr, code := captureMainStdio(t, Main)
	if code == 0 {
		t.Fatalf("invalid port exited 0; stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "invalid port 0") {
		t.Fatalf("stderr = %q, want invalid port", stderr)
	}
}

func resetMainFlags(t *testing.T) {
	t.Helper()
	saved := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	t.Cleanup(func() { flag.CommandLine = saved })
}

func setMainArgs(t *testing.T, args []string) {
	t.Helper()
	saved := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = saved })
}

func captureMainStdio(t *testing.T, fn func() int) (string, string, int) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	t.Cleanup(func() {
		os.Stdout = origOut
		os.Stderr = origErr
	})

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stdout = outW
	os.Stderr = errW

	var outBuf, errBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&outBuf, outR) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&errBuf, errR) }()

	code := fn()

	_ = outW.Close()
	_ = errW.Close()
	wg.Wait()
	_ = outR.Close()
	_ = errR.Close()

	return outBuf.String(), errBuf.String(), code
}
