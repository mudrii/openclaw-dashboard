package apprefresh

import (
	"testing"
)

// TestParseJournaldLine_PriorityMapping pins the journald PRIORITY (syslog
// level 0-7) → dashboard severity mapping used by the Linux logs collector.
// journald -o json emits all values as strings, including PRIORITY.
func TestParseJournaldLine_PriorityMapping(t *testing.T) {
	cases := []struct {
		priority string
		want     string
	}{
		{"0", "error"}, // emerg
		{"1", "error"}, // alert
		{"2", "error"}, // crit
		{"3", "error"}, // err
		{"4", "warn"},  // warning
		{"5", "info"},  // notice
		{"6", "info"},  // info
		{"7", "debug"}, // debug
	}
	for _, tc := range cases {
		t.Run("priority_"+tc.priority, func(t *testing.T) {
			line := `{"PRIORITY":"` + tc.priority + `","MESSAGE":"something happened","__REALTIME_TIMESTAMP":"1700000000000000"}`
			rec, ok := parseJournaldLine(line)
			if !ok {
				t.Fatalf("parseJournaldLine(%q) ok=false, want true", line)
			}
			if rec.Severity != tc.want {
				t.Errorf("severity: priority %s → %q, want %q", tc.priority, rec.Severity, tc.want)
			}
		})
	}
}

// TestParseJournaldLine_FieldsExtracted proves MESSAGE and the microsecond
// __REALTIME_TIMESTAMP are surfaced correctly.
func TestParseJournaldLine_FieldsExtracted(t *testing.T) {
	line := `{"PRIORITY":"6","MESSAGE":"gateway listening on 18789","__REALTIME_TIMESTAMP":"1700000000000000"}`
	rec, ok := parseJournaldLine(line)
	if !ok {
		t.Fatalf("ok=false, want true")
	}
	if rec.Message != "gateway listening on 18789" {
		t.Errorf("message = %q, want %q", rec.Message, "gateway listening on 18789")
	}
	// 1700000000000000 µs == 1700000000000 ms
	if rec.TimestampMs != 1700000000000 {
		t.Errorf("timestampMs = %d, want 1700000000000", rec.TimestampMs)
	}
	if rec.Timestamp.IsZero() {
		t.Errorf("timestamp should be set from __REALTIME_TIMESTAMP")
	}
}

// TestParseJournaldLine_MissingPriorityFallsBackToMessage proves that when
// PRIORITY is absent the severity is inferred from MESSAGE via classifySeverity,
// keeping behavior consistent with the file-based JSONL path.
func TestParseJournaldLine_MissingPriorityFallsBackToMessage(t *testing.T) {
	line := `{"MESSAGE":"fatal error: connection refused","__REALTIME_TIMESTAMP":"1700000000000000"}`
	rec, ok := parseJournaldLine(line)
	if !ok {
		t.Fatalf("ok=false, want true")
	}
	if rec.Severity != "error" {
		t.Errorf("severity = %q, want error (inferred from message)", rec.Severity)
	}
}

// TestParseJournaldLine_MissingTimestampStillParses proves a line without
// __REALTIME_TIMESTAMP is still surfaced (ok=true) with a zero timestamp, so
// the merge layer can fall back the same way it does for file records.
func TestParseJournaldLine_MissingTimestampStillParses(t *testing.T) {
	rec, ok := parseJournaldLine(`{"PRIORITY":"6","MESSAGE":"no timestamp here"}`)
	if !ok {
		t.Fatalf("ok=false, want true")
	}
	if !rec.Timestamp.IsZero() {
		t.Errorf("timestamp = %v, want zero", rec.Timestamp)
	}
	if rec.TimestampMs != 0 {
		t.Errorf("timestampMs = %d, want 0", rec.TimestampMs)
	}
	if rec.Message != "no timestamp here" {
		t.Errorf("message = %q, want %q", rec.Message, "no timestamp here")
	}
}

// TestParseJournaldLine_Degenerate covers malformed JSON and an absent MESSAGE:
// both must report ok=false so the caller skips the line rather than emitting an
// empty record.
func TestParseJournaldLine_Degenerate(t *testing.T) {
	for _, line := range []string{
		`{not json`,
		`{"PRIORITY":"6","__REALTIME_TIMESTAMP":"1700000000000000"}`, // no MESSAGE
		``,
	} {
		if rec, ok := parseJournaldLine(line); ok {
			t.Errorf("parseJournaldLine(%q) ok=true (%+v), want false", line, rec)
		}
	}
}
