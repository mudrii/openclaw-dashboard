package appserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

type logRecord struct {
	Source      string
	SeenAt      string
	TimestampMs int64
	Severity    string
	Message     string
	Line        string
	order       int
	timestamp   time.Time
}

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
}

var (
	reLogPrefixTs = regexp.MustCompile(`^\s*([0-9]{4}-[0-9]{2}-[0-9]{2}[T ][0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?(?:Z|[+\-][0-9]{2}:[0-9]{2})?)`)
	reUUID       = regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	reIDTokens   = regexp.MustCompile(`\b(?:id|session|task|job|trace|request|pid)[:=]?\s*[0-9a-f-]+\b`)
	reNumeric    = regexp.MustCompile(`\b\d+\b`)
)

const (
	logLimitDefault        = 200
	logLimitMax            = 1000
	logFastRefreshDefaultMs = 3000
	errorLimitDefault      = 1000
	errorWindowHoursDefault = 24
	errorWindowHoursMax     = 168
)

// handleLogs serves merged and sorted tail lines across configured log sources.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Logs.Enabled {
		s.sendJSONRaw(w, r, http.StatusServiceUnavailable, []byte(`{"error":"logs disabled"}`))
		return
	}

	query := r.URL.Query()
	limit := clampInt(query.Get("limit"), logLimitDefault, 1, logLimitMax)
	sources := resolveSources(query.Get("source"), s.cfg.Logs.Sources)
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
		s.setCORSHeaders(w, r)
		s.sendJSONRaw(w, r, http.StatusInternalServerError, []byte(`{"error":"failed to read logs"}`))
		return
	}

	if sinceMs > 0 {
		threshold := time.UnixMilli(sinceMs)
		filtered := make([]logRecord, 0, len(entries))
		for _, entry := range entries {
			if entry.timestamp.IsZero() || !entry.timestamp.Before(threshold) {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}

	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	payload := logsResponse{
		OK:       true,
		Limit:    limit,
		Count:    len(entries),
		Sources:  sourceList,
		SinceMs:  sinceMs,
		FastMode: logFastRefreshDefaultMs,
		Entries:  make([]logEntry, 0, len(entries)),
	}
	for _, entry := range entries {
		payload.Entries = append(payload.Entries, logEntry{
			Source:   entry.Source,
			SeenAt:   entry.SeenAt,
			Severity: entry.Severity,
			Line:     entry.Line,
			Message:  entry.Message,
			Raw:      entry.Line,
			Ts:       entry.TimestampMs,
		})
	}

	s.sendJSON(w, r, http.StatusOK, payload)
}

// handleErrors returns deduplicated warning/error signatures from recent logs.
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
	windowHours := clampInt(query.Get("windowHours"), errorWindowHoursDefault, 1, errorWindowHoursMax)
	sources := resolveSources(query.Get("source"), s.cfg.Logs.Sources)
	rawSourceList := append([]string(nil), sources...)

	entries, err := s.readMergedLogs(sources, errorLimitDefault)
	if err != nil {
		s.setCORSHeaders(w, r)
		s.sendJSONRaw(w, r, http.StatusInternalServerError, []byte(`{"error":"failed to read logs"}`))
		return
	}

	windowStart := time.Now().Add(-time.Duration(windowHours) * time.Hour)
	itemsBySig := make(map[string]*errorFeedItem, s.cfg.Logs.MaxErrorSignatures)
	for _, entry := range entries {
		if entry.timestamp.IsZero() || entry.timestamp.Before(windowStart) {
			continue
		}
		if entry.Severity != "warn" && entry.Severity != "error" {
			continue
		}

		sig := entry.Source + "|" + normalizeErrorSignature(entry.Message)
		if len(itemsBySig) >= s.cfg.Logs.MaxErrorSignatures {
			if _, exists := itemsBySig[sig]; !exists {
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
		sort.Slice(items, func(i, j int) bool {
			if items[i].Count != items[j].Count {
				return items[i].Count > items[j].Count
			}
			return items[i].LastSeen > items[j].LastSeen
		})
	default:
		sort.Slice(items, func(i, j int) bool {
			return items[i].LastSeen > items[j].LastSeen
		})
	}

	if len(items) > limit {
		items = items[:limit]
	}

	s.sendJSON(w, r, http.StatusOK, errorsResponse{
		OK:      true,
		Window:  windowHours,
		Count:   len(items),
		Sort:    sortMode,
		Limit:   limit,
		Items:   items,
		Sources: rawSourceList,
	})
}

func (s *Server) readMergedLogs(sources []string, globalLimit int) ([]logRecord, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	perSourceLimit := clampInt(fmt.Sprintf("%d", clampIntToSources(globalLimit, len(sources))), 1, 1, logLimitMax)
	records := make([]logRecord, 0)
	order := 0

	for _, source := range sources {
		path, ok := resolveLogPath(s.openclawPath, source)
		if !ok {
			continue
		}
		lines, err := readTail(path, perSourceLimit)
		if err != nil {
			continue
		}
		for _, line := range lines {
			order++
			ts, seenAt := parseLogTimestamp(line)
			records = append(records, logRecord{
				Source:      source,
				SeenAt:      seenAt,
				TimestampMs: ts.UnixMilli(),
				Severity:    classifySeverity(line, source),
				Message:     strings.TrimSpace(line),
				Line:        line,
				order:       order,
				timestamp:   ts,
			})
		}
	}

	sort.SliceStable(records, func(i, j int) bool {
		ti := records[i].timestamp
		tj := records[j].timestamp
		if !ti.Equal(tj) {
			if ti.IsZero() {
				return true
			}
			if tj.IsZero() {
				return false
			}
			return ti.Before(tj)
		}
		return records[i].order < records[j].order
	})

	if len(records) > globalLimit {
		records = records[len(records)-globalLimit:]
	}
	return records, nil
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

func resolveLogPath(openclawPath, source string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(source)))
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", false
	}
	return filepath.Join(openclawPath, clean), true
}

func readTail(path string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	out := make([]string, 0, limit)
	for sc.Scan() {
		out = append(out, sc.Text())
		if len(out) > limit {
			out = out[1:]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan log file %s: %w", path, err)
	}
	return out, nil
}

func parseLogTimestamp(line string) (time.Time, string) {
	match := reLogPrefixTs.FindStringSubmatch(line)
	if len(match) < 2 {
		return time.Time{}, ""
	}
	candidate := match[1]
	if parsed, err := time.Parse(time.RFC3339Nano, candidate); err == nil {
		return parsed, candidate
	}
	if parsed, err := time.Parse(time.RFC3339, candidate); err == nil {
		return parsed, candidate
	}
	if parsed, err := time.Parse("2006-01-02 15:04:05.999999999", candidate); err == nil {
		return parsed, candidate
	}
	if parsed, err := time.Parse("2006-01-02 15:04:05", candidate); err == nil {
		return parsed, candidate
	}
	return time.Time{}, candidate
}

func classifySeverity(line, source string) string {
	if strings.Contains(strings.ToLower(source), "err") {
		return "error"
	}
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "panic") || strings.Contains(lower, "fatal") || strings.Contains(lower, "segfault"):
		return "error"
	case strings.Contains(lower, "warn") || strings.Contains(lower, "warning"):
		return "warn"
	case strings.Contains(lower, "debug"):
		return "debug"
	default:
		return "info"
	}
}

func normalizeErrorSignature(msg string) string {
	v := strings.ToLower(msg)
	v = reUUID.ReplaceAllString(v, "<uuid>")
	v = reIDTokens.ReplaceAllString(v, "<id>")
	v = reNumeric.ReplaceAllString(v, "<n>")
	v = reLogPrefixTs.ReplaceAllString(v, "<ts>")
	v = strings.ReplaceAll(v, "\t", " ")
	v = strings.TrimSpace(v)
	for strings.Contains(v, "  ") {
		v = strings.ReplaceAll(v, "  ", " ")
	}
	return v
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

func clampIntToSources(globalLimit, sourceCount int) int {
	if sourceCount <= 0 {
		return 0
	}
	perSource := int(math.Ceil(float64(globalLimit) / float64(sourceCount)))
	if perSource < 1 {
		perSource = 1
	}
	return perSource
}
