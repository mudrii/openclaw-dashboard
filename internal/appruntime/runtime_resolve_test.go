package appruntime

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// writeShare lays down a minimal Homebrew share/openclaw-dashboard tree under
// binDir's sibling and returns the share path. Only the named files are written.
func writeShare(t *testing.T, binDir string, files map[string]string) string {
	t.Helper()
	shareDir := filepath.Join(filepath.Dir(binDir), "share", "openclaw-dashboard")
	for rel, content := range files {
		p := filepath.Join(shareDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return shareDir
}

func writeRefreshAsset(t *testing.T, dir string) {
	t.Helper()
	rt := filepath.Join(dir, "assets", "runtime")
	if err := os.MkdirAll(rt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rt, "refresh.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestFindDashboardDir_ParentWalk verifies the parent walk finds an ancestor
// that holds the refresh.sh asset within the 3-level budget, and returns
// ("",false) when the asset is too deep / absent.
func TestFindDashboardDir_ParentWalk(t *testing.T) {
	t.Run("asset in grandparent is found", func(t *testing.T) {
		root := t.TempDir()
		writeRefreshAsset(t, root)
		start := filepath.Join(root, "a", "b", "c")
		if err := os.MkdirAll(start, 0o755); err != nil {
			t.Fatal(err)
		}
		got, ok := findDashboardDir(start)
		if !ok {
			t.Fatal("expected to find ancestor with refresh.sh")
		}
		if got != root {
			t.Errorf("got %q, want ancestor %q", got, root)
		}
	})

	t.Run("asset beyond walk budget is missed", func(t *testing.T) {
		root := t.TempDir()
		writeRefreshAsset(t, root)
		// 4 levels deep: walk only climbs 3 parents, so root is out of reach.
		start := filepath.Join(root, "a", "b", "c", "d")
		if err := os.MkdirAll(start, 0o755); err != nil {
			t.Fatal(err)
		}
		got, ok := findDashboardDir(start)
		if ok {
			t.Fatalf("expected miss, got %q", got)
		}
		if got != "" {
			t.Errorf("expected empty dir on miss, got %q", got)
		}
	})
}

// TestResolveDashboardDirWithError_RepoRootWins verifies a repo-root refresh.sh
// takes precedence over a Homebrew share tree.
func TestResolveDashboardDirWithError_RepoRootWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENCLAW_DASHBOARD_DIR", "")

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Repo asset present at binDir itself.
	writeRefreshAsset(t, binDir)
	// Homebrew share also present — should be ignored in favor of repo root.
	writeShare(t, binDir, map[string]string{
		"themes.json": "{}\n",
		"refresh.sh":  "#!/bin/sh\n",
		"config.json": "{}\n",
		"VERSION":     "v1\n",
	})

	got, err := ResolveDashboardDirWithError(binDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != binDir {
		t.Errorf("expected repo root %q to win, got %q", binDir, got)
	}
}

// TestResolveDashboardDirWithError_SeedsHomebrew verifies that with no repo
// asset but a Homebrew share/themes.json present, the seeded
// ~/.openclaw/dashboard dir is returned.
func TestResolveDashboardDirWithError_SeedsHomebrew(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENCLAW_DASHBOARD_DIR", "")

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeShare(t, binDir, map[string]string{
		"themes.json": "{}\n",
		"refresh.sh":  "#!/bin/sh\n",
		"config.json": "{}\n",
		"VERSION":     "v1\n",
	})

	got, err := ResolveDashboardDirWithError(binDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, ".openclaw", "dashboard")
	if got != want {
		t.Errorf("expected seeded dir %q, got %q", want, got)
	}
	if _, err := os.Stat(filepath.Join(want, "themes.json")); err != nil {
		t.Errorf("expected themes.json seeded: %v", err)
	}
}

// TestResolveDashboardDirWithError_NoMatchReturnsInput verifies that with
// neither repo asset nor Homebrew share, the input dir is returned unchanged
// with a nil error.
func TestResolveDashboardDirWithError_NoMatchReturnsInput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENCLAW_DASHBOARD_DIR", "")

	dir := t.TempDir() // bare, no assets, no sibling share
	got, err := ResolveDashboardDirWithError(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Errorf("expected input dir %q, got %q", dir, got)
	}
}

// TestSeedHomebrewRuntimeDir_NotHomebrewShortCircuits verifies the early return
// when no share/.../themes.json exists: ("", false, nil), no error.
func TestSeedHomebrewRuntimeDir_NotHomebrewShortCircuits(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, ok, err := SeedHomebrewRuntimeDir(binDir)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if ok {
		t.Error("expected ok=false when not a Homebrew layout")
	}
	if dir != "" {
		t.Errorf("expected empty dir, got %q", dir)
	}
}

// TestSeedHomebrewRuntimeDir_MissingExamplesTolerated verifies that an absent
// examples/config.minimal.json is swallowed (os.IsNotExist) while the seed
// still succeeds.
func TestSeedHomebrewRuntimeDir_MissingExamplesTolerated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// All required files present, but no examples/ subtree.
	writeShare(t, binDir, map[string]string{
		"themes.json": "{}\n",
		"refresh.sh":  "#!/bin/sh\n",
		"config.json": "{}\n",
		"VERSION":     "v1\n",
	})

	dir, ok, err := SeedHomebrewRuntimeDir(binDir)
	if err != nil {
		t.Fatalf("expected missing examples to be tolerated, got %v", err)
	}
	if !ok {
		t.Error("expected ok=true")
	}
	if dir != filepath.Join(home, ".openclaw", "dashboard") {
		t.Errorf("unexpected runtime dir %q", dir)
	}
}

// TestSeedHomebrewRuntimeDir_RequiredSrcMissing verifies that when a required
// source (refresh.sh) is absent but themes.json gates the path open, the seed
// fails with an error.
func TestSeedHomebrewRuntimeDir_RequiredSrcMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// themes.json present (gate opens) but refresh.sh missing.
	writeShare(t, binDir, map[string]string{
		"themes.json": "{}\n",
		"config.json": "{}\n",
		"VERSION":     "v1\n",
	})

	_, _, err := SeedHomebrewRuntimeDir(binDir)
	if err == nil {
		t.Fatal("expected error when required refresh.sh source is missing")
	}
	if !strings.Contains(err.Error(), "refresh.sh") {
		t.Errorf("expected error to mention refresh.sh, got %v", err)
	}
}

// TestCopyFile_RenameErrorCleansTmp verifies that when the final rename fails
// (dst is an existing non-empty directory), CopyFile returns an error and
// leaves no .tmp sibling behind.
func TestCopyFile_RenameErrorCleansTmp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	// dst is a non-empty directory: os.Rename(tmpFile, dstDir) must fail.
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "occupant"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dst, 0o644); err == nil {
		t.Fatal("expected error renaming over a non-empty directory")
	}
	if leaks := listTmpSiblings(t, dir, "dst"); len(leaks) != 0 {
		t.Fatalf("temp siblings leaked: %v", leaks)
	}
}

// TestCopyFile_ModeEnforcedUnderUmask verifies CopyFile applies the requested
// mode even when a restrictive process umask would otherwise strip bits.
func TestCopyFile_ModeEnforcedUnderUmask(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX umask semantics required")
	}
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })

	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dst, 0o644); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o644 != 0o644 {
		t.Errorf("dst mode = %o, expected 0o644 bits set despite umask 0o077", info.Mode().Perm())
	}
}
