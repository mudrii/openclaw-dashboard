package apprefresh

import (
	"encoding/json"
	"strconv"
	"time"
)

// journaldPrioritySeverity maps a syslog PRIORITY level (0-7, as emitted by
// `journalctl -o json`) to the dashboard severity vocabulary. journald exports
// PRIORITY as a decimal string. Levels 0-3 (emerg/alert/crit/err) collapse to
// "error", 4 (warning) to "warn", 5-6 (notice/info) to "info", 7 to "debug".
func journaldPrioritySeverity(priority string) (string, bool) {
	n, err := strconv.Atoi(priority)
	if err != nil {
		return "", false
	}
	switch {
	case n <= 3:
		return "error", true
	case n == 4:
		return "warn", true
	case n == 7:
		return "debug", true
	case n == 5 || n == 6:
		return "info", true
	default:
		return "", false
	}
}

// parseJournaldLine parses a single `journalctl -o json` object into a
// LogRecord. It maps PRIORITY → severity (falling back to message-based
// classification when PRIORITY is absent or out of range) and the microsecond
// __REALTIME_TIMESTAMP → time. It reports ok=false for malformed JSON or a
// missing MESSAGE so the caller skips the entry. The Source and Raw fields are
// left for the caller to populate, mirroring ReadMergedLogs's file path.
func parseJournaldLine(line string) (LogRecord, bool) {
	var payload struct {
		Message  string `json:"MESSAGE"`
		Priority string `json:"PRIORITY"`
		Realtime string `json:"__REALTIME_TIMESTAMP"`
	}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return LogRecord{}, false
	}
	if payload.Message == "" {
		return LogRecord{}, false
	}

	severity, ok := journaldPrioritySeverity(payload.Priority)
	if !ok {
		severity = classifySeverity(payload.Message, "")
	}

	var ts time.Time
	if us, err := strconv.ParseInt(payload.Realtime, 10, 64); err == nil && us > 0 {
		ts = time.UnixMicro(us)
	}

	rec := LogRecord{
		Severity:  severity,
		Message:   payload.Message,
		Timestamp: ts,
	}
	if !ts.IsZero() {
		rec.TimestampMs = ts.UnixMilli()
		rec.SeenAt = ts.Format(time.RFC3339Nano)
	}
	return rec, true
}
