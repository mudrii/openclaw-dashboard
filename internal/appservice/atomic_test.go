package appservice

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	t.Run("writes content with the requested permission", func(t *testing.T) {
		dir := t.TempDir()
		dst := filepath.Join(dir, "unit.conf")
		want := []byte("secret=token\n")

		if err := writeFileAtomic(dst, want, 0o600); err != nil {
			t.Fatalf("writeFileAtomic: %v", err)
		}

		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if string(got) != string(want) {
			t.Errorf("content = %q, want %q", got, want)
		}

		info, err := os.Stat(dst)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("perm = %o, want 0o600", perm)
		}
	})

	t.Run("overwrites an existing file atomically", func(t *testing.T) {
		dir := t.TempDir()
		dst := filepath.Join(dir, "unit.conf")
		if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
			t.Fatalf("seed dst: %v", err)
		}
		if err := writeFileAtomic(dst, []byte("new"), 0o600); err != nil {
			t.Fatalf("writeFileAtomic: %v", err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if string(got) != "new" {
			t.Errorf("content = %q, want %q", got, "new")
		}
	})

	t.Run("errors and leaks no temp file when the parent dir is missing", func(t *testing.T) {
		dir := t.TempDir()
		// Parent directory of dst does not exist → temp create fails.
		dst := filepath.Join(dir, "nope", "unit.conf")
		if err := writeFileAtomic(dst, []byte("data"), 0o600); err == nil {
			t.Fatal("expected error writing into a missing directory, got nil")
		}
		assertNoTempLeak(t, dir)
	})

	t.Run("errors and leaks no temp file when dst is a directory", func(t *testing.T) {
		dir := t.TempDir()
		dst := filepath.Join(dir, "target")
		if err := os.Mkdir(dst, 0o755); err != nil {
			t.Fatalf("mkdir dst: %v", err)
		}
		// Rename of the temp file onto an existing directory must fail.
		if err := writeFileAtomic(dst, []byte("data"), 0o600); err == nil {
			t.Fatal("expected error when dst is a directory, got nil")
		}
		assertNoTempLeak(t, dir)
	})
}

// assertNoTempLeak fails if any writeFileAtomic temp file (".*.tmp-*") remains
// in dir after a failed write.
func assertNoTempLeak(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && strings.Contains(name, ".tmp-") {
			t.Errorf("leaked temp file: %q", name)
		}
	}
}

func TestUniqueTempPath(t *testing.T) {
	t.Run("returns a non-existing path inside dir", func(t *testing.T) {
		dir := t.TempDir()
		p, err := uniqueTempPath(dir, "unit.conf")
		if err != nil {
			t.Fatalf("uniqueTempPath: %v", err)
		}
		if got := filepath.Dir(p); got != dir {
			t.Errorf("temp path dir = %q, want %q", got, dir)
		}
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("expected returned path to not exist yet, lstat err = %v", err)
		}
	})

	t.Run("skips an already-occupied candidate to find a free slot", func(t *testing.T) {
		dir := t.TempDir()
		// The probe loop starts at index 0; pre-create that candidate so the
		// function must advance to index 1.
		first := filepath.Join(dir, fmt.Sprintf(".unit.conf.tmp-%d-0", os.Getpid()))
		if err := os.WriteFile(first, nil, 0o600); err != nil {
			t.Fatalf("seed first candidate: %v", err)
		}
		p, err := uniqueTempPath(dir, "unit.conf")
		if err != nil {
			t.Fatalf("uniqueTempPath: %v", err)
		}
		if p == first {
			t.Errorf("expected uniqueTempPath to skip occupied %q", first)
		}
	})

	t.Run("errors when every candidate is occupied", func(t *testing.T) {
		dir := t.TempDir()
		for i := 0; i < 1000; i++ {
			p := filepath.Join(dir, fmt.Sprintf(".unit.conf.tmp-%d-%d", os.Getpid(), i))
			if err := os.WriteFile(p, nil, 0o600); err != nil {
				t.Fatalf("seed candidate %d: %v", i, err)
			}
		}
		if _, err := uniqueTempPath(dir, "unit.conf"); err == nil {
			t.Fatal("expected exhausted temp path error")
		}
	})
}
