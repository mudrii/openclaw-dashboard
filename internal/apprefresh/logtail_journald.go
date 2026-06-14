package apprefresh

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// defaultSystemdUnit is the systemd --user unit openclaw's gateway runs under
// when no override is configured (daemon/constants.ts).
const defaultSystemdUnit = "openclaw-gateway"

// journaldEnabled reports whether the journald fallback should be attempted.
// It is a func var so tests can force-enable the Linux-only path on any host.
var journaldEnabled = func() bool { return runtime.GOOS == "linux" }

// journaldRunner executes `journalctl --user -u <unit> -o json` and
// returns the raw output. Stubbed in tests. A missing binary or any other
// failure is returned as an error and collapses to an empty source upstream.
var journaldRunner = func(ctx context.Context, unit string, lines int) ([]byte, error) {
	return exec.CommandContext(ctx, "journalctl",
		"--user", "-u", systemdUnitName(unit),
		"-o", "json", "--no-pager",
		"-n", strconv.Itoa(lines),
	).Output()
}

// ResolveSystemdUnit resolves the gateway's systemd unit name. Precedence:
// the OPENCLAW_SYSTEMD_UNIT env var (used verbatim) > the configured unit >
// the default "openclaw-gateway". For the two non-override forms, a non-empty
// OPENCLAW_PROFILE is appended as a "-<profile>" suffix, mirroring openclaw's
// per-profile unit naming.
func ResolveSystemdUnit(configUnit string) string {
	if env := strings.TrimSpace(os.Getenv("OPENCLAW_SYSTEMD_UNIT")); env != "" {
		return env
	}
	unit := strings.TrimSpace(configUnit)
	if unit == "" {
		unit = defaultSystemdUnit
	}
	if profile := strings.TrimSpace(os.Getenv("OPENCLAW_PROFILE")); profile != "" {
		unit += "-" + profile
	}
	return unit
}

func systemdUnitName(unit string) string {
	unit = strings.TrimSpace(unit)
	if unit == "" {
		unit = defaultSystemdUnit
	}
	if strings.HasSuffix(unit, ".service") {
		return unit
	}
	return unit + ".service"
}

// collectJournaldRecords runs the journald collector for one source and returns
// the parsed records (Source/Raw populated), or nil on any runner error or when
// no parseable lines are produced.
func collectJournaldRecords(ctx context.Context, unit, source string, limit int) []LogRecord {
	out, err := journaldRunner(ctx, unit, limit)
	if err != nil {
		return nil
	}
	var records []LogRecord
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		record, ok := parseJournaldLine(line)
		if !ok {
			continue
		}
		record.Source = source
		record.Raw = line
		records = append(records, record)
	}
	return records
}

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

// decodeJournaldMessage extracts the MESSAGE field, which journald emits either
// as a plain string or — when the payload is not valid UTF-8 — as a JSON array
// of byte values. Both forms are decoded to a trimmed string; anything else
// yields "".
func decodeJournaldMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var bytesArr []byte
	if err := json.Unmarshal(raw, &bytesArr); err == nil {
		return strings.TrimSpace(string(bytesArr))
	}
	return ""
}

// parseJournaldLine parses a single `journalctl -o json` object into a
// LogRecord. It maps PRIORITY → severity (falling back to message-based
// classification when PRIORITY is absent or out of range) and the microsecond
// __REALTIME_TIMESTAMP → time. It reports ok=false for malformed JSON or a
// missing MESSAGE so the caller skips the entry. The Source and Raw fields are
// left for the caller to populate, mirroring ReadMergedLogs's file path.
func parseJournaldLine(line string) (LogRecord, bool) {
	var payload struct {
		Message  json.RawMessage `json:"MESSAGE"`
		Priority string          `json:"PRIORITY"`
		Realtime string          `json:"__REALTIME_TIMESTAMP"`
	}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return LogRecord{}, false
	}
	message := decodeJournaldMessage(payload.Message)
	if message == "" {
		return LogRecord{}, false
	}

	severity, ok := journaldPrioritySeverity(payload.Priority)
	if !ok {
		severity = classifySeverity(message, "")
	}

	var ts time.Time
	if us, err := strconv.ParseInt(payload.Realtime, 10, 64); err == nil && us > 0 {
		ts = time.UnixMicro(us)
	}

	rec := LogRecord{
		Severity:  severity,
		Message:   message,
		Timestamp: ts,
	}
	if !ts.IsZero() {
		rec.TimestampMs = ts.UnixMilli()
		rec.SeenAt = ts.Format(time.RFC3339Nano)
	}
	return rec, true
}
