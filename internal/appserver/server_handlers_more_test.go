package appserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// --- handleIndex ----------------------------------------------------------

func TestHandleIndex(t *testing.T) {
	s := newTestServer(t)

	t.Run("GET / returns rendered index", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
		}
		if got := w.Body.Bytes(); string(got) != string(s.indexHTMLRendered) {
			t.Errorf("body = %q, want %q", got, s.indexHTMLRendered)
		}
		if cl := w.Header().Get("Content-Length"); cl != strconv.Itoa(len(s.indexHTMLRendered)) {
			t.Errorf("Content-Length = %q, want %d", cl, len(s.indexHTMLRendered))
		}
		if csp := w.Header().Get("Content-Security-Policy"); csp == "" {
			t.Error("missing Content-Security-Policy header")
		}
		if v := w.Header().Get("X-Content-Type-Options"); v != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q, want nosniff", v)
		}
		if v := w.Header().Get("X-Frame-Options"); v != "DENY" {
			t.Errorf("X-Frame-Options = %q, want DENY", v)
		}
		if v := w.Header().Get("Cache-Control"); v == "" {
			t.Error("missing Cache-Control header")
		}
	})

	t.Run("GET /index.html returns rendered index", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", w.Code)
		}
		if w.Body.String() != string(s.indexHTMLRendered) {
			t.Errorf("body mismatch for /index.html")
		}
	})

	t.Run("HEAD / returns 200 with empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", w.Code)
		}
		if w.Body.Len() != 0 {
			t.Errorf("HEAD body should be empty, got %d bytes", w.Body.Len())
		}
		if cl := w.Header().Get("Content-Length"); cl != strconv.Itoa(len(s.indexHTMLRendered)) {
			t.Errorf("HEAD Content-Length = %q, want %d", cl, len(s.indexHTMLRendered))
		}
	})
}

// --- handleSystem ---------------------------------------------------------

func TestHandleSystem_Disabled(t *testing.T) {
	s := newTestServer(t) // System.Enabled = false

	t.Run("GET 503 application/json ok:false", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("want 503, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		var out struct {
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if out.OK {
			t.Errorf("want ok:false, got ok:true")
		}
	})

	t.Run("HEAD 503 empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/api/system", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("want 503, got %d", w.Code)
		}
		if w.Body.Len() != 0 {
			t.Errorf("HEAD body should be empty, got %d bytes", w.Body.Len())
		}
	})
}

func TestHandleSystem_Enabled(t *testing.T) {
	dir := t.TempDir()
	cfg := appconfig.Default()
	cfg.AI.Enabled = false
	cfg.System.Enabled = true
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	refreshFn := func(ctx context.Context, d, o string, cfg appconfig.Config) error { return nil }
	s := NewServer(dir, "1.0.0-test", cfg, "", []byte("<html><head></head><body></body></html>"), ctx, refreshFn)

	// The enabled path is data-dependent: GetJSON collects host metrics
	// synchronously on first call (cold path) and, on most dev/CI hosts, at
	// least one collector succeeds → 200 + cached payload. We cannot force a
	// deterministic 200 here: metricsPayload is unexported in package appsystem
	// and the only test seam (SetMetricsTimestampForTest) sets the timestamp, not
	// the bytes — priming the payload from this package would require a new
	// exported seam, i.e. a production change, which is out of scope for a
	// tests-only fix. So we read whatever status the host yields and assert the
	// handler's status/body propagation against THAT value, plus the headers and
	// HEAD contract that must hold on every enabled response regardless of status.
	status, body := s.systemSvc.GetJSON(context.Background())

	t.Run("GET propagates status + body, Content-Type + Cache-Control", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != status {
			t.Fatalf("want %d (from systemSvc.GetJSON), got %d", status, w.Code)
		}
		if w.Body.String() != string(body) {
			t.Errorf("body not propagated from systemSvc.GetJSON")
		}
		// Headers are status-independent: the enabled handler always emits JSON
		// content type and no-cache, so these assertions hold on 200 and on a
		// degraded 503 alike — they never vanish on a host where probes fail.
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache", cc)
		}
	})

	t.Run("HEAD enabled empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/api/system", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != status {
			t.Fatalf("want %d, got %d", status, w.Code)
		}
		if w.Body.Len() != 0 {
			t.Errorf("HEAD body should be empty, got %d bytes", w.Body.Len())
		}
	})
}

// --- method gating via ServeHTTP -----------------------------------------

func TestServeHTTP_MethodGating(t *testing.T) {
	s := newTestServer(t)

	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"POST / not allowed", http.MethodPost, "/", http.StatusMethodNotAllowed},
		{"DELETE /api/system not allowed", http.MethodDelete, "/api/system", http.StatusMethodNotAllowed},
		{"GET /api/chat is not a read route", http.MethodGet, "/api/chat", http.StatusNotFound},
		{"PUT /api/chat not allowed", http.MethodPut, "/api/chat", http.StatusMethodNotAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("%s %s: want %d, got %d", tc.method, tc.path, tc.want, w.Code)
			}
		})
	}
}

// --- handleRefresh --------------------------------------------------------

func TestHandleRefresh(t *testing.T) {
	t.Run("recent data.json served, debounce blocks refresh", func(t *testing.T) {
		dir := t.TempDir()
		s := newTestServerForRefresh(t, dir, func(ctx context.Context, d, o string, cfg appconfig.Config) error {
			t.Fatal("refreshFn must not run when debounce blocks")
			return nil
		})
		want := []byte(`{"version":"recent"}`)
		if err := os.WriteFile(filepath.Join(dir, "data.json"), want, 0o644); err != nil {
			t.Fatal(err)
		}
		// Back-date lastRefresh so it is recent (within debounce window).
		s.mu.Lock()
		s.lastRefresh = time.Now()
		s.mu.Unlock()

		req := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
		}
		if w.Body.String() != string(want) {
			t.Errorf("body = %q, want %q", w.Body.Bytes(), want)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache", cc)
		}

		// HEAD: same status, empty body.
		reqH := httptest.NewRequest(http.MethodHead, "/api/refresh", nil)
		wH := httptest.NewRecorder()
		s.ServeHTTP(wH, reqH)
		if wH.Code != http.StatusOK {
			t.Fatalf("HEAD want 200, got %d", wH.Code)
		}
		if wH.Body.Len() != 0 {
			t.Errorf("HEAD body should be empty, got %d bytes", wH.Body.Len())
		}
	})

	t.Run("no data.json, refreshFn writes valid data.json", func(t *testing.T) {
		dir := t.TempDir()
		want := []byte(`{"version":"written"}`)
		s := newTestServerForRefresh(t, dir, func(ctx context.Context, d, o string, cfg appconfig.Config) error {
			return os.WriteFile(filepath.Join(d, "data.json"), want, 0o644)
		})

		req := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
		}
		if w.Body.String() != string(want) {
			t.Errorf("body = %q, want %q", w.Body.Bytes(), want)
		}
	})

	t.Run("refreshFn errors and writes nothing returns 503", func(t *testing.T) {
		dir := t.TempDir()
		s := newTestServerForRefresh(t, dir, func(ctx context.Context, d, o string, cfg appconfig.Config) error {
			return context.Canceled // any error; no data.json written
		})

		req := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("want 503, got %d body=%s", w.Code, w.Body.String())
		}
		if w.Body.String() != string(errDataMissing) {
			t.Errorf("body = %q, want %q", w.Body.Bytes(), errDataMissing)
		}
	})

	t.Run("read error (data.json is a directory) returns 500", func(t *testing.T) {
		dir := t.TempDir()
		// Make data.json a directory: os.Stat succeeds (not IsNotExist), but
		// os.ReadFile fails with a non-IsNotExist error.
		dataPath := filepath.Join(dir, "data.json")
		if err := os.Mkdir(dataPath, 0o755); err != nil {
			t.Fatal(err)
		}
		// Verify the platform yields a non-IsNotExist read error; skip otherwise.
		if _, err := os.ReadFile(dataPath); err == nil || os.IsNotExist(err) {
			t.Skipf("reading a directory did not yield a non-IsNotExist error on this platform: %v", err)
		}

		s := newTestServerForRefresh(t, dir, func(ctx context.Context, d, o string, cfg appconfig.Config) error {
			return nil
		})
		// Back-date so debounce blocks the refresh and we hit the read path.
		s.mu.Lock()
		s.lastRefresh = time.Now()
		s.mu.Unlock()

		req := httptest.NewRequest(http.MethodGet, "/api/refresh", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

// --- handleLogs -----------------------------------------------------------

func TestHandleLogs_Disabled(t *testing.T) {
	cfg := appconfig.Default()
	cfg.AI.Enabled = false
	cfg.System.Enabled = false
	cfg.Logs.Enabled = false
	s := newTestServerWithOpenclawHome(t, cfg, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Error != "logs disabled" {
		t.Errorf("error = %q, want 'logs disabled'", out.Error)
	}
}

func TestHandleLogs_InvalidSince(t *testing.T) {
	cfg := appconfig.Default()
	cfg.AI.Enabled = false
	cfg.System.Enabled = false
	cfg.Logs.Enabled = true
	s := newTestServerWithOpenclawHome(t, cfg, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/logs?since=not-a-time", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Error != "invalid since" {
		t.Errorf("error = %q, want 'invalid since'", out.Error)
	}
}

func TestHandleLogs_SinceFilter(t *testing.T) {
	openclawDir := t.TempDir()
	writeLines(t, filepath.Join(openclawDir, "logs", "gateway.log"),
		"2026-04-13T10:00:00Z gateway before cutoff",
		"2026-04-13T10:00:05Z gateway after cutoff",
	)

	cfg := appconfig.Default()
	cfg.AI.Enabled = false
	cfg.System.Enabled = false
	cfg.Logs.Enabled = true
	cfg.Logs.Sources = []string{"logs/gateway.log"}
	s := newTestServerWithOpenclawHome(t, cfg, openclawDir)

	// Cutoff is strictly between the two lines (10:00:02Z).
	cutoff := time.Date(2026, 4, 13, 10, 0, 2, 0, time.UTC).UnixMilli()
	req := httptest.NewRequest(http.MethodGet, "/api/logs?source=gateway&since="+strconv.FormatInt(cutoff, 10), nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		Count   int   `json:"count"`
		SinceMs int64 `json:"sinceMs"`
		Entries []struct {
			Message string `json:"message"`
			Ts      int64  `json:"timestamp"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload.SinceMs != cutoff {
		t.Errorf("SinceMs = %d, want %d", payload.SinceMs, cutoff)
	}
	if payload.Count != len(payload.Entries) {
		t.Errorf("Count = %d, but %d entries returned", payload.Count, len(payload.Entries))
	}
	if payload.Count != 1 {
		t.Fatalf("want exactly 1 entry >= cutoff, got %d", payload.Count)
	}
	if payload.Entries[0].Message != "gateway after cutoff" {
		t.Errorf("message = %q, want 'gateway after cutoff'", payload.Entries[0].Message)
	}
	if payload.Entries[0].Ts < cutoff {
		t.Errorf("entry timestamp %d < cutoff %d", payload.Entries[0].Ts, cutoff)
	}
}

func TestHandleLogs_LimitClamp(t *testing.T) {
	cfg := appconfig.Default()
	cfg.AI.Enabled = false
	cfg.System.Enabled = false
	cfg.Logs.Enabled = true
	cfg.Logs.TailLines = 0 // force logLimitDefault as the default
	cfg.Logs.Sources = []string{"logs/gateway.log"}
	openclawDir := t.TempDir()
	writeLines(t, filepath.Join(openclawDir, "logs", "gateway.log"),
		"2026-04-13T10:00:00Z gateway one",
	)
	s := newTestServerWithOpenclawHome(t, cfg, openclawDir)

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"limit=0 falls back to default", "?limit=0", logLimitDefault},
		{"non-numeric limit falls back to default", "?limit=abc", logLimitDefault},
		{"oversized limit clamps to max", "?limit=999999", logLimitMax},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/logs"+tc.query, nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
			}
			var payload struct {
				Limit int `json:"limit"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if payload.Limit != tc.want {
				t.Errorf("limit = %d, want %d", payload.Limit, tc.want)
			}
		})
	}
}

// --- defaultLogLimit / defaultErrorWindowHours ----------------------------

func TestDefaultLogLimit(t *testing.T) {
	t.Run("TailLines>0 wins", func(t *testing.T) {
		s := &Server{cfg: appconfig.Config{Logs: appconfig.LogsConfig{TailLines: 42}}}
		if got := s.defaultLogLimit(); got != 42 {
			t.Errorf("defaultLogLimit() = %d, want 42", got)
		}
	})
	t.Run("TailLines<=0 falls back to default", func(t *testing.T) {
		s := &Server{cfg: appconfig.Config{Logs: appconfig.LogsConfig{TailLines: 0}}}
		if got := s.defaultLogLimit(); got != logLimitDefault {
			t.Errorf("defaultLogLimit() = %d, want %d", got, logLimitDefault)
		}
	})
}

func TestDefaultErrorWindowHours(t *testing.T) {
	t.Run("ErrorWindowHours>0 wins", func(t *testing.T) {
		s := &Server{cfg: appconfig.Config{Logs: appconfig.LogsConfig{ErrorWindowHours: 12}}}
		if got := s.defaultErrorWindowHours(); got != 12 {
			t.Errorf("defaultErrorWindowHours() = %d, want 12", got)
		}
	})
	t.Run("ErrorWindowHours<=0 falls back to default", func(t *testing.T) {
		s := &Server{cfg: appconfig.Config{Logs: appconfig.LogsConfig{ErrorWindowHours: 0}}}
		if got := s.defaultErrorWindowHours(); got != errorWindowHoursDefault {
			t.Errorf("defaultErrorWindowHours() = %d, want %d", got, errorWindowHoursDefault)
		}
	})
}

// --- chatRateLimiter.cleanup ----------------------------------------------

func TestChatRateLimiter_Cleanup(t *testing.T) {
	t.Run("removes stale, retains fresh", func(t *testing.T) {
		var rl chatRateLimiter
		rl.allow("1.1.1.1") // stale (will be back-dated)
		rl.allow("2.2.2.2") // fresh

		// Back-date one bucket beyond the 2x window cutoff.
		v, ok := rl.entries.Load("1.1.1.1")
		if !ok {
			t.Fatal("stale bucket not seeded")
		}
		b := v.(*rateBucket)
		b.mu.Lock()
		b.lastReset = time.Now().Add(-2*chatRateWindow - time.Second)
		b.mu.Unlock()

		rl.cleanup()

		if _, ok := rl.entries.Load("1.1.1.1"); ok {
			t.Error("stale bucket should have been removed")
		}
		if _, ok := rl.entries.Load("2.2.2.2"); !ok {
			t.Error("fresh bucket should be retained")
		}
	})

	t.Run("empty limiter cleanup does not panic", func(t *testing.T) {
		var rl chatRateLimiter
		rl.cleanup()
	})
}

// --- PreWarm --------------------------------------------------------------

func TestPreWarm_RunsExactlyOneRefresh(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int64
	s := newTestServerForRefresh(t, dir, func(ctx context.Context, d, o string, cfg appconfig.Config) error {
		calls.Add(1)
		return nil
	})

	before := s.lastRefresh

	// PreWarm spawns the refresh worker. Capture the refreshDone channel under
	// the lock the moment PreWarm returns. runRefresh closes this channel from
	// its deferred cleanup (server_refresh.go) — *after* it has reset
	// refreshRunning and (on success) advanced lastRefresh. So a closed
	// refreshDone is the deterministic "settled" signal, with no sleep or
	// busy-poll on unexported fields. If the worker already finished before we
	// grabbed the lock, refreshDone is nil and the work is already settled.
	s.PreWarm()
	s.mu.Lock()
	doneCh := s.refreshDone
	s.mu.Unlock()
	if doneCh != nil {
		select {
		case <-doneCh:
		case <-time.After(5 * time.Second):
			t.Fatal("refresh did not complete: refreshDone never closed")
		}
	}

	s.mu.Lock()
	advanced := s.lastRefresh.After(before)
	running := s.refreshRunning
	s.mu.Unlock()
	if !advanced {
		t.Error("lastRefresh did not advance after PreWarm")
	}
	if running {
		t.Error("refreshRunning still set after refreshDone closed")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("refreshFn called %d times, want exactly 1", got)
	}
}

// --- SystemService accessor -----------------------------------------------

func TestSystemServiceAccessor(t *testing.T) {
	s := newTestServer(t)
	if s.SystemService() != s.systemSvc {
		t.Error("SystemService() should return the server's systemSvc")
	}
	if s.SystemService() == nil {
		t.Error("SystemService() should be non-nil")
	}
}

// --- helper ---------------------------------------------------------------

// newTestServerForRefresh builds a server with a 1s refresh debounce and an
// injected refreshFn, matching the established harness shape.
func newTestServerForRefresh(t *testing.T, dir string, refreshFn func(context.Context, string, string, appconfig.Config) error) *Server {
	t.Helper()
	cfg := appconfig.Default()
	cfg.AI.Enabled = false
	cfg.System.Enabled = false
	cfg.Refresh.IntervalSeconds = 1
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return NewServer(dir, "1.0.0-test", cfg, "", []byte("<html><head></head><body></body></html>"), ctx, refreshFn)
}
