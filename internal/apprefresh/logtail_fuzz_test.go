package apprefresh

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"
)

var logLineSeeds = []string{
	"2026-03-01T10:00:00Z [gateway] [error] connection refused",
	"2026-03-01 10:00:00.123 [ws] disconnected: timeout",
	"2026-13-45T25:61:99 bogus",
	`{"timestamp":"2026-03-01T10:00:00Z","level":"error","message":"boom"}`,
	`{"time":1700000000,"msg":{"content":"nested"},"status":"stale"}`,
	`{"ts":"2026-03-01T10:00:00.5+02:00","event":"no error here"}`,
	"not an error: warning cleared",
	"panic: runtime error: index out of range",
	"plain text line",
	"   ",
	"",
}

var validSeverities = []string{"error", "warn", "info", "debug"}

// FuzzParseLogLine checks plain and JSONL log parsing never panics and always
// yields a known severity with a timestamp consistent with TimestampMs.
func FuzzParseLogLine(f *testing.F) {
	for _, s := range logLineSeeds {
		f.Add(s, false)
		f.Add(s, true)
	}
	fallback := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, line string, jsonl bool) {
		path := "gateway.log"
		if jsonl {
			path = "gateway.jsonl"
		}
		rec, ok := parseLogLine(line, path, fallback)
		if !ok {
			if strings.TrimSpace(line) != "" && !jsonl {
				t.Fatalf("plain line %q rejected", line)
			}
			return
		}
		if !slices.Contains(validSeverities, rec.Severity) {
			t.Fatalf("severity %q for %q", rec.Severity, line)
		}
		if rec.Timestamp.IsZero() {
			t.Fatalf("zero timestamp despite fallback for %q", line)
		}
		if rec.TimestampMs != rec.Timestamp.UnixMilli() {
			t.Fatalf("TimestampMs %d != Timestamp %v for %q", rec.TimestampMs, rec.Timestamp, line)
		}
		// Whole-file parsing adds redaction on top; it must not panic either.
		_ = parseLogFileLines([]string{line}, path, "gateway", fallback)
	})
}

// FuzzClassifySeverity checks both severity classifiers stay within the known
// set, and that an explicit error level always wins.
func FuzzClassifySeverity(f *testing.F) {
	for _, s := range logLineSeeds {
		f.Add(s, "", "")
	}
	f.Add("fine", "err-handler", "")
	f.Add("no error", "", "warning")
	f.Add("x", "", "FATAL")
	f.Fuzz(func(t *testing.T, line, component, raw string) {
		if got := classifySeverity(line, component); !slices.Contains(validSeverities, got) {
			t.Fatalf("classifySeverity(%q, %q) = %q", line, component, got)
		}
		got := inferSeverity(raw, line)
		if !slices.Contains(validSeverities, got) {
			t.Fatalf("inferSeverity(%q, %q) = %q", raw, line, got)
		}
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "err", "error", "fatal", "panic":
			if got != "error" {
				t.Fatalf("inferSeverity(%q, %q) = %q, want error", raw, line, got)
			}
		}
	})
}

// FuzzNormalizeErrorSignature checks signature normalization is idempotent
// (grouping an already-normalized signature must not split it) and leaves no
// tabs or doubled spaces.
func FuzzNormalizeErrorSignature(f *testing.F) {
	for _, s := range logLineSeeds {
		f.Add(s)
	}
	f.Add("request id=4f3a-99 failed after 1200 ms for session 1234abcd")
	f.Add("uuid 123e4567-e89b-12d3-a456-426614174000 missing")
	f.Add("tab\tseparated  and   spaced")
	f.Fuzz(func(t *testing.T, msg string) {
		once := NormalizeErrorSignature(msg)
		if strings.Contains(once, "\t") || strings.Contains(once, "  ") {
			t.Fatalf("NormalizeErrorSignature(%q) = %q, has tab or double space", msg, once)
		}
		if twice := NormalizeErrorSignature(once); twice != once {
			t.Fatalf("not idempotent\ninput: %q\nonce:  %q\ntwice: %q", msg, once, twice)
		}
	})
}

// naiveTail is the reference for readTailFrom: split everything, drop one
// trailing "\r" per line, cap each line, skip empties and keep the last limit.
func naiveTail(data []byte, limit int) []string {
	if limit <= 0 {
		return nil
	}
	var out []string
	for line := range strings.SplitSeq(string(data), "\n") {
		capped := line
		if len(capped) <= readTailMaxLineBytes+1 {
			capped = strings.TrimSuffix(capped, "\r")
		}
		capped = truncateBytes(capped, readTailMaxLineBytes)
		if capped != "" {
			out = append(out, capped)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// FuzzReadTailFrom compares the backwards chunked tail reader against the
// naive forward reference. The fuzzed bytes are repeated so inputs cross the
// readTailChunkBytes boundary and exercise lines spanning two chunks.
func FuzzReadTailFrom(f *testing.F) {
	f.Add([]byte("a\nb\nc"), uint8(0), uint16(2))
	f.Add([]byte("line one\r\nline two\r\n\r\n"), uint8(3), uint16(100))
	f.Add([]byte("\n\n\n"), uint8(0), uint16(1))
	f.Add([]byte("no newline at all"), uint8(200), uint16(5))
	f.Add([]byte("x\r"), uint8(255), uint16(1000))
	f.Add([]byte(strings.Repeat("y", 300)+"\n"), uint8(255), uint16(3))
	f.Fuzz(func(t *testing.T, unit []byte, reps uint8, limit uint16) {
		if len(unit) == 0 {
			unit = []byte("\n")
		}
		// Up to ~256 copies: small units stay small, larger ones cross the
		// 64 KiB chunk size. Cap total size to keep each exec fast.
		n := min(int(reps)+1, max(1, (4*readTailChunkBytes)/len(unit)))
		data := bytes.Repeat(unit, n)
		got, err := readTailFrom(bytes.NewReader(data), int64(len(data)), int(limit))
		if err != nil {
			t.Fatalf("readTailFrom: %v", err)
		}
		want := naiveTail(data, int(limit))
		if !slices.Equal(got, want) {
			t.Fatalf("readTailFrom mismatch (size %d, limit %d)\n got  %d lines: %.200q\n want %d lines: %.200q",
				len(data), limit, len(got), got, len(want), want)
		}
	})
}

// FuzzParseJournaldLine checks journald JSON parsing never panics and only
// accepts entries with a message and a known severity.
func FuzzParseJournaldLine(f *testing.F) {
	for _, s := range []string{
		`{"MESSAGE":"hello","PRIORITY":"3","__REALTIME_TIMESTAMP":"1700000000000000"}`,
		`{"MESSAGE":[104,105],"PRIORITY":"9"}`,
		`{"MESSAGE":null}`,
		`{"MESSAGE":"x","__REALTIME_TIMESTAMP":"-5"}`,
		`{"MESSAGE":"x","__REALTIME_TIMESTAMP":"99999999999999999999"}`,
		`not json`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		rec, ok := parseJournaldLine(line)
		if !ok {
			return
		}
		if rec.Message == "" {
			t.Fatalf("accepted empty message for %q", line)
		}
		if !slices.Contains(validSeverities, rec.Severity) {
			t.Fatalf("severity %q for %q", rec.Severity, line)
		}
	})
}

// FuzzParseSmallJSON covers the small JSON payload parsers fed by the gateway
// and the OpenClaw CLI: lock files, /readyz bodies and the model catalog.
func FuzzParseSmallJSON(f *testing.F) {
	for _, s := range []string{
		`{"pid":123,"createdAt":"2026-03-01T10:00:00Z"}`,
		`{"pid":-1}`,
		`{"ready":false,"failing":["discord","telegram"]}`,
		`{"failing":[]}`,
		`{"count":1,"models":[{"key":"openai/gpt-5","name":"GPT-5","contextWindow":400000}]}`,
		`[{"key":"a","contextTokens":1e30},{"key":""},null,1]`,
		`null`,
		``,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if lk, ok := parseGatewayLock(data); ok && lk.pid <= 0 {
			t.Fatalf("lock accepted pid %d", lk.pid)
		}
		if failing, ok := parseReadyzFailingOK(data); ok && failing != nil && len(failing) == 0 {
			t.Fatal("readyz returned an empty non-nil failing list")
		}
		// Window values are not range-checked: int(float64) of an absurd
		// contextWindow is implementation-defined (saturating on arm64,
		// MinInt64 on amd64), so only names and panics are asserted.
		for key, name := range parseModelCatalog(data).names {
			if name == "" {
				t.Fatalf("catalog kept empty name for %q", key)
			}
		}
	})
}
