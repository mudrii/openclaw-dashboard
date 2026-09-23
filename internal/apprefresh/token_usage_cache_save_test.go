package apprefresh

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

func TestSaveTokenUsageCacheDoesNotShareTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	otherWriter, err := os.Create(path + ".tmp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = otherWriter.Close() })
	saveTokenUsageCache(path, tokenUsageCache{Version: tokenUsageCacheVersion, Files: map[string]tokenUsageFileSummary{}})
	if _, err := otherWriter.WriteAt([]byte("not JSON"), 0); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !json.Valid(raw) {
		t.Fatalf("another writer corrupted published cache: %s, %v", raw, err)
	}
}

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

	if files, err := filepath.Glob(dest + ".tmp*"); err != nil || len(files) != 0 {
		t.Fatalf("temporary files leaked: %v, %v", files, err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("dest stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("dest should still be a directory; no partial cache should overwrite it")
	}
}

// TestCollectTokenUsageWithCache_RewritesCacheOnlyWhenChanged guards the
// steady-state skip: an unchanged transcript set must not re-marshal and
// fsync the cache, while any parse, removal or zone change must persist.
func TestCollectTokenUsageWithCache_RewritesCacheOnlyWhenChanged(t *testing.T) {
	tmp := t.TempDir()
	basePath := filepath.Join(tmp, "agents")
	cachePath := filepath.Join(tmp, "token-cache.json")
	line := `{"timestamp":"2026-03-22T10:00:00Z","message":{"role":"assistant","model":"openai/gpt-5","usage":{"totalTokens":100,"cost":{"total":0.1}}}}`
	a := filepath.Join(basePath, "main", "sessions", "a.jsonl")
	b := filepath.Join(basePath, "main", "sessions", "b.jsonl")
	writeJSONL(t, a, line)
	writeJSONL(t, b, line, line)

	collect := func(loc *time.Location) {
		t.Helper()
		agg := freshAggregates()
		CollectTokenUsageWithCache(
			cachePath, basePath, loc, "2026-03-22", "2026-03-15", "2026-02-20",
			map[string]string{}, map[string]string{}, map[string]string{},
			agg.modelsAll, agg.modelsToday, agg.models7d, agg.models30d,
			agg.subagentAll, agg.subagentToday, agg.subagent7d, agg.subagent30d,
			agg.dailyCosts, agg.dailyTokens, agg.dailyCalls, agg.dailySubagentCosts, agg.dailySubagentCount,
		)
	}
	stat := func() os.FileInfo {
		t.Helper()
		info, err := os.Stat(cachePath)
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	cachedFiles := func() []string {
		t.Helper()
		return slices.Sorted(maps.Keys(loadTokenUsageCache(cachePath).Files))
	}

	collect(time.UTC)
	first := stat()
	if got := cachedFiles(); !slices.Equal(got, []string{a, b}) {
		t.Fatalf("cached files = %v, want [%s %s]", got, a, b)
	}

	collect(time.UTC)
	if !os.SameFile(first, stat()) {
		t.Fatal("unchanged transcripts rewrote the cache")
	}

	steps := []struct {
		name   string
		mutate func()
		loc    *time.Location
		want   []string
	}{
		{"appended transcript", func() { writeJSONL(t, a, line, line, line) }, time.UTC, []string{a, b}},
		{"removed transcript", func() { _ = os.Remove(b) }, time.UTC, []string{a}},
		{"timezone change", func() {}, time.FixedZone("plus8", 8*3600), []string{a}},
	}
	for _, step := range steps {
		before := stat()
		step.mutate()
		collect(step.loc)
		if os.SameFile(before, stat()) {
			t.Fatalf("%s: cache was not rewritten", step.name)
		}
		if got := cachedFiles(); !slices.Equal(got, step.want) {
			t.Fatalf("%s: cached files = %v, want %v", step.name, got, step.want)
		}
	}
}
