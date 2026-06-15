package apprefresh

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	want := tokenUsageCache{
		Version: tokenUsageCacheVersion,
		Files: map[string]tokenUsageFileSummary{
			"sess.jsonl": {
				Size:            10,
				ModTimeUnixNano: 123,
				SessionModel:    "openai/gpt-5",
				Models: map[string]TokenBucket{
					"openai/gpt-5": {Calls: 1, Input: 60, Output: 40, Total: 100, Cost: 0.12},
				},
				Daily: map[string]map[string]TokenBucket{
					"2026-03-22": {
						"openai/gpt-5": {Calls: 1, Input: 60, Output: 40, Total: 100, Cost: 0.12},
					},
				},
			},
		},
	}
	body, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	got := loadTokenUsageCache(path)
	if got.Version != tokenUsageCacheVersion {
		t.Fatalf("Version = %d, want %d", got.Version, tokenUsageCacheVersion)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cache mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}
