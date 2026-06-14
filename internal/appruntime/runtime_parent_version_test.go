package appruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestDetectVersion_FromParentDir covers the second iteration of the lookup
// loop (runtime.go:276): no VERSION in dir, but one in filepath.Dir(dir).
func TestDetectVersion_FromParentDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "child")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "VERSION"), []byte("v9.8.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := DetectVersion(context.Background(), dir)
	if v != "9.8.7" {
		t.Errorf("DetectVersion = %q, want %q (from parent VERSION)", v, "9.8.7")
	}
}
