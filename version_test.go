package dashboard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, string(out))
	}
}

func writeRepoRefreshScript(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "assets", "runtime", "refresh.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir assets/runtime: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write refresh.sh: %v", err)
	}
}

func TestDetectVersion_VersionFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2.5.0\n"), 0644)

	v := detectVersion(dir)

	// In a temp dir there's no git repo, so it should fall back to VERSION file
	if v != "2.5.0" {
		t.Fatalf("expected 2.5.0, got %s", v)
	}
}

func TestDetectVersion_Fallback(t *testing.T) {
	dir := t.TempDir()
	// No git, no VERSION file

	v := detectVersion(dir)

	if v != "dev" {
		t.Fatalf("expected dev, got %s", v)
	}
}

func TestDetectVersion_EmptyVersionFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "VERSION"), []byte("  \n"), 0644)

	v := detectVersion(dir)

	if v != "dev" {
		t.Fatalf("expected dev for empty VERSION file, got %s", v)
	}
}

func TestDetectVersion_VersionFilePrecedesGitTag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026.3.5-beta-runtime-observability\n"), 0644); err != nil {
		t.Fatalf("write VERSION: %v", err)
	}
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "config", "user.email", "test@example.com")
	mustRun(t, dir, "git", "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustRun(t, dir, "git", "add", "README.md", "VERSION")
	mustRun(t, dir, "git", "commit", "-m", "init")
	mustRun(t, dir, "git", "tag", "v9999.1.1")

	v := detectVersion(dir)

	if v != "2026.3.5-beta-runtime-observability" {
		t.Fatalf("expected VERSION file to take precedence over git tag, got %s", v)
	}
}

func TestDetectVersion_ParentDirectoryVersionFile(t *testing.T) {
	repoDir := t.TempDir()
	binDir := filepath.Join(repoDir, "dist")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "VERSION"), []byte("2026.3.5-beta-runtime-observability\n"), 0644); err != nil {
		t.Fatalf("write VERSION: %v", err)
	}

	v := detectVersion(binDir)

	if v != "2026.3.5-beta-runtime-observability" {
		t.Fatalf("expected parent VERSION file to be used for dist binary, got %s", v)
	}
}

func TestResolveRepoRoot_Direct(t *testing.T) {
	dir := t.TempDir()
	writeRepoRefreshScript(t, dir)

	got := resolveRepoRoot(dir)
	if got != dir {
		t.Fatalf("expected %s, got %s", dir, got)
	}
}

func TestResolveRepoRoot_DistSubdir(t *testing.T) {
	repo := t.TempDir()
	writeRepoRefreshScript(t, repo)
	dist := filepath.Join(repo, "dist")
	if err := os.MkdirAll(dist, 0755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}

	got := resolveRepoRoot(dist)
	if got != repo {
		t.Fatalf("expected repo root %s, got %s", repo, got)
	}
}

func TestResolveRepoRoot_RepoRootDirect(t *testing.T) {
	// When binary is at repo root (assets/runtime/refresh.sh is in repo), return dir unchanged
	repoDir := t.TempDir()
	writeRepoRefreshScript(t, repoDir)

	got := resolveRepoRoot(repoDir)
	if got != repoDir {
		t.Fatalf("expected %s, got %s", repoDir, got)
	}
}

func TestResolveRepoRoot_BinaryInDist(t *testing.T) {
	// Binary in dist/ subdir, assets/runtime/refresh.sh in parent (repo root)
	repoDir := t.TempDir()
	distDir := filepath.Join(repoDir, "dist")
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	writeRepoRefreshScript(t, repoDir)

	got := resolveRepoRoot(distDir)
	if got != repoDir {
		t.Fatalf("expected repo root %s, got %s", repoDir, got)
	}
}

func TestResolveRepoRoot_BinaryInDeepSubdir(t *testing.T) {
	// Binary in build/output/ (2 levels deep), assets/runtime/refresh.sh at repo root
	repoDir := t.TempDir()
	deepDir := filepath.Join(repoDir, "build", "output")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatalf("mkdir deep dir: %v", err)
	}
	writeRepoRefreshScript(t, repoDir)

	got := resolveRepoRoot(deepDir)
	if got != repoDir {
		t.Fatalf("expected repo root %s from 2 levels deep, got %s", repoDir, got)
	}
}

func TestResolveRepoRoot_NoRefreshScript(t *testing.T) {
	// No repo refresh script anywhere — returns original dir
	dir := t.TempDir()
	got := resolveRepoRoot(dir)
	if got != dir {
		t.Fatalf("expected unchanged dir %s when no refresh.sh found, got %s", dir, got)
	}
}

func TestResolveRepoRoot_TooDeep(t *testing.T) {
	// assets/runtime/refresh.sh is 4 levels up — beyond the 3-level limit
	repoDir := t.TempDir()
	deepDir := filepath.Join(repoDir, "a", "b", "c", "d")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatalf("mkdir deep dir: %v", err)
	}
	writeRepoRefreshScript(t, repoDir)

	got := resolveRepoRoot(deepDir)
	// Should NOT find assets/runtime/refresh.sh (4 levels up > 3 max)
	if got == repoDir {
		t.Fatalf("should not walk more than 3 levels up, but found repo root at %s", repoDir)
	}
}

func TestResolveDashboardDir_EnvOverride(t *testing.T) {
	override := t.TempDir()
	t.Setenv("OPENCLAW_DASHBOARD_DIR", override)

	got := resolveDashboardDir("/tmp/does-not-matter")
	if got != override {
		t.Fatalf("expected env override %s, got %s", override, got)
	}
}

func TestResolveDashboardDir_HomebrewSeedsRuntimeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENCLAW_DASHBOARD_DIR", "")

	cellar := filepath.Join(t.TempDir(), "Cellar", "openclaw-dashboard", "1.2.3")
	binDir := filepath.Join(cellar, "bin")
	shareDir := filepath.Join(cellar, "share", "openclaw-dashboard")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(shareDir, "examples"), 0o755); err != nil {
		t.Fatalf("mkdir share examples: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shareDir, "themes.json"), []byte(`{"midnight":true}`), 0o644); err != nil {
		t.Fatalf("write themes.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shareDir, "config.json"), []byte(`{"server":{"port":8080}}`), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shareDir, "refresh.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write refresh.sh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shareDir, "VERSION"), []byte("9.9.9\n"), 0o644); err != nil {
		t.Fatalf("write VERSION: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shareDir, "examples", "config.minimal.json"), []byte(`{"server":{"port":8080}}`), 0o644); err != nil {
		t.Fatalf("write config.minimal.json: %v", err)
	}

	got := resolveDashboardDir(binDir)
	want := filepath.Join(home, ".openclaw", "dashboard")
	if got != want {
		t.Fatalf("expected homebrew runtime dir %s, got %s", want, got)
	}

	for _, rel := range []string{
		"themes.json",
		"config.json",
		"refresh.sh",
		"VERSION",
		filepath.Join("examples", "config.minimal.json"),
	} {
		if _, err := os.Stat(filepath.Join(got, rel)); err != nil {
			t.Fatalf("expected seeded %s: %v", rel, err)
		}
	}
}

func TestResolveDashboardDirWithError_HomebrewMissingRequiredAssetFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENCLAW_DASHBOARD_DIR", "")

	cellar := filepath.Join(t.TempDir(), "Cellar", "openclaw-dashboard", "1.2.3")
	binDir := filepath.Join(cellar, "bin")
	shareDir := filepath.Join(cellar, "share", "openclaw-dashboard")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(shareDir, "examples"), 0o755); err != nil {
		t.Fatalf("mkdir share examples: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shareDir, "themes.json"), []byte(`{"midnight":true}`), 0o644); err != nil {
		t.Fatalf("write themes.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shareDir, "VERSION"), []byte("9.9.9\n"), 0o644); err != nil {
		t.Fatalf("write VERSION: %v", err)
	}
	// refresh.sh intentionally missing

	_, err := resolveDashboardDirWithError(binDir)
	if err == nil {
		t.Fatal("expected missing required Homebrew asset to fail resolution")
	}
	if !strings.Contains(err.Error(), "refresh.sh") {
		t.Fatalf("expected error to mention refresh.sh, got %v", err)
	}
}
