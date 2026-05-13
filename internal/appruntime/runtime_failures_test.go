package appruntime

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// listTmpSiblings returns any leftover ".tmp." files beside dst — these
// indicate a failed atomic write that didn't clean up after itself.
func listTmpSiblings(t *testing.T, dir, base string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var leaks []string
	prefix := base + ".tmp."
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			leaks = append(leaks, e.Name())
		}
	}
	return leaks
}

// TestCopyFile_DstParentReadOnly verifies CopyFile surfaces an error when the
// destination directory is not writable and does not leak temp-sibling files.
func TestCopyFile_DstParentReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory perms")
	}

	root := t.TempDir()
	src := filepath.Join(root, "src.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	ro := filepath.Join(root, "readonly")
	if err := os.MkdirAll(ro, 0o755); err != nil {
		t.Fatal(err)
	}
	// Restrict to r-x. MkdirAll inside CopyFile will succeed (dir already
	// exists), but opening the temp sibling must fail.
	if err := os.Chmod(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })

	dst := filepath.Join(ro, "dst.txt")
	err := CopyFile(src, dst, 0o644)
	if err == nil {
		t.Fatal("expected error when dst parent is read-only")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "permission") &&
		!strings.Contains(strings.ToLower(err.Error()), "denied") &&
		!strings.Contains(err.Error(), "open") {
		t.Logf("error text (informational): %v", err)
	}

	// Restore perms so we can inspect for leaks.
	if err := os.Chmod(ro, 0o755); err != nil {
		t.Fatal(err)
	}
	if leaks := listTmpSiblings(t, ro, "dst.txt"); len(leaks) != 0 {
		t.Fatalf("temp siblings leaked: %v", leaks)
	}
	if _, err := os.Stat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("dst should not exist after failed copy, stat err=%v", err)
	}
}

// TestCopyFile_SrcUnreadable verifies CopyFile reports an error when src
// cannot be opened and leaves no partial dst behind.
func TestCopyFile_SrcUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file perms")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(src, 0o644) })

	dst := filepath.Join(dir, "dst.txt")
	if err := CopyFile(src, dst, 0o644); err == nil {
		t.Fatal("expected error reading unreadable src")
	}

	if _, err := os.Stat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("dst should not exist after failed copy, stat err=%v", err)
	}
	if leaks := listTmpSiblings(t, dir, "dst.txt"); len(leaks) != 0 {
		t.Fatalf("temp siblings leaked: %v", leaks)
	}
}

// TestCopyFile_TruncatesAndReplaces verifies a smaller payload fully replaces
// a larger pre-existing dst — no trailing bytes from the old contents survive.
func TestCopyFile_TruncatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	if err := os.WriteFile(dst, []byte(strings.Repeat("X", 4096)), 0o644); err != nil {
		t.Fatal(err)
	}
	newContent := []byte("short")
	if err := os.WriteFile(src, newContent, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dst, 0o644); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(newContent) {
		t.Fatalf("dst not fully replaced: got %q (len %d), want %q", got, len(got), newContent)
	}
	if leaks := listTmpSiblings(t, dir, "dst.txt"); len(leaks) != 0 {
		t.Fatalf("temp siblings leaked: %v", leaks)
	}
}

// TestCopyIfMissing_ExistingDstUnchanged confirms an existing dst is left
// untouched and no error is returned.
func TestCopyIfMissing_ExistingDstUnchanged(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("NEW"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyIfMissing(src, dst, 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "OLD" {
		t.Fatalf("dst overwritten: got %q, want %q", got, "OLD")
	}
}

// TestCopyIfMissing_SrcMissing verifies the error path when src does not exist:
// the error wraps fs.ErrNotExist, dst is removed, and no temp leftovers remain.
func TestCopyIfMissing_SrcMissing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "nope.txt") // does not exist
	dst := filepath.Join(dir, "dst.txt")

	err := CopyIfMissing(src, dst, 0o644)
	if err == nil {
		t.Fatal("expected error when src is missing")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist wrap, got %v", err)
	}
	if _, statErr := os.Stat(dst); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("dst should not survive failed CopyIfMissing, stat err=%v", statErr)
	}
	if leaks := listTmpSiblings(t, dir, "dst.txt"); len(leaks) != 0 {
		t.Fatalf("temp siblings leaked: %v", leaks)
	}
}

// TestSeedHomebrewRuntimeDir_ConcurrentSafe drives many goroutines through the
// full seed flow concurrently. The atomic primitives (CopyIfMissing exclusive
// create + CopyFile temp-rename) must compose without error, race, or partial
// state.
func TestSeedHomebrewRuntimeDir_ConcurrentSafe(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	binDir := filepath.Join(root, "cellar", "bin")
	shareDir := filepath.Join(root, "cellar", "share", "openclaw-dashboard")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(shareDir, "examples"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"refresh.sh":                   "#!/bin/sh\n",
		"themes.json":                  "{}\n",
		"config.json":                  "{}\n",
		"VERSION":                      "v9.9.9\n",
		"examples/config.minimal.json": "{}\n",
	}
	for rel, content := range files {
		p := filepath.Join(shareDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	const workers = 8
	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		errs  = make([]error, workers)
		dirs  = make([]string, workers)
	)
	wg.Add(workers)
	for i := range workers {
		go func() {
			defer wg.Done()
			<-start
			d, _, err := SeedHomebrewRuntimeDir(binDir)
			dirs[i] = d
			errs[i] = err
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d: %v", i, err)
		}
	}
	runtimeDir := filepath.Join(home, ".openclaw", "dashboard")
	for i, d := range dirs {
		if d != runtimeDir {
			t.Errorf("worker %d: got %q, want %q", i, d, runtimeDir)
		}
	}
	for rel := range files {
		got, err := os.ReadFile(filepath.Join(runtimeDir, rel))
		if err != nil {
			t.Fatalf("missing seeded file %s: %v", rel, err)
		}
		if len(got) == 0 {
			t.Fatalf("seeded file %s is empty", rel)
		}
	}
	// No temp leftovers anywhere in runtimeDir.
	_ = filepath.WalkDir(runtimeDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.Contains(d.Name(), ".tmp.") {
			t.Errorf("temp sibling leaked: %s", path)
		}
		return nil
	})
}

// TestResolveOpenclawPath_EnvOverride covers the OPENCLAW_HOME branch and
// tilde expansion.
func TestResolveOpenclawPath_EnvOverride(t *testing.T) {
	t.Setenv("OPENCLAW_HOME", "~/custom-openclaw")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "custom-openclaw")
	if got := ResolveOpenclawPath(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestResolveOpenclawPath_DefaultHome covers the default ~/.openclaw branch.
func TestResolveOpenclawPath_DefaultHome(t *testing.T) {
	t.Setenv("OPENCLAW_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home")
	}
	want := filepath.Join(home, ".openclaw")
	if got := ResolveOpenclawPath(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
