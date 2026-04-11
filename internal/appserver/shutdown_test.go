package appserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

func testServerWithCtxAndMockRefresh(t *testing.T, dir string, ctx context.Context) *Server {
	t.Helper()
	cfg := appconfig.Config{
		Refresh: appconfig.RefreshConfig{IntervalSeconds: 1},
		AI:      appconfig.AIConfig{Enabled: false},
		System:  appconfig.SystemConfig{Enabled: false},
	}

	// No-op refresh to avoid real CLI calls
	mockRefresh := func(dir, home string, cfg ...appconfig.Config) error {
		return nil
	}
	return NewServer(dir, "test", cfg, "", []byte("<head><body>__VERSION__</body>"), ctx, mockRefresh)
}

func writeMinimalData(t *testing.T, dir string) {
	t.Helper()
	data := []byte(`{"version":"test"}`)
	if err := writeFile(filepath.Join(dir, "data.json"), data); err != nil {
		t.Fatal(err)
	}
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}

func TestStartRefresh_SkipsAfterShutdown(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := testServerWithCtxAndMockRefresh(t, dir, ctx)
	writeMinimalData(t, dir)

	// First request — should work
	req := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Cancel lifecycle context (simulate shutdown)
	cancel()
	time.Sleep(50 * time.Millisecond)

	// Second request after shutdown — should respond quickly, not hang
	req2 := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
	w2 := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.ServeHTTP(w2, req2)
		close(done)
	}()

	select {
	case <-done:
		// Good — didn't hang
	case <-time.After(3 * time.Second):
		t.Fatal("request after shutdown hung — startRefresh may be blocking")
	}
}

func TestStartRefresh_ReturnsInFlightChannelDuringShutdown(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	blockRefresh := make(chan struct{})
	cfg := appconfig.Config{
		Refresh: appconfig.RefreshConfig{IntervalSeconds: 1},
		AI:      appconfig.AIConfig{Enabled: false},
		System:  appconfig.SystemConfig{Enabled: false},
	}

	slowRefresh := func(dir, home string, cfg ...appconfig.Config) error {
		<-blockRefresh // block until test releases
		return nil
	}
	srv := NewServer(dir, "test", cfg, "", []byte("<head><body>__VERSION__</body>"), ctx, slowRefresh)

	// Start a refresh (will block)
	ch := srv.startRefresh()
	if ch == nil {
		t.Fatal("expected non-nil channel for first refresh")
	}

	// Cancel context while refresh is in flight
	cancel()
	time.Sleep(50 * time.Millisecond)

	// startRefresh should still return the in-flight channel
	// (refreshRunning check comes before shutdown check)
	ch2 := srv.startRefresh()
	if ch2 != ch {
		t.Fatal("expected same channel for in-flight refresh even during shutdown")
	}

	// Release the refresh
	close(blockRefresh)
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight refresh didn't complete")
	}
}
