package apprefresh

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSaveTokenUsageCache_FileModeIs0o600 guards the file-mode invariant for
// the token usage cache. The cache holds per-session token counts, costs, and
// model identifiers and should not be world-readable. This test exists so a
// careless edit of the mode bits is caught by CI rather than discovered via
// audit later.
func TestSaveTokenUsageCache_FileModeIs0o600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes on Windows are not POSIX-permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	saveTokenUsageCache(path, tokenUsageCache{Version: tokenUsageCacheVersion, Files: map[string]tokenUsageFileSummary{}})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat cache: %v", err)
	}
	got := info.Mode().Perm()
	const want = os.FileMode(0o600)
	if got != want {
		t.Fatalf("cache file mode: want %#o, got %#o", want, got)
	}
}
