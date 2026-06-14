package apprefresh

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFileSync_OpenFailure asserts the OpenFile error path: a path whose
// parent does not exist must return a non-nil error and create no file.
func TestWriteFileSync_MissingParent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "does-not-exist", "out.json")

	err := writeFileSync(target, []byte("payload"), 0o600)
	if err == nil {
		t.Fatal("expected error for missing parent dir, got nil")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("expected no file written, stat err: %v", statErr)
	}
}

// TestWriteFileSync_ParentIsFile asserts the same error contract when the
// parent path component is a regular file rather than a directory.
func TestWriteFileSync_ParentIsFile(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "afile")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed parent file: %v", err)
	}
	target := filepath.Join(parent, "out.json")

	err := writeFileSync(target, []byte("payload"), 0o600)
	if err == nil {
		t.Fatal("expected error when parent is a file, got nil")
	}
	// A path component being a file makes the target unstattable (ENOTDIR);
	// the contract under test is that no payload file exists.
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatal("expected no file written, but target exists")
	}
}
