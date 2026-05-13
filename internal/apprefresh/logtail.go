// Package apprefresh collects and parses dashboard-facing log entries.
package apprefresh

import (
	"bufio"
	"cmp"
	"container/heap"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

type LogRecord struct {
	Source      string    `json:"source,omitempty"`
	SeenAt      string    `json:"seenAt,omitempty"`
	TimestampMs int64     `json:"timestamp,omitempty"`
	Severity    string    `json:"severity,omitempty"`
	Message     string    `json:"message,omitempty"`
	Line        string    `json:"line,omitempty"`
	Raw         string    `json:"raw,omitempty"`
	Timestamp   time.Time `json:"timestampTime,omitzero"`
}

var (
	reGatewayLine = regexp.MustCompile(`^\s*([0-9]{4}-[0-9]{2}-[0-9]{2}[T ][0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?(?:Z|[+\-][0-9]{2}:[0-9]{2})?)\s+(?:\[([^\]]+)\])?\s*(?:\[([^\]]+)\]\s*)?(.*)$`)
	reUUID        = regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	reIDTokens    = regexp.MustCompile(`\b(?:id|session|task|job|trace|request|pid)[:=]?\s*[0-9a-f-]+\b`)
	reNumeric     = regexp.MustCompile(`\b\d+\b`)
	reLogPrefix   = regexp.MustCompile(`^\s*([0-9]{4}-[0-9]{2}-[0-9]{2}[T ][0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?(?:Z|[+\-][0-9]{2}:[0-9]{2})?)`)
)

const (
	readTailMaxLineBytes = 1024 * 1024
)

var (
	errorTokens = map[string]struct{}{
		"error":    {},
		"panic":    {},
		"fatal":    {},
		"segfault": {},
	}
	warnTokens = map[string]struct{}{
		"warn":        {},
		"warning":     {},
		"missing":     {},
		"stale":       {},
		"timeout":     {},
		"unavailable": {},
	}
	debugTokens = map[string]struct{}{
		"debug": {},
	}
)

// ReadMergedLogs tails and parses log sources, returning entries in oldest-to-newest order.
func ReadMergedLogs(openclawPath string, sources []string, globalLimit int) ([]LogRecord, error) {
	if globalLimit <= 0 {
		return nil, nil
	}
	if len(sources) == 0 {
		return nil, nil
	}

	perSourceRecords := make([][]LogRecord, 0, len(sources))
	for _, source := range sources {
		path, ok := resolveLogPath(openclawPath, source)
		if !ok {
			continue
		}

		stat, err := os.Stat(path)
		if err != nil {
			continue
		}

		lines, err := readTailLines(path, globalLimit)
		if err != nil {
			continue
		}

		sourceRecords := make([]LogRecord, 0, len(lines))
		for _, line := range lines {
			record, ok := parseLogLine(line, path, stat.ModTime())
			if !ok {
				continue
			}
			record.Source = source
			record.Raw = line
			if record.TimestampMs == 0 {
				record.TimestampMs = stat.ModTime().UnixMilli()
			}
			sourceRecords = append(sourceRecords, record)
		}
		if len(sourceRecords) > 0 {
			perSourceRecords = append(perSourceRecords, sourceRecords)
		}
	}

	return mergeLatestRecords(perSourceRecords, globalLimit), nil
}

func mergeLatestRecords(perSourceRecords [][]LogRecord, globalLimit int) []LogRecord {
	if len(perSourceRecords) == 0 || globalLimit <= 0 {
		return nil
	}

	h := make(logRecordCursorHeap, 0, len(perSourceRecords))
	for sourceIdx, records := range perSourceRecords {
		last := len(records) - 1
		if last >= 0 {
			h = append(h, logRecordCursor{
				sourceIdx: sourceIdx,
				recordIdx: last,
				record:    records[last],
			})
		}
	}
	heap.Init(&h)

	out := make([]LogRecord, 0, min(globalLimit, len(perSourceRecords)))
	for len(out) < globalLimit && h.Len() > 0 {
		cursor := heap.Pop(&h).(logRecordCursor)
		out = append(out, cursor.record)
		if nextIdx := cursor.recordIdx - 1; nextIdx >= 0 {
			nextRecord := perSourceRecords[cursor.sourceIdx][nextIdx]
			heap.Push(&h, logRecordCursor{
				sourceIdx: cursor.sourceIdx,
				recordIdx: nextIdx,
				record:    nextRecord,
			})
		}
	}

	slices.Reverse(out)
	return out
}

type logRecordCursor struct {
	sourceIdx int
	recordIdx int
	record    LogRecord
}

type logRecordCursorHeap []logRecordCursor

func (h logRecordCursorHeap) Len() int { return len(h) }

func (h logRecordCursorHeap) Less(i, j int) bool {
	return compareLogRecords(h[i].record, h[j].record) > 0
}

func (h logRecordCursorHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *logRecordCursorHeap) Push(x any) {
	*h = append(*h, x.(logRecordCursor))
}

func (h *logRecordCursorHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

func parseLogLine(line string, path string, fallback time.Time) (LogRecord, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return LogRecord{}, false
	}

	ext := strings.ToLower(filepath.Ext(path))
	var record LogRecord
	if ext == ".jsonl" {
		var ok bool
		record, ok = parseJSONLLine(trimmed, fallback)
		if !ok {
			return LogRecord{}, false
		}
	} else {
		record = parsePlainLine(trimmed, fallback)
		record.Timestamp = fallbackIfMissing(record.Timestamp, fallback)
		if record.Timestamp.IsZero() {
			record.Timestamp = fallback
			if !fallback.IsZero() {
				record.SeenAt = fallback.Format(time.RFC3339Nano)
			}
		}
	}
	if record.TimestampMs == 0 && !record.Timestamp.IsZero() {
		record.TimestampMs = record.Timestamp.UnixMilli()
	}
	return record, true
}

func parseJSONLLine(line string, fallback time.Time) (LogRecord, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return parsePlainLine(line, fallback), true
	}

	ts, seen := ParseLogTimestamp(
		getString(payload["timestamp"]),
		getString(payload["time"]),
		getString(payload["ts"]),
	)
	ts = fallbackIfMissing(ts, fallback)
	if ts.IsZero() {
		ts = fallback
	}

	msg := pickLogMessage(payload)
	if msg == "" {
		msg = line
	}
	severity := inferSeverity(
		pickFirst(payload, "severity", "level", "log", "type", "status"),
		msg,
	)

	if seen == "" && !ts.IsZero() {
		seen = ts.Format(time.RFC3339Nano)
	}

	return LogRecord{
		SeenAt:    seen,
		Timestamp: ts,
		Severity:  severity,
		Message:   msg,
	}, true
}

func parsePlainLine(line string, fallback time.Time) LogRecord {
	ts, seen := ParseLogTimestamp(line)
	ts = fallbackIfMissing(ts, fallback)
	if ts.IsZero() {
		ts = fallback
		if !fallback.IsZero() {
			seen = fallback.Format(time.RFC3339Nano)
		}
	}

	msg := line
	component := ""
	if m := reGatewayLine.FindStringSubmatch(line); len(m) >= 4 {
		if m[2] != "" {
			component = m[2]
		}
		msg = strings.TrimSpace(m[len(m)-1])
	}
	return LogRecord{
		SeenAt:    seen,
		Timestamp: ts,
		Severity:  classifySeverity(msg, component),
		Message:   msg,
	}
}

// ParseLogTimestamp tries multiple timestamp formats and returns the first valid match.
func ParseLogTimestamp(candidates ...string) (time.Time, string) {
	if len(candidates) == 0 {
		return time.Time{}, ""
	}

	for _, candidate := range candidates {
		c := strings.TrimSpace(candidate)
		if c == "" {
			continue
		}
		for _, layout := range []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05.999999999",
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05.999999999",
			"2006-01-02T15:04:05",
			"2006-01-02T15:04:05Z",
		} {
			if parsed, err := time.Parse(layout, c); err == nil {
				return parsed, c
			}
		}
	}
	for _, candidate := range candidates {
		c := strings.TrimSpace(candidate)
		if c == "" {
			continue
		}
		if match := reLogPrefix.FindStringSubmatch(c); len(match) >= 2 {
			return ParseLogTimestamp(match[1])
		}
	}
	return time.Time{}, ""
}

func classifySeverity(line, component string) string {
	if strings.Contains(strings.ToLower(component), "err") {
		return "error"
	}
	low := strings.ToLower(line)
	tokens := strings.FieldsFunc(low, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	severity := "info"
	for i, tok := range tokens {
		negated := i > 0 && (tokens[i-1] == "no" || tokens[i-1] == "not")
		if _, ok := errorTokens[tok]; ok {
			if negated {
				continue
			}
			return "error"
		}
		if _, ok := warnTokens[tok]; ok {
			if negated {
				continue
			}
			severity = "warn"
			continue
		}
		if severity == "info" {
			if _, ok := debugTokens[tok]; ok {
				severity = "debug"
			}
		}
	}
	return severity
}

func inferSeverity(raw string, line string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "err", "error", "fatal", "panic", "stale", "missing", "unavailable", "timeout":
		return "error"
	case "warn", "warning":
		return "warn"
	case "debug":
		return "debug"
	}
	return classifySeverity(line, "")
}

func pickFirst(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := obj[key]; ok {
			if s := getString(value); s != "" {
				return s
			}
		}
	}
	return ""
}

func pickLogMessage(obj map[string]any) string {
	keys := []string{"message", "msg", "text", "error", "err", "event", "details", "content"}
	for _, key := range keys {
		if value, ok := obj[key]; ok {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
			if nested, ok := value.(map[string]any); ok {
				for _, nestedKey := range []string{"content", "text", "message"} {
					if nestedValue, nestedOk := nested[nestedKey]; nestedOk {
						if s, ok := nestedValue.(string); ok && strings.TrimSpace(s) != "" {
							return strings.TrimSpace(s)
						}
					}
				}
			}
		}
	}
	return ""
}

func getString(v any) string {
	if v == nil {
		return ""
	}
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	}
	return ""
}

// NormalizeErrorSignature removes volatile values so similar failures can be grouped.
func NormalizeErrorSignature(msg string) string {
	v := strings.ToLower(msg)
	v = reUUID.ReplaceAllString(v, "<uuid>")
	v = reIDTokens.ReplaceAllString(v, "<id>")
	v = reNumeric.ReplaceAllString(v, "<n>")
	v = reLogPrefix.ReplaceAllString(v, "<ts>")
	v = strings.ReplaceAll(v, "\t", " ")
	v = strings.TrimSpace(v)
	for strings.Contains(v, "  ") {
		v = strings.ReplaceAll(v, "  ", " ")
	}
	return v
}

func readTailLines(path string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Size() <= 0 {
		return nil, nil
	}

	scanner := bufio.NewScanner(f)
	// Allow scanner buffer to grow up to 2x the line cap so we can capture
	// over-long lines and truncate them, rather than failing with ErrTooLong.
	scanner.Buffer(make([]byte, 64*1024), 2*readTailMaxLineBytes)

	ring := make([]string, limit)
	var count, write int
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		if len(line) > readTailMaxLineBytes {
			line = line[:readTailMaxLineBytes]
		}
		ring[write] = line
		write = (write + 1) % limit
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	n := count
	if n > limit {
		n = limit
	}
	out := make([]string, 0, n)
	start := 0
	if count > limit {
		start = write
	}
	for i := 0; i < n; i++ {
		out = append(out, ring[(start+i)%limit])
	}
	return out, nil
}

func ResolveLogPath(openclawPath, source string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(source)))
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", false
	}
	return filepath.Join(openclawPath, clean), true
}

func resolveLogPath(openclawPath, source string) (string, bool) {
	return ResolveLogPath(openclawPath, source)
}

func GetLogRuntimeConfig(cfg appconfig.Config) map[string]any {
	return map[string]any{
		"logTailLines":         cfg.Logs.TailLines,
		"logFastRefreshMs":     cfg.Logs.FastRefreshMs,
		"errorWindowHours":     cfg.Logs.ErrorWindowHours,
		"maxErrorSignatures":   cfg.Logs.MaxErrorSignatures,
		"logSources":           GetEffectiveLogSources(cfg),
		"logRefreshIntervalMs": cfg.Refresh.IntervalSeconds * 1000,
	}
}

func GetEffectiveLogSources(cfg appconfig.Config) []string {
	return append([]string(nil), cfg.Logs.Sources...)
}

func fallbackIfMissing(ts time.Time, fallback time.Time) time.Time {
	if ts.IsZero() && !fallback.IsZero() {
		return fallback
	}
	return ts
}

func compareLogRecords(a, b LogRecord) int {
	if a.Timestamp.IsZero() {
		if b.Timestamp.IsZero() {
			if c := cmp.Compare(a.Source, b.Source); c != 0 {
				return c
			}
			return cmp.Compare(a.Raw, b.Raw)
		}
		return -1
	}
	if b.Timestamp.IsZero() {
		return 1
	}
	if !a.Timestamp.Equal(b.Timestamp) {
		if a.Timestamp.Before(b.Timestamp) {
			return -1
		}
		return 1
	}
	if c := cmp.Compare(a.Source, b.Source); c != 0 {
		return c
	}
	return cmp.Compare(a.Raw, b.Raw)
}
