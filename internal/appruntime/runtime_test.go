package appruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectVersion_FromFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("v2.3.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := DetectVersion(dir)
	if v != "2.3.4" {
		t.Errorf("expected 2.3.4, got %q", v)
	}
}

func TestDetectVersion_Fallback(t *testing.T) {
	dir := t.TempDir()
	v := DetectVersion(dir)
	if v != "dev" {
		t.Errorf("expected dev fallback, got %q", v)
	}
}

func TestResolveDashboardDirWithError_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCLAW_DASHBOARD_DIR", dir)
	result, err := ResolveDashboardDirWithError("/some/other/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != dir {
		t.Errorf("expected %q, got %q", dir, result)
	}
}

func TestResolveDashboardDirWithError_EnvOverrideTilde(t *testing.T) {
	t.Setenv("OPENCLAW_DASHBOARD_DIR", "~/test-dashboard")
	result, err := ResolveDashboardDirWithError("/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, "test-dashboard")
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestResolveRepoRoot_FindsRefreshSh(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "assets", "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "refresh.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Clear the env var to avoid interference
	t.Setenv("OPENCLAW_DASHBOARD_DIR", "")
	result := ResolveRepoRoot(dir)
	if result != dir {
		t.Errorf("expected %q, got %q", dir, result)
	}
}

func TestCopyIfMissing_DestExists(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyIfMissing(src, dst, 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "existing" {
		t.Errorf("existing file should not be overwritten, got %q", string(data))
	}
}

func TestCopyIfMissing_NewFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "sub", "dst.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyIfMissing(src, dst, 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read copied file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}
}

func TestSeedHomebrewRuntimeDir_UpdatesVersionButPreservesUserConfig(t *testing.T) {
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

	shareFiles := map[string]string{
		"refresh.sh":                   "#!/bin/sh\necho refresh\n",
		"themes.json":                  "{\"midnight\":{}}\n",
		"config.json":                  "{\"server\":{\"port\":8080}}\n",
		"VERSION":                      "v2026.4.8\n",
		"examples/config.minimal.json": "{}\n",
	}
	for rel, content := range shareFiles {
		path := filepath.Join(shareDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	runtimeDir := filepath.Join(home, ".openclaw", "dashboard")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "VERSION"), []byte("v2026.3.8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.json"), []byte("{\"server\":{\"port\":9090}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	gotDir, ok, err := SeedHomebrewRuntimeDir(binDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected Homebrew runtime seeding to activate")
	}
	if gotDir != runtimeDir {
		t.Fatalf("expected runtime dir %q, got %q", runtimeDir, gotDir)
	}

	versionData, err := os.ReadFile(filepath.Join(runtimeDir, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if string(versionData) != "v2026.4.8\n" {
		t.Fatalf("expected runtime VERSION to be updated, got %q", string(versionData))
	}

	configData, err := os.ReadFile(filepath.Join(runtimeDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(configData) != "{\"server\":{\"port\":9090}}\n" {
		t.Fatalf("expected existing config.json to be preserved, got %q", string(configData))
	}
}
