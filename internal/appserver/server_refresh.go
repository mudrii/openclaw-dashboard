package appserver

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
	"os"
	"path/filepath"
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
	ch := make(chan struct{})
	s.refreshDone = ch
	s.mu.Unlock()

	go s.runRefresh(ch)
	return ch
}

// runRefresh generates data.json using the Go-native data collector.
// Updates lastRefresh only on success.
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

	if err := s.refreshFn(s.dir, s.openclawPath, s.cfg); err != nil {
		log.Printf("[dashboard] refresh failed: %v", err)
		return
	}

	s.mu.Lock()
	s.lastRefresh = time.Now()
	s.mu.Unlock()
}

// loadData reads data.json with mtime-based caching, filling both raw bytes and
// parsed map atomically under one lock acquisition. Merges the old
// getDataRawCached/getDataCached into a single cache layer to eliminate
// double-read on concurrent requests.
func (s *Server) loadData() ([]byte, map[string]any, error) {
	dataPath := filepath.Join(s.dir, "data.json")
	stat, err := os.Stat(dataPath)
	if err != nil {
		return nil, nil, err
	}
	mtime := stat.ModTime()

	s.dataMu.RLock()
	if s.cachedDataRaw != nil && s.cachedData != nil && !mtime.After(s.cachedDataMtime) {
		raw, parsed := s.cachedDataRaw, s.cachedData
		s.dataMu.RUnlock()
		return raw, parsed, nil
	}
	s.dataMu.RUnlock()

	raw, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, nil, err
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return raw, nil, err
	}
	if parsed == nil {
		return raw, nil, errInvalidDashboardData
	}

	s.dataMu.Lock()
	// Double-check: another goroutine may have updated while we read/parsed
	if s.cachedDataRaw != nil && s.cachedData != nil && !mtime.After(s.cachedDataMtime) {
		raw, parsed = s.cachedDataRaw, s.cachedData
	} else {
		s.cachedDataRaw = raw
		s.cachedData = parsed
		s.cachedDataMtime = mtime
	}
	s.dataMu.Unlock()
	return raw, parsed, nil
}

// getDataRawCached returns cached data.json bytes — delegates to loadData().
func (s *Server) GetDataRawCached() ([]byte, error) {
	raw, _, err := s.loadData()
	return raw, err
}

// handleRefresh implements stale-while-revalidate:
// Returns existing data.json immediately, triggers refresh in background if stale.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	debounce := time.Duration(s.cfg.Refresh.IntervalSeconds) * time.Second

	s.mu.Lock()
	shouldRun := !s.refreshRunning && time.Since(s.lastRefresh) >= debounce
	waitCh := s.refreshDone
	s.mu.Unlock()

	if shouldRun {
		waitCh = s.startRefresh()
	}

	data, err := s.GetDataRawCached()
	if err != nil {
		if !os.IsNotExist(err) {
			s.sendJSON(w, r, http.StatusInternalServerError, map[string]string{"error": "failed to read dashboard data"})
			return
		}
		if waitCh == nil {
			waitCh = s.startRefresh()
		}
		if waitCh != nil {
			ctx, cancel := context.WithTimeout(r.Context(), refreshTimeout)
			defer cancel()
			select {
			case <-waitCh:
			case <-ctx.Done():
			}
			data, err = s.GetDataRawCached()
			if err == nil {
				goto respond
			}
			if !os.IsNotExist(err) {
				s.sendJSON(w, r, http.StatusInternalServerError, map[string]string{"error": "failed to read dashboard data"})
				return
			}
		}
		s.sendJSONRaw(w, r, http.StatusServiceUnavailable, errDataMissing)
		return
	}

respond:
	s.setCORSHeaders(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	log.Printf("[dashboard] GET /api/refresh")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

// getDataCached returns parsed data.json — delegates to loadData().
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
