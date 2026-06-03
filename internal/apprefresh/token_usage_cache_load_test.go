package apprefresh

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadTokenUsageCache_Recompute verifies that a cache which is corrupt,
// version-mismatched, or has a nil files map is discarded and recomputed from
// an empty baseline (current version, non-nil Files) rather than poisoning the
// aggregation. The discard now also emits a slog.Warn so schema drift is
// observable; this test pins the recompute behavior.
func TestLoadTokenUsageCache_Recompute(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"corrupt_json", `{not json`},
		{"version_mismatch", `{"version":1,"files":{"a":{}}}`},
		{"nil_files", `{"version":2,"files":null}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cache.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			got := loadTokenUsageCache(path)
			if got.Version != tokenUsageCacheVersion {
				t.Errorf("Version = %d, want %d", got.Version, tokenUsageCacheVersion)
			}
			if got.Files == nil {
				t.Error("Files is nil; want empty non-nil map")
			}
			if len(got.Files) != 0 {
				t.Errorf("Files has %d entries; want 0 (recomputed baseline)", len(got.Files))
			}
		})
	}
}

// TestLoadTokenUsageCache_ValidRoundTrips verifies a well-formed current-version
// cache is preserved (not discarded).
func TestLoadTokenUsageCache_ValidRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	body := `{"version":2,"files":{"sess.jsonl":{"size":10,"modTimeUnixNano":123}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := loadTokenUsageCache(path)
	if got.Version != tokenUsageCacheVersion {
		t.Fatalf("Version = %d, want %d", got.Version, tokenUsageCacheVersion)
	}
	if _, ok := got.Files["sess.jsonl"]; !ok {
		t.Errorf("expected file entry preserved, got %v", got.Files)
	}
}
