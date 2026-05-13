//go:build !windows

package main

import (
	"flag"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// In-process tests for the cmd entrypoint. main() itself is untestable (it calls
// os.Exit), so the cmd package exposes a tiny run() wrapper that returns the exit
// code; these tests drive run() and capture stdio. They complement the subprocess
// smoke tests in cmd_smoke_test.go, which exercise the full built binary.
//
// flag.CommandLine is global; tests that traverse the flag.Parse path must reset
// it between calls to avoid "flag redefined" panics. Tests that exit before
// flag.Parse (unknown subcommand, bare `service`) do not need the reset.
//
// The "invalid flag" case is intentionally NOT covered in-process: the default
// flag.CommandLine uses flag.ExitOnError, which calls os.Exit(2) and would kill
// the test process. That case is exercised by TestCmdInvalidFlag in
// cmd_smoke_test.go via a subprocess.

// captureStdio swaps os.Stdout and os.Stderr for pipes, invokes fn, then
// restores them and returns the captured streams.
func captureStdio(t *testing.T, fn func() int) (string, string, int) {
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

// resetFlagCommandLine restores a fresh flag.CommandLine so a subsequent
// run() call can re-register its flags without panicking.
func resetFlagCommandLine(t *testing.T) {
	t.Helper()
	saved := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	t.Cleanup(func() { flag.CommandLine = saved })
}

// setArgs swaps os.Args and restores on cleanup.
func setArgs(t *testing.T, args []string) {
	t.Helper()
	saved := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = saved })
}

func TestInProcessVersionFlag(t *testing.T) {
	setArgs(t, []string{"openclaw-dashboard", "--version"})
	resetFlagCommandLine(t)
	// AI gating is downstream of --version, but be defensive.
	t.Setenv("DASHBOARD_AI_TOKEN_OPTIONAL", "1")

	out, errOut, code := captureStdio(t, run)
	if code != 0 {
		t.Fatalf("--version exit code = %d, want 0; stdout=%q stderr=%q", code, out, errOut)
	}
	if !strings.Contains(out, "openclaw-dashboard") {
		t.Fatalf("--version stdout missing program name; got %q", out)
	}
}

func TestInProcessShortVersionFlag(t *testing.T) {
	setArgs(t, []string{"openclaw-dashboard", "-V"})
	resetFlagCommandLine(t)
	t.Setenv("DASHBOARD_AI_TOKEN_OPTIONAL", "1")

	out, errOut, code := captureStdio(t, run)
	if code != 0 {
		t.Fatalf("-V exit code = %d, want 0; stdout=%q stderr=%q", code, out, errOut)
	}
	if !strings.Contains(out, "openclaw-dashboard") {
		t.Fatalf("-V stdout missing program name; got %q", out)
	}
}

func TestInProcessUnknownSubcommand(t *testing.T) {
	setArgs(t, []string{"openclaw-dashboard", "bogus-subcommand"})
	// No flag reset needed: unknown subcommand returns before flag.Parse.

	out, errOut, code := captureStdio(t, run)
	if code == 0 {
		t.Fatalf("unknown subcommand should exit non-zero; stdout=%q stderr=%q", out, errOut)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Fatalf("expected 'unknown command' in stderr; got %q", errOut)
	}
	if !strings.Contains(errOut, "Usage:") {
		t.Fatalf("expected 'Usage:' in stderr; got %q", errOut)
	}
}

func TestInProcessBareServicePrintsUsage(t *testing.T) {
	setArgs(t, []string{"openclaw-dashboard", "service"})
	// No flag reset: bare `service` returns before flag.Parse.

	out, errOut, code := captureStdio(t, run)
	if code == 0 {
		t.Fatalf("bare 'service' should exit non-zero; stdout=%q stderr=%q", out, errOut)
	}
	if !strings.Contains(errOut, "Usage:") || !strings.Contains(errOut, "install") {
		t.Fatalf("expected service usage in stderr; got %q", errOut)
	}
}
