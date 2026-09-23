package appserver

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// startRefresh launches at most one refresh worker and returns a channel that
// closes when the current refresh attempt completes.
func (s *Server) startRefresh() chan struct{} {
	s.mu.Lock()
	if s.refreshRunning {
		ch := s.refreshDone
		s.mu.Unlock()
		return ch
	}
	// If shutdown is already in progress, skip spawning a new goroutine.
	// A nil channel in a select case never fires, so this correctly prevents
	// new refreshes after shutdown without blocking the caller.
	select {
	case <-s.done:
		s.mu.Unlock()
		return nil
	default:
	}
	s.refreshRunning = true
	s.lastRefreshAttempt = time.Now()
	ch := make(chan struct{})
	s.refreshDone = ch
	s.mu.Unlock()

	go s.runRefresh(ch)
	return ch
}

// WaitRefresh blocks until the in-flight refresh, if any, has finished or ctx
// is done, and returns ctx.Err() in the latter case.
func (s *Server) WaitRefresh(ctx context.Context) error {
	s.mu.Lock()
	ch := s.refreshDone
	s.mu.Unlock()
	if ch == nil {
		return nil
	}
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runRefresh generates data.json using the Go-native data collector, bounded
// by RefreshDeadline so a hung OpenClaw subprocess cannot pin the worker.
func (s *Server) runRefresh(done chan struct{}) {
	defer func() {
		s.mu.Lock()
		s.refreshRunning = false
		if s.refreshDone == done {
			s.refreshDone = nil
		}
		s.mu.Unlock()
		close(done)
	}()

	ctx, cancel := context.WithTimeout(s.ctx, RefreshDeadline)
	defer cancel()
	if err := s.refreshFn(ctx, s.dir, s.openclawPath, s.cfg); err != nil {
		slog.Error("[dashboard] refresh failed", "error", err)
	}
}

// loadData reads data.json with mtime-based caching, filling both raw bytes and
// parsed map atomically under one lock acquisition. Merges the old
// getDataRawCached/getDataCached into a single cache layer to eliminate
// double-read on concurrent requests.
//
// Callers receive a top-level shallow clone of the cached map so concurrent
// refreshes that swap or mutate s.cachedData cannot race with caller iteration.
// The raw byte slice is treated as immutable (only ever replaced wholesale)
// and is returned as-is.
func (s *Server) loadData() ([]byte, map[string]any, error) {
	dataPath := filepath.Join(s.dir, "data.json")
	stat, err := os.Stat(dataPath)
	if err != nil {
		return nil, nil, err
	}
	mtime, size := stat.ModTime(), stat.Size()

	s.dataMu.RLock()
	if s.cachedDataRaw != nil && s.cachedData != nil && mtime.Equal(s.cachedDataMtime) && size == s.cachedDataSize {
		raw := s.cachedDataRaw
		parsed := maps.Clone(s.cachedData)
		s.dataMu.RUnlock()
		return raw, parsed, nil
	}
	s.dataMu.RUnlock()

	raw, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, nil, err
	}
	var parsed map[string]any
	// data.json is written by this program (apprefresh marshals it with
	// encoding/json v1 MarshalIndent), so it can contain neither duplicate
	// object members nor invalid UTF-8 by construction. Decoding with strict
	// encoding/json/v2 therefore yields the same map as v1 while skipping v1's
	// duplicate-name bookkeeping; a rejection here means the file is corrupt.
	if err := jsonv2.Unmarshal(raw, &parsed); err != nil {
		slog.Warn("[dashboard] data.json failed strict decode; regenerate it with --refresh", "path", dataPath, "error", err.Error())
		return raw, nil, err
	}
	if parsed == nil {
		return raw, nil, errInvalidDashboardData
	}

	s.dataMu.Lock()
	// Double-check: another goroutine may have updated while we read/parsed
	if s.cachedDataRaw != nil && s.cachedData != nil && mtime.Equal(s.cachedDataMtime) && size == s.cachedDataSize {
		raw = s.cachedDataRaw
		parsed = maps.Clone(s.cachedData)
	} else {
		s.cachedDataRaw = raw
		s.cachedData = parsed
		s.cachedDataMtime = mtime
		s.cachedDataSize = size
		parsed = maps.Clone(parsed)
	}
	s.dataMu.Unlock()
	return raw, parsed, nil
}

// GetDataRawCached returns the cached data.json bytes, re-reading the file when
// its mtime or size changed.
func (s *Server) GetDataRawCached() ([]byte, error) {
	raw, _, err := s.loadData()
	return raw, err
}

// handleRefresh implements stale-while-revalidate:
// Returns existing data.json immediately, triggers refresh in background if the
// last attempt (successful or not) is older than the debounce interval.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	debounce := time.Duration(s.cfg.Refresh.IntervalSeconds) * time.Second

	s.mu.Lock()
	shouldRun := !s.refreshRunning && time.Since(s.lastRefreshAttempt) >= debounce
	waitCh := s.refreshDone
	s.mu.Unlock()

	if shouldRun {
		waitCh = s.startRefresh()
	}

	data, err := s.GetDataRawCached()
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.sendJSON(w, r, http.StatusInternalServerError, map[string]string{"error": "failed to read dashboard data"})
			return
		}
		// A nil waitCh means the debounce is holding back a retry after a
		// recent attempt (or shutdown began): report missing data rather than
		// re-running a collector that just failed.
		if waitCh != nil {
			ctx, cancel := context.WithTimeout(r.Context(), refreshTimeout)
			defer cancel()
			select {
			case <-waitCh:
			case <-ctx.Done():
			}
			data, err = s.GetDataRawCached()
			if err == nil {
				s.writeRefreshResponse(w, r, data)
				return
			}
			if !errors.Is(err, fs.ErrNotExist) {
				s.sendJSON(w, r, http.StatusInternalServerError, map[string]string{"error": "failed to read dashboard data"})
				return
			}
		}
		s.sendJSONRaw(w, r, http.StatusServiceUnavailable, errDataMissing)
		return
	}

	s.writeRefreshResponse(w, r, data)
}

func (s *Server) writeRefreshResponse(w http.ResponseWriter, r *http.Request, data []byte) {
	s.setCORSHeaders(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	slog.Info("[dashboard] GET /api/refresh")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

// GetDataCached returns a shallow clone of the parsed data.json, re-reading the
// file when its mtime or size changed.
func (s *Server) GetDataCached() (map[string]any, error) {
	_, parsed, err := s.loadData()
	if err != nil {
		return nil, err
	}
	if parsed == nil {
		return nil, errInvalidDashboardData
	}
	return parsed, nil
}
