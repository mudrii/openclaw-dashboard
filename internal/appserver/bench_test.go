package appserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

func BenchmarkLoadData_CacheHit(b *testing.B) {
	dir := b.TempDir()
	dataPath := filepath.Join(dir, "data.json")
	// A production-sized payload, so the cache-miss path measures JSON
	// decoding rather than os.Stat/os.ReadFile syscall overhead.
	if err := os.WriteFile(dataPath, marshalDashboardPayload(b), 0o644); err != nil {
		b.Fatal(err)
	}

	cfg := appconfig.Default()
	ctx := b.Context()

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
	// A production-sized payload, so the cache-miss path measures JSON
	// decoding rather than os.Stat/os.ReadFile syscall overhead.
	if err := os.WriteFile(dataPath, marshalDashboardPayload(b), 0o644); err != nil {
		b.Fatal(err)
	}

	cfg := appconfig.Default()
	ctx := b.Context()

	s := NewServer(dir, "1.0.0", cfg, "", []byte("<html></html>"), ctx, nil)

	b.ResetTimer()
	for b.Loop() {
		// Invalidate cache by resetting mtime tracker
		s.dataMu.Lock()
		s.cachedDataMtime = time.Time{}
		s.dataMu.Unlock()

		_, _, _ = s.loadData()
	}
}
