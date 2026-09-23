package appruntime

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeStat reports the listed paths as existing and everything else as
// missing, so StableExecutablePath is tested without a Homebrew install.
func fakeStat(existing ...string) func(string) (fs.FileInfo, error) {
	return func(p string) (fs.FileInfo, error) {
		if slices.Contains(existing, p) {
			return fakeFileInfo{name: filepath.Base(p)}, nil
		}
		return nil, fs.ErrNotExist
	}
}

type fakeFileInfo struct{ name string }

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return 0o755 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

func TestStableExecutablePath(t *testing.T) {
	tests := []struct {
		name     string
		resolved string
		existing []string
		want     string
	}{
		{
			name:     "apple silicon cellar maps to opt",
			resolved: "/opt/homebrew/Cellar/openclaw-dashboard/2026.9.7/bin/openclaw-dashboard",
			existing: []string{"/opt/homebrew/opt/openclaw-dashboard/bin/openclaw-dashboard"},
			want:     "/opt/homebrew/opt/openclaw-dashboard/bin/openclaw-dashboard",
		},
		{
			name:     "intel cellar maps to opt",
			resolved: "/usr/local/Cellar/openclaw-dashboard/2026.9.7_1/bin/openclaw-dashboard",
			existing: []string{"/usr/local/opt/openclaw-dashboard/bin/openclaw-dashboard"},
			want:     "/usr/local/opt/openclaw-dashboard/bin/openclaw-dashboard",
		},
		{
			name:     "linuxbrew cellar maps to opt",
			resolved: "/home/linuxbrew/.linuxbrew/Cellar/openclaw-dashboard/2026.9.7/bin/openclaw-dashboard",
			existing: []string{"/home/linuxbrew/.linuxbrew/opt/openclaw-dashboard/bin/openclaw-dashboard"},
			want:     "/home/linuxbrew/.linuxbrew/opt/openclaw-dashboard/bin/openclaw-dashboard",
		},
		{
			name:     "missing opt link keeps cellar path",
			resolved: "/opt/homebrew/Cellar/openclaw-dashboard/2026.9.7/bin/openclaw-dashboard",
			want:     "/opt/homebrew/Cellar/openclaw-dashboard/2026.9.7/bin/openclaw-dashboard",
		},
		{
			name:     "non-homebrew path unchanged",
			resolved: "/usr/local/bin/openclaw-dashboard",
			existing: []string{"/usr/local/opt/openclaw-dashboard/bin/openclaw-dashboard"},
			want:     "/usr/local/bin/openclaw-dashboard",
		},
		{
			name:     "cellar path outside bin unchanged",
			resolved: "/opt/homebrew/Cellar/openclaw-dashboard/2026.9.7/libexec/openclaw-dashboard",
			existing: []string{"/opt/homebrew/opt/openclaw-dashboard/libexec/openclaw-dashboard"},
			want:     "/opt/homebrew/Cellar/openclaw-dashboard/2026.9.7/libexec/openclaw-dashboard",
		},
		{
			name:     "relative path unchanged",
			resolved: "Cellar/openclaw-dashboard/1/bin/openclaw-dashboard",
			existing: []string{"opt/openclaw-dashboard/bin/openclaw-dashboard"},
			want:     "Cellar/openclaw-dashboard/1/bin/openclaw-dashboard",
		},
		{
			name:     "empty unchanged",
			resolved: "",
			want:     "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stableExecutablePath(tc.resolved, fakeStat(tc.existing...))
			if got != tc.want {
				t.Errorf("stableExecutablePath(%q) = %q, want %q", tc.resolved, got, tc.want)
			}
		})
	}
}

func TestStableExecutablePath_RealFilesystem(t *testing.T) {
	prefix := t.TempDir()
	cellarBin := filepath.Join(prefix, "Cellar", "openclaw-dashboard", "2026.9.7", "bin")
	if err := os.MkdirAll(cellarBin, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(cellarBin, "openclaw-dashboard")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := StableExecutablePath(bin); got != bin {
		t.Fatalf("without opt link got %q, want %q", got, bin)
	}

	optDir := filepath.Join(prefix, "opt")
	if err := os.MkdirAll(optDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "Cellar", "openclaw-dashboard", "2026.9.7"), filepath.Join(optDir, "openclaw-dashboard")); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(optDir, "openclaw-dashboard", "bin", "openclaw-dashboard")
	if got := StableExecutablePath(bin); got != want {
		t.Errorf("with opt link got %q, want %q", got, want)
	}
}

func TestResolveOpenclawPathWithError_NoHome(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("UserHomeDir reads HOME only on unix")
	}
	t.Setenv("HOME", "")
	t.Setenv("OPENCLAW_HOME", "")
	got, err := ResolveOpenclawPathWithError()
	if err == nil {
		t.Fatalf("expected error without a home dir, got %q", got)
	}
	if got != "" {
		t.Errorf("path = %q, want empty on error", got)
	}
}

func TestResolveOpenclawPathWithError_OverrideNeedsNoHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("OPENCLAW_HOME", "/srv/openclaw")
	got, err := ResolveOpenclawPathWithError()
	if err != nil || got != "/srv/openclaw" {
		t.Errorf("got (%q, %v), want (/srv/openclaw, nil)", got, err)
	}
}

// seedableShare builds a Homebrew-style share dir with every required asset.
func seedableShare(t *testing.T) (binDir, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	binDir = filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeShare(t, binDir, map[string]string{
		"themes.json": "{}\n",
		"refresh.sh":  "#!/bin/sh\n",
		"config.json": "{}\n",
		"VERSION":     "v2\n",
	})
	return binDir, home
}

func TestSeedHomebrewRuntimeDir_FailurePaths(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory perms")
	}

	t.Run("no home dir", func(t *testing.T) {
		binDir, _ := seedableShare(t)
		t.Setenv("HOME", "")
		if _, ok, err := SeedHomebrewRuntimeDir(binDir); err == nil || ok {
			t.Fatalf("got ok=%v err=%v, want error", ok, err)
		}
	})

	t.Run("runtime dir cannot be created", func(t *testing.T) {
		binDir, home := seedableShare(t)
		// A regular file where ~/.openclaw should be blocks MkdirAll.
		if err := os.WriteFile(filepath.Join(home, ".openclaw"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := SeedHomebrewRuntimeDir(binDir)
		if err == nil || !strings.Contains(err.Error(), "create runtime dir") {
			t.Fatalf("err = %v, want create runtime dir error", err)
		}
	})

	t.Run("VERSION source missing", func(t *testing.T) {
		binDir, _ := seedableShare(t)
		shareDir := filepath.Join(filepath.Dir(binDir), "share", "openclaw-dashboard")
		if err := os.Remove(filepath.Join(shareDir, "VERSION")); err != nil {
			t.Fatal(err)
		}
		_, _, err := SeedHomebrewRuntimeDir(binDir)
		if err == nil || !strings.Contains(err.Error(), "VERSION") {
			t.Fatalf("err = %v, want VERSION error", err)
		}
	})

	t.Run("examples dir unwritable", func(t *testing.T) {
		binDir, home := seedableShare(t)
		writeShare(t, binDir, map[string]string{"examples/config.minimal.json": "{}\n"})
		// A regular file where the examples dir should be blocks MkdirAll
		// with a non-ErrNotExist error, which must surface.
		runtimeDir := filepath.Join(home, ".openclaw", "dashboard")
		if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runtimeDir, "examples"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := SeedHomebrewRuntimeDir(binDir)
		if err == nil || !strings.Contains(err.Error(), "examples") {
			t.Fatalf("err = %v, want examples error", err)
		}
	})
}

func TestSeedHomebrewRuntimeDir_CopiesVersionOnce(t *testing.T) {
	binDir, home := seedableShare(t)
	runtimeDir := filepath.Join(home, ".openclaw", "dashboard")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "VERSION"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := SeedHomebrewRuntimeDir(binDir); err != nil || !ok {
		t.Fatalf("seed: ok=%v err=%v", ok, err)
	}
	got, err := os.ReadFile(filepath.Join(runtimeDir, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2\n" {
		t.Errorf("VERSION = %q, want synced v2", got)
	}
}

func TestCopyIfMissing_FailurePaths(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory perms")
	}

	t.Run("parent is a file", func(t *testing.T) {
		dir := t.TempDir()
		blocker := filepath.Join(dir, "blocker")
		if err := os.WriteFile(blocker, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := CopyIfMissing(filepath.Join(dir, "src"), filepath.Join(blocker, "dst"), 0o644); err == nil {
			t.Fatal("expected MkdirAll error")
		}
	})

	t.Run("stat fails with permission error", func(t *testing.T) {
		dir := t.TempDir()
		locked := filepath.Join(dir, "locked")
		if err := os.MkdirAll(locked, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		err := CopyIfMissing(filepath.Join(dir, "src"), filepath.Join(locked, "dst"), 0o644)
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("err = %v, want fs.ErrPermission", err)
		}
	})

	t.Run("temp sibling cannot be created", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		ro := filepath.Join(dir, "ro")
		if err := os.MkdirAll(ro, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		err := CopyIfMissing(src, filepath.Join(ro, "dst"), 0o644)
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("err = %v, want fs.ErrPermission", err)
		}
	})
}

func TestCopyFile_ParentIsFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(filepath.Join(dir, "src"), filepath.Join(blocker, "dst"), 0o644); err == nil {
		t.Fatal("expected MkdirAll error")
	}
}
