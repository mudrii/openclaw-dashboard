package appserver

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

// discardResponseWriter is a minimal http.ResponseWriter for handler
// benchmarks: unlike httptest.ResponseRecorder it neither copies the body nor
// snapshots headers, so allocations reported are the handler's own.
type discardResponseWriter struct {
	header http.Header
	status int
}

func (w *discardResponseWriter) Header() http.Header         { return w.header }
func (w *discardResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *discardResponseWriter) WriteHeader(status int)      { w.status = status }

// newWarmBenchServer returns a server whose data.json cache is primed and
// whose refresh debounce never fires during the benchmark.
func newWarmBenchServer(b *testing.B) *Server {
	b.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(prev) })

	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.json"), marshalDashboardPayload(b), 0o644); err != nil {
		b.Fatal(err)
	}
	cfg := appconfig.Default()
	cfg.AI.Enabled = false
	cfg.Refresh.IntervalSeconds = 3600
	refreshFn := func(context.Context, string, string, appconfig.Config) error { return nil }
	s := NewServer(dir, "1.0.0", cfg, "", []byte("<html><head></head><body>__VERSION__</body></html>"), b.Context(), refreshFn)
	s.mu.Lock()
	s.lastRefreshAttempt = time.Now()
	s.mu.Unlock()
	s.systemSvc.StubHostProbesForTest()
	s.systemSvc.StubOpenclawProbesForTest()
	if _, _, err := s.loadData(); err != nil {
		b.Fatal(err)
	}
	return s
}

func benchServe(b *testing.B, s *Server, method, target, origin string, wantStatus int) {
	b.Helper()
	req := httptest.NewRequest(method, target, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := &discardResponseWriter{header: http.Header{}}
	s.ServeHTTP(w, req)
	if w.status != wantStatus {
		b.Fatalf("%s %s status = %d, want %d", method, target, w.status, wantStatus)
	}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		s.ServeHTTP(w, req)
	}
}

func BenchmarkGetDataRawCached_Warm(b *testing.B) {
	s := newWarmBenchServer(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.GetDataRawCached(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkServeRefresh_Warm(b *testing.B) {
	benchServe(b, newWarmBenchServer(b), http.MethodGet, "http://localhost/api/refresh", "http://localhost:8080", http.StatusOK)
}

func BenchmarkServeIndex(b *testing.B) {
	benchServe(b, newWarmBenchServer(b), http.MethodGet, "http://localhost/", "", http.StatusOK)
}

func BenchmarkServeSystem_Warm(b *testing.B) {
	benchServe(b, newWarmBenchServer(b), http.MethodGet, "http://localhost/api/system", "http://localhost:8080", http.StatusOK)
}

func BenchmarkChatRateLimiterAllow(b *testing.B) {
	var rl chatRateLimiter
	b.ReportAllocs()
	for b.Loop() {
		rl.allow("127.0.0.1") // steady state: existing bucket, limit reached
	}
}
