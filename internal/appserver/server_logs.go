package appserver

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

type logEntry struct {
	Source   string `json:"source"`
	SeenAt   string `json:"seenAt"`
	Severity string `json:"severity"`
	Line     string `json:"line"`
	Message  string `json:"message"`
	Raw      string `json:"raw"`
	Ts       int64  `json:"timestamp"`
}

type logsResponse struct {
	OK       bool       `json:"ok"`
	Limit    int        `json:"limit"`
	Count    int        `json:"count"`
	Sources  []string   `json:"sources"`
	Entries  []logEntry `json:"entries"`
	SinceMs  int64      `json:"sinceMs"`
	FastMode int        `json:"fastModeMs"`
}

type logErrorOccurrence struct {
	Timestamp int64  `json:"timestamp"`
	Line      string `json:"line"`
	Message   string `json:"message"`
}

type errorFeedItem struct {
	Source          string               `json:"source"`
	Severity        string               `json:"severity"`
	Signature       string               `json:"signature"`
	Count           int                  `json:"count"`
	FirstSeen       int64                `json:"firstSeen"`
	LastSeen        int64                `json:"lastSeen"`
	SampleMessage   string               `json:"sampleMessage"`
	LastOccurrences []logErrorOccurrence `json:"lastOccurrences"`
}

type errorsResponse struct {
	OK      bool            `json:"ok"`
	Window  int             `json:"windowHours"`
	Count   int             `json:"count"`
	Sort    string          `json:"sort"`
	Limit   int             `json:"limit"`
	Items   []errorFeedItem `json:"items"`
	Sources []string        `json:"sources"`
	// DroppedSignatures counts unique signatures rejected once the per-request
	// dedup map reached cfg.Logs.MaxErrorSignatures. Ordering policy is
	// first-seen: earlier signatures retain their slot, later ones are dropped
	// silently aside from this counter so operators can detect saturation.
	DroppedSignatures int `json:"dropped_signatures"`
}

const (
	logLimitDefault         = 200
	logLimitMax             = 1000
	errorLimitDefault       = 1000
	errorWindowHoursDefault = 24
	errorWindowHoursMax     = 168
)

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Logs.Enabled {
		s.sendJSONRaw(w, r, http.StatusServiceUnavailable, []byte(`{"error":"logs disabled"}`))
		return
	}

	query := r.URL.Query()
	limit := clampInt(query.Get("limit"), s.defaultLogLimit(), 1, logLimitMax)
	sources := resolveSources(query.Get("source"), apprefresh.GetEffectiveLogSources(s.cfg))
	sourceList := append([]string(nil), sources...)
	sinceRaw := query.Get("since")
	sinceMs := int64(0)
	if sinceRaw != "" {
		v, err := parseSince(sinceRaw, 0)
		if err != nil {
			s.sendJSONRaw(w, r, http.StatusBadRequest, []byte(`{"error":"invalid since"}`))
			return
		}
		sinceMs = v
	}

	entries, err := s.readMergedLogs(sources, limit)
	if err != nil {
		s.sendJSONRaw(w, r, http.StatusInternalServerError, []byte(`{"error":"failed to read logs"}`))
		return
	}

	filtered := make([]apprefresh.LogRecord, 0, len(entries))
	if sinceMs > 0 {
		threshold := time.UnixMilli(sinceMs)
		for _, entry := range entries {
			if entry.Timestamp.IsZero() || !entry.Timestamp.Before(threshold) {
				filtered = append(filtered, entry)
			}
		}
	} else {
		filtered = entries
	}

	payload := logsResponse{
		OK:       true,
		Limit:    limit,
		Count:    len(filtered),
		Sources:  sourceList,
		SinceMs:  sinceMs,
		FastMode: s.cfg.Logs.FastRefreshMs,
		Entries:  make([]logEntry, 0, len(filtered)),
	}
	for _, entry := range filtered {
		payload.Entries = append(payload.Entries, logEntry{
			Source:   entry.Source,
			SeenAt:   entry.SeenAt,
			Severity: entry.Severity,
			Line:     entry.Line,
			Message:  entry.Message,
			Raw:      entry.Raw,
			Ts:       entry.TimestampMs,
		})
	}

	s.sendJSON(w, r, http.StatusOK, payload)
}

func (s *Server) handleErrors(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Logs.Enabled {
		s.sendJSONRaw(w, r, http.StatusServiceUnavailable, []byte(`{"error":"logs disabled"}`))
		return
	}

	query := r.URL.Query()
	sortMode := strings.ToLower(strings.TrimSpace(query.Get("sort")))
	if sortMode != "count" && sortMode != "last_seen" {
		sortMode = "count"
	}
	limit := clampInt(query.Get("limit"), errorLimitDefault, 1, errorLimitDefault)
	windowHours := clampInt(query.Get("windowHours"), s.defaultErrorWindowHours(), 1, errorWindowHoursMax)
	sources := resolveSources(query.Get("source"), apprefresh.GetEffectiveLogSources(s.cfg))
	rawSourceList := append([]string(nil), sources...)

	entries, err := s.readMergedLogs(sources, errorLimitDefault)
	if err != nil {
		s.sendJSONRaw(w, r, http.StatusInternalServerError, []byte(`{"error":"failed to read logs"}`))
		return
	}

	windowStart := time.Now().Add(-time.Duration(windowHours) * time.Hour)
	itemsBySig := make(map[string]*errorFeedItem, s.cfg.Logs.MaxErrorSignatures)
	droppedSignatures := 0
	for _, entry := range entries {
		if entry.Timestamp.IsZero() || entry.Timestamp.Before(windowStart) {
			continue
		}
		if entry.Severity != "warn" && entry.Severity != "error" {
			continue
		}

		sig := entry.Source + "|" + apprefresh.NormalizeErrorSignature(entry.Message)
		if len(itemsBySig) >= s.cfg.Logs.MaxErrorSignatures {
			if _, exists := itemsBySig[sig]; !exists {
				droppedSignatures++
				continue
			}
		}

		item, ok := itemsBySig[sig]
		if !ok {
			item = &errorFeedItem{
				Source:        entry.Source,
				Severity:      entry.Severity,
				Signature:     sig,
				Count:         0,
				FirstSeen:     entry.TimestampMs,
				LastSeen:      entry.TimestampMs,
				SampleMessage: entry.Message,
			}
			itemsBySig[sig] = item
		}
		if entry.TimestampMs < item.FirstSeen {
			item.FirstSeen = entry.TimestampMs
		}
		if entry.TimestampMs > item.LastSeen {
			item.LastSeen = entry.TimestampMs
		}
		item.Count++
		item.LastOccurrences = append(item.LastOccurrences, logErrorOccurrence{
			Timestamp: entry.TimestampMs,
			Line:      entry.Line,
			Message:   entry.Message,
		})
		if len(item.LastOccurrences) > 3 {
			item.LastOccurrences = item.LastOccurrences[len(item.LastOccurrences)-3:]
		}
	}

	items := make([]errorFeedItem, 0, len(itemsBySig))
	for _, item := range itemsBySig {
		items = append(items, *item)
	}

	switch sortMode {
	case "count":
		slices.SortFunc(items, func(a, b errorFeedItem) int {
			if a.Count != b.Count {
				return cmp.Compare(b.Count, a.Count)
			}
			return cmp.Compare(b.LastSeen, a.LastSeen)
		})
	default:
		slices.SortFunc(items, func(a, b errorFeedItem) int {
			return cmp.Compare(b.LastSeen, a.LastSeen)
		})
	}

	if len(items) > limit {
		items = items[:limit]
	}

	s.sendJSON(w, r, http.StatusOK, errorsResponse{
		OK:                true,
		Window:            windowHours,
		Count:             len(items),
		Sort:              sortMode,
		Limit:             limit,
		Items:             items,
		Sources:           rawSourceList,
		DroppedSignatures: droppedSignatures,
	})
}

func (s *Server) readMergedLogs(sources []string, globalLimit int) ([]apprefresh.LogRecord, error) {
	return apprefresh.ReadMergedLogs(s.openclawPath, sources, globalLimit)
}

func (s *Server) defaultLogLimit() int {
	if s.cfg.Logs.TailLines > 0 {
		return s.cfg.Logs.TailLines
	}
	return logLimitDefault
}

func (s *Server) defaultErrorWindowHours() int {
	if s.cfg.Logs.ErrorWindowHours > 0 {
		return s.cfg.Logs.ErrorWindowHours
	}
	return errorWindowHoursDefault
}

func resolveSources(raw string, configured []string) []string {
	if strings.TrimSpace(raw) == "" {
		return append([]string(nil), configured...)
	}
	requested := strings.Split(raw, ",")
	if len(requested) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(configured))
	result := make([]string, 0, len(requested))
	for _, source := range requested {
		for _, candidate := range resolveSourceToken(source, configured) {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			result = append(result, candidate)
		}
	}
	return result
}

func resolveSourceToken(raw string, configured []string) []string {
	requested := strings.ToLower(strings.TrimSpace(raw))
	switch requested {
	case "", "all":
		return append([]string(nil), configured...)
	case "gateway":
		return configuredByContains(configured, "gateway")
	case "cron":
		return configuredByContains(configured, "cron")
	case "session":
		return configuredByContains(configured, "session")
	case "subagent", "sub-agent":
		return configuredByContains(configured, "subagent")
	}

	matches := make([]string, 0, 1)
	for _, source := range configured {
		if requested == strings.ToLower(strings.TrimSpace(source)) {
			matches = append(matches, source)
		}
	}
	return matches
}

func configuredByContains(configured []string, token string) []string {
	matches := make([]string, 0, len(configured))
	for _, source := range configured {
		if strings.Contains(strings.ToLower(strings.TrimSpace(source)), token) {
			matches = append(matches, source)
		}
	}
	return matches
}

func parseSince(raw string, fallback int64) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return v, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err == nil {
		return t.UnixMilli(), nil
	}
	return fallback, fmt.Errorf("invalid since value %q", raw)
}

func clampInt(raw string, def, min, max int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < min {
		return def
	}
	if v > max {
		return max
	}
	return v
}
