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
