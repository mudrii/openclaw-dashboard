package apprefresh

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSaveTokenUsageCache_EmptyPath asserts the early return on an empty path:
// no file is written and no panic occurs.
func TestSaveTokenUsageCache_EmptyPath(t *testing.T) {
	saveTokenUsageCache("", tokenUsageCache{Version: tokenUsageCacheVersion, Files: map[string]tokenUsageFileSummary{}})
	// Reaching here without panic is the assertion; an empty path resolves to
	// "" + ".tmp" = ".tmp" which must never be created in the working dir.
	if _, err := os.Stat(".tmp"); err == nil {
		_ = os.Remove(".tmp")
		t.Fatal("empty path wrote a .tmp file; expected early return")
	}
}

// TestSaveTokenUsageCache_RenameFailureCleansUpTmp forces os.Rename to fail by
// pre-creating the destination path as a directory. The .tmp must be removed
// and the destination must remain a directory (no partial cache written).
func TestSaveTokenUsageCache_RenameFailureCleansUpTmp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename-onto-directory semantics differ on Windows")
	}
	dir := t.TempDir()
	// Destination is a directory; os.Rename(file, dir) fails on POSIX.
	dest := filepath.Join(dir, "cache.json")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatalf("mkdir dest: %v", err)
	}

	saveTokenUsageCache(dest, tokenUsageCache{Version: tokenUsageCacheVersion, Files: map[string]tokenUsageFileSummary{}})

	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected .tmp cleaned up, stat err: %v", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("dest stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("dest should still be a directory; no partial cache should overwrite it")
	}
}
