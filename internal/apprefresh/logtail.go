package apprefresh

import (
	"cmp"
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

type LogRecord struct {
	Source      string    `json:"source,omitempty"`
	SeenAt      string    `json:"seenAt,omitempty"`
	TimestampMs int64     `json:"timestamp"`
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
	// readTailChunkBytes is the backwards read step used to find line starts.
	readTailChunkBytes = 64 * 1024
	// defaultLogRefreshIntervalMs is the normal log-panel poll interval,
	// matching the frontend LogTail.normalMs default. It is deliberately not
	// derived from the data refresh debounce.
	defaultLogRefreshIntervalMs = 15000
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

// ReadMergedLogs tails and parses log sources, returning entries in
// oldest-to-newest order. The systemd unit for the journald fallback is
// resolved from the environment (OPENCLAW_SYSTEMD_UNIT / OPENCLAW_PROFILE) and
// the package default; callers with a configured unit should use
// ReadMergedLogsWithUnit.
func ReadMergedLogs(openclawPath string, sources []string, globalLimit int) ([]LogRecord, error) {
	return ReadMergedLogsWithUnit(openclawPath, sources, globalLimit, ResolveSystemdUnit(""))
}

// ReadMergedLogsWithUnit is ReadMergedLogs with an explicit systemd unit name
// for the Linux journald fallback. On Linux, when a source has no log file on
// disk, gateway output is read from journald (systemd emits no log file) so the
// Logs panel and error alerts still populate. On other platforms the journald
// path is skipped entirely.
func ReadMergedLogsWithUnit(openclawPath string, sources []string, globalLimit int, systemdUnit string) ([]LogRecord, error) {
	return ReadMergedLogsWithUnitContext(context.Background(), openclawPath, sources, globalLimit, systemdUnit)
}

// ReadMergedLogsWithUnitContext is ReadMergedLogsWithUnit with caller
// cancellation support for journald collection.
func ReadMergedLogsWithUnitContext(ctx context.Context, openclawPath string, sources []string, globalLimit int, systemdUnit string) ([]LogRecord, error) {
	if globalLimit <= 0 {
		return nil, nil
	}
	if len(sources) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	perSourceRecords := make([][]LogRecord, 0, len(sources))
	journaldUsed := false
	for _, source := range sources {
		candidates := candidateLogPaths(openclawPath, source)
		sourceRecords := make([]LogRecord, 0)
		// Fallback roots may hold the same file (or a migrated copy); a record
		// already read from an earlier candidate is dropped, but repeated lines
		// within one file are genuine and kept.
		seen := map[logRecordDedupKey]struct{}{}
		for _, path := range candidates {
			stat, err := os.Stat(path)
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					slog.Warn("[dashboard] log source stat failed", "source", source, "path", path, "error", err)
				}
				continue
			}
			lines, err := readTailLines(path, globalLimit)
			if err != nil {
				slog.Warn("[dashboard] log source read failed", "source", source, "path", path, "error", err)
				continue
			}
			fileRecords := parseLogFileLines(lines, path, source, stat.ModTime())
			for _, record := range fileRecords {
				if _, dup := seen[record.dedupKey()]; !dup {
					sourceRecords = append(sourceRecords, record)
				}
			}
			for _, record := range fileRecords {
				seen[record.dedupKey()] = struct{}{}
			}
		}
		// Linux journald fallback: when no log file exists for this source,
		// synthesize records from journalctl (systemd gateway logs have no
		// file to tail). Skipped on non-Linux and when a file was found.
		if len(sourceRecords) == 0 && journaldEnabled() && !journaldUsed && isGatewayLogSource(source) {
			jctx, jcancel := context.WithTimeout(ctx, 5*time.Second)
			sourceRecords = append(sourceRecords, collectJournaldRecords(jctx, systemdUnit, source, globalLimit)...)
			jcancel()
			journaldUsed = true
		}
		if len(sourceRecords) > 0 {
			// Stable: equal timestamps keep candidate-file then line order, so
			// multi-line entries (stack traces) stay in the order written.
			slices.SortStableFunc(sourceRecords, func(a, b LogRecord) int {
				return a.Timestamp.Compare(b.Timestamp)
			})
			perSourceRecords = append(perSourceRecords, sourceRecords)
		}
	}

	return mergeLatestRecords(perSourceRecords, globalLimit), nil
}

func isGatewayLogSource(source string) bool {
	name := strings.ToLower(filepath.Base(source))
	return strings.Contains(name, "gateway")
}

type logRecordDedupKey struct {
	source      string
	timestampMs int64
	raw         string
}

func (r LogRecord) dedupKey() logRecordDedupKey {
	return logRecordDedupKey{source: r.Source, timestampMs: r.TimestampMs, raw: r.Raw}
}

// parseLogFileLines turns one file's tail into records in file order. Lines
// without their own timestamp (stack traces, wrapped output) inherit the
// previous timestamped line's time; lines before the first timestamp take
// the first one found, and a file with none falls back to its mtime.
func parseLogFileLines(lines []string, path, source string, modTime time.Time) []LogRecord {
	records := make([]LogRecord, 0, len(lines))
	var firstTs time.Time
	for _, line := range lines {
		// Parse before redacting: redaction rewrites JSON values (e.g.
		// "hasToken":false) and would make structured lines unparseable.
		record, ok := parseLogLine(line, path, time.Time{})
		if !ok {
			continue
		}
		if firstTs.IsZero() {
			firstTs = record.Timestamp
		}
		record.Source = source
		record.Raw = appopenclaw.Redact(line)
		record.Message = appopenclaw.Redact(record.Message)
		record.Line = appopenclaw.Redact(record.Line)
		records = append(records, record)
	}
	if firstTs.IsZero() {
		firstTs = modTime
	}
	last := firstTs
	for i := range records {
		if records[i].Timestamp.IsZero() {
			records[i].Timestamp = last
			records[i].TimestampMs = last.UnixMilli()
			records[i].SeenAt = last.Format(time.RFC3339Nano)
			continue
		}
		last = records[i].Timestamp
	}
	return records
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

// ParseLogTimestamp tries multiple timestamp formats and returns the first
// valid match. TZ-less layouts are interpreted in the process's local
// timezone — gateway logs are emitted in local time on the same host as the
// dashboard, so anchoring on time.Local keeps chart buckets and alert
// windows aligned. RFC3339 layouts carry their own offset in the string and
// are unaffected by the location argument.
func ParseLogTimestamp(candidates ...string) (time.Time, string) {
	if len(candidates) == 0 {
		return time.Time{}, ""
	}

	for _, candidate := range candidates {
		c := strings.TrimSpace(candidate)
		if c == "" || !onlyTimestampBytes(c) {
			continue
		}
		for _, layout := range logTimestampLayouts {
			if parsed, err := time.ParseInLocation(layout, c, time.Local); err == nil {
				return parsed, c
			}
		}
	}
	for _, candidate := range candidates {
		c := strings.TrimSpace(candidate)
		if c == "" {
			continue
		}
		// Recurse only when the extracted prefix is strictly shorter than the
		// candidate — i.e. real progress was made by stripping trailing content.
		// When c is already a bare timestamp the layout loop above could not
		// parse (e.g. a syntactically-shaped but invalid "2026-13-45T25:61:99"),
		// match[1] == c and recursing would loop forever → stack overflow.
		if match := reLogPrefix.FindStringSubmatch(c); len(match) >= 2 && match[1] != c {
			return ParseLogTimestamp(match[1])
		}
	}
	return time.Time{}, ""
}

// logTimestampLayouts are the whole-string formats ParseLogTimestamp accepts,
// tried in order.
var logTimestampLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05Z",
}

// onlyTimestampBytes reports whether s consists solely of bytes some
// logTimestampLayouts entry can match: digits, the '-' ':' 'T' ' ' separators,
// '.' or ',' before fractional seconds, and the 'Z' '+' '-' zone designators.
// Any other byte makes every layout fail, so a whole log line is rejected
// without paying for (and allocating) one failed time.Parse per layout.
func onlyTimestampBytes(s string) bool {
	for i := range len(s) {
		switch c := s[i]; {
		case '0' <= c && c <= '9':
		case c == '-', c == ':', c == 'T', c == ' ', c == '.', c == ',', c == 'Z', c == '+':
		default:
			return false
		}
	}
	return true
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
	case "err", "error", "fatal", "panic":
		return "error"
	case "warn", "warning", "stale", "missing", "unavailable", "timeout":
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
	lines, err := readTailFrom(f, stat.Size(), limit)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return lines, nil
}

// readTailFrom returns the last limit non-empty lines of the first size bytes
// of r, oldest first. It walks backwards from EOF in fixed chunks, so the
// bytes read are bounded by the tail it returns rather than the file size.
// Lines longer than readTailMaxLineBytes are truncated rune-safely to their
// prefix; a trailing "\r" is dropped.
func readTailFrom(r io.ReaderAt, size int64, limit int) ([]string, error) {
	if limit <= 0 || size <= 0 {
		return nil, nil
	}
	out := make([]string, 0, min(limit, 64))
	chunk := make([]byte, min(size, readTailChunkBytes))
	lineEnd := size // exclusive end of the line whose start is not yet found
	pos := size     // bytes before pos are not yet scanned
	emit := func(buf []byte, bufStart, start int64) error {
		line, err := readTailLine(r, buf, bufStart, start, lineEnd)
		if err != nil {
			return err
		}
		if line != "" {
			out = append(out, line)
		}
		return nil
	}
	for pos > 0 && len(out) < limit {
		n := min(int64(len(chunk)), pos)
		pos -= n
		buf := chunk[:n]
		if _, err := r.ReadAt(buf, pos); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		for i := len(buf) - 1; i >= 0 && len(out) < limit; i-- {
			if buf[i] != '\n' {
				continue
			}
			if err := emit(buf, pos, pos+int64(i)+1); err != nil {
				return nil, err
			}
			lineEnd = pos + int64(i)
		}
		if pos == 0 && len(out) < limit {
			if err := emit(buf, pos, 0); err != nil {
				return nil, err
			}
		}
	}
	slices.Reverse(out)
	return out, nil
}

// readTailLine returns the capped content of the line [start, end). It slices
// buf (which begins at file offset bufStart) when the line lies inside it and
// otherwise reads just the capped prefix from r.
func readTailLine(r io.ReaderAt, buf []byte, bufStart, start, end int64) (string, error) {
	// One byte beyond the cap distinguishes "fits after dropping \r" from
	// "must be truncated", matching the forward scanner's semantics.
	length := min(end-start, readTailMaxLineBytes+1)
	if length <= 0 {
		return "", nil
	}
	var raw []byte
	if start >= bufStart && start+length <= bufStart+int64(len(buf)) {
		raw = buf[start-bufStart : start-bufStart+length]
	} else {
		raw = make([]byte, length)
		if _, err := r.ReadAt(raw, start); err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
	}
	line := string(raw)
	if end-start == length {
		line = strings.TrimSuffix(line, "\r")
	}
	return truncateBytes(line, readTailMaxLineBytes), nil
}

func ResolveLogPath(openclawPath, source string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(source)))
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", false
	}
	return filepath.Join(openclawPath, clean), true
}

// logFallbackRootsFunc is overridable so tests can disable home-dir lookup
// and stay hermetic. Production callers must not reassign it.
var (
	logFallbackRootsMu   sync.RWMutex
	logFallbackRootsFunc = defaultLogFallbackRoots
)

// LogFallbackRoots returns directories to search for log files when the primary
// path under openclawPath is missing. OpenClaw 2026.5.18+ writes logs to the
// platform standard log directory, so the dashboard needs to look outside
// ~/.openclaw to find the live file.
func LogFallbackRoots() []string {
	logFallbackRootsMu.RLock()
	fn := logFallbackRootsFunc
	logFallbackRootsMu.RUnlock()
	roots := fn()
	return append([]string(nil), roots...)
}

func defaultLogFallbackRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, "Library", "Logs", "openclaw")}
}

// SetLogFallbackRoots overrides the directory list returned by LogFallbackRoots.
// Pass nil to revert to the platform default. Intended for tests; production
// code should leave this alone.
func SetLogFallbackRoots(fn func() []string) {
	logFallbackRootsMu.Lock()
	defer logFallbackRootsMu.Unlock()
	if fn == nil {
		logFallbackRootsFunc = defaultLogFallbackRoots
		return
	}
	logFallbackRootsFunc = fn
}

// candidateLogPaths returns paths to try for a given source, in priority order:
// the primary path under openclawPath followed by each fallback root from
// LogFallbackRoots(). Callers should read every existing candidate so log
// data is merged when OpenClaw rotates or migrates log locations (e.g.
// 2026.5.18+ moved gateway logs from ~/.openclaw/logs/ to
// ~/Library/Logs/openclaw/).
func candidateLogPaths(openclawPath, source string) []string {
	out := make([]string, 0, 2)
	if path, ok := ResolveLogPath(openclawPath, source); ok {
		out = append(out, path)
	}
	base := strings.TrimPrefix(filepath.Clean(filepath.FromSlash(source)), "logs"+string(filepath.Separator))
	if base == "" || filepath.IsAbs(base) || strings.HasPrefix(base, "..") {
		return out
	}
	for _, root := range LogFallbackRoots() {
		out = append(out, filepath.Join(root, base))
	}
	return out
}

func GetLogRuntimeConfig(cfg appconfig.Config) map[string]any {
	return map[string]any{
		"logTailLines":         cfg.Logs.TailLines,
		"logFastRefreshMs":     cfg.Logs.FastRefreshMs,
		"errorWindowHours":     cfg.Logs.ErrorWindowHours,
		"maxErrorSignatures":   cfg.Logs.MaxErrorSignatures,
		"logSources":           GetEffectiveLogSources(cfg),
		"logRefreshIntervalMs": defaultLogRefreshIntervalMs,
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
