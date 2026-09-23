package appsystem

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fakeCLIDispatcher is the one executable file behind every fake CLI in this
// test binary. macOS assesses each newly written executable on its first exec
// (~0.1–0.4 s, longer under load), which dominated the fake-CLI tests and
// pushed short probe deadlines into flaky timeouts. Each fake CLI is instead a
// symlink to this pre-warmed dispatcher, which sources the per-CLI script body
// from "<symlink path>.sh"; writing that body is a plain file write.
var fakeCLIDispatcher string

// setupFakeCLIDispatcher writes and pre-warms the dispatcher. TestMain calls it
// and runs cleanup after the tests finish.
func setupFakeCLIDispatcher() (cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "appsystem-fakecli-")
	if err != nil {
		return nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "dispatch")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n. \"$0.sh\"\n"), 0o755); err != nil {
		cleanup()
		return nil, err
	}
	if err := os.WriteFile(path+".sh", []byte("exit 0\n"), 0o644); err != nil {
		cleanup()
		return nil, err
	}
	// Pay the first-exec assessment here, outside every timed test.
	if out, err := exec.Command(path).CombinedOutput(); err != nil {
		cleanup()
		return nil, fmt.Errorf("warm fake CLI dispatcher: %w: %s", err, out)
	}
	fakeCLIDispatcher = path
	return cleanup, nil
}

// writeFakeCLI creates an executable at dir/name that runs body as a POSIX sh
// script with the caller's argv. Use "exec sleep N" rather than "sleep N" for a
// hanging CLI so a deadline kill reaches the sleeping process itself.
func writeFakeCLI(tb testing.TB, dir, name, body string) string {
	tb.Helper()
	if fakeCLIDispatcher == "" {
		tb.Fatal("fake CLI dispatcher not initialised; TestMain must call setupFakeCLIDispatcher")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path+".sh", []byte(body), 0o644); err != nil {
		tb.Fatalf("write fake CLI body: %v", err)
	}
	if err := os.Symlink(fakeCLIDispatcher, path); err != nil {
		tb.Fatalf("link fake CLI: %v", err)
	}
	return path
}
