package appserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

func BenchmarkLoadData_CacheHit(b *testing.B) {
	dir := b.TempDir()
	dataPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(dataPath, []byte(`{"botName":"test","lastRefresh":"2026-01-01"}`), 0o644); err != nil {
		b.Fatal(err)
	}

	cfg := appconfig.Default()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := NewServer(dir, "1.0.0", cfg, "", []byte("<html></html>"), ctx, nil)

	// Prime the cache
	if _, _, err := s.loadData(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for b.Loop() {
		_, _, _ = s.loadData()
	}
}

func BenchmarkLoadData_CacheMiss(b *testing.B) {
	dir := b.TempDir()
	dataPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(dataPath, []byte(`{"botName":"test","lastRefresh":"2026-01-01"}`), 0o644); err != nil {
		b.Fatal(err)
	}

	cfg := appconfig.Default()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := NewServer(dir, "1.0.0", cfg, "", []byte("<html></html>"), ctx, nil)

	b.ResetTimer()
	for b.Loop() {
		// Invalidate cache by resetting mtime tracker
		s.dataMu.Lock()
		s.cachedDataMtime = s.cachedDataMtime.Add(-1)
		s.dataMu.Unlock()

		_, _, _ = s.loadData()
	}
}
