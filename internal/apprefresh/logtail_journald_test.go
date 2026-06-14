package apprefresh

import (
	"context"
	"errors"
	"testing"
)

// stubJournaldRunner replaces the journalctl exec seam with a deterministic
// fake and restores it after the test.
func stubJournaldRunner(t *testing.T, out string, err error) {
	t.Helper()
	prev := journaldRunner
	journaldRunner = func(_ context.Context, _ string, _ int) ([]byte, error) {
		return []byte(out), err
	}
	t.Cleanup(func() { journaldRunner = prev })
}

// TestCollectJournaldRecords_ParsesLines proves the collector turns journalctl
// -o json output (one object per line) into LogRecords with Source and Raw set,
// skipping blank and unparseable lines.
func TestCollectJournaldRecords_ParsesLines(t *testing.T) {
	out := `{"PRIORITY":"3","MESSAGE":"boom","__REALTIME_TIMESTAMP":"1700000000000000"}
{"PRIORITY":"6","MESSAGE":"ok","__REALTIME_TIMESTAMP":"1700000001000000"}

{garbage}
`
	stubJournaldRunner(t, out, nil)
	recs := collectJournaldRecords(context.Background(), "openclaw-gateway", "logs/gateway.log", 50)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2 (blank+garbage skipped)", len(recs))
	}
	if recs[0].Source != "logs/gateway.log" {
		t.Errorf("source = %q, want logs/gateway.log", recs[0].Source)
	}
	if recs[0].Severity != "error" || recs[0].Message != "boom" {
		t.Errorf("record0 = %+v, want severity=error message=boom", recs[0])
	}
	if recs[0].Raw == "" {
		t.Errorf("raw should carry the original journald line")
	}
}

// TestCollectJournaldRecords_RunnerErrorYieldsNil proves a missing journalctl
// binary (or any runner error) collapses to nil rather than surfacing an error
// — the dashboard shows an empty journald source, identical to a missing file.
func TestCollectJournaldRecords_RunnerErrorYieldsNil(t *testing.T) {
	stubJournaldRunner(t, "", errors.New("exec: journalctl: not found"))
	if recs := collectJournaldRecords(context.Background(), "openclaw-gateway", "logs/gateway.log", 50); recs != nil {
		t.Errorf("got %v, want nil on runner error", recs)
	}
}

// forceJournald enables the Linux-only journald path on any host for the
// duration of a test.
func forceJournald(t *testing.T, on bool) {
	t.Helper()
	prev := journaldEnabled
	journaldEnabled = func() bool { return on }
	t.Cleanup(func() { journaldEnabled = prev })
}

// TestReadMergedLogsWithUnit_JournaldFallback proves the merge hook: when a
// source has no log file on disk and journald is enabled, records are
// synthesized from journalctl. Uses a temp dir with no log files so the file
// path yields nothing.
func TestReadMergedLogsWithUnit_JournaldFallback(t *testing.T) {
	SetLogFallbackRoots(func() []string { return nil })
	t.Cleanup(func() { SetLogFallbackRoots(nil) })
	forceJournald(t, true)
	stubJournaldRunner(t, `{"PRIORITY":"3","MESSAGE":"journald says hi","__REALTIME_TIMESTAMP":"1700000000000000"}`, nil)

	tmp := t.TempDir() // no logs/ inside → file candidates miss
	recs, err := ReadMergedLogsWithUnit(tmp, []string{"logs/gateway.log"}, 50, "openclaw-gateway")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1 from journald", len(recs))
	}
	if recs[0].Message != "journald says hi" || recs[0].Severity != "error" {
		t.Errorf("record = %+v, want journald message error severity", recs[0])
	}
}

// TestReadMergedLogsWithUnit_DisabledSkipsJournald proves macOS behavior is
// unchanged: with journald disabled, no shell-out happens and an absent file
// yields no records (the runner must not even be consulted).
func TestReadMergedLogsWithUnit_DisabledSkipsJournald(t *testing.T) {
	SetLogFallbackRoots(func() []string { return nil })
	t.Cleanup(func() { SetLogFallbackRoots(nil) })
	forceJournald(t, false)
	prev := journaldRunner
	journaldRunner = func(_ context.Context, _ string, _ int) ([]byte, error) {
		t.Fatalf("journaldRunner must not be called when journald is disabled")
		return nil, nil
	}
	t.Cleanup(func() { journaldRunner = prev })

	tmp := t.TempDir()
	recs, err := ReadMergedLogsWithUnit(tmp, []string{"logs/gateway.log"}, 50, "openclaw-gateway")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("got %d records, want 0 (journald disabled, no file)", len(recs))
	}
}

// TestResolveSystemdUnit_Precedence pins the unit resolution order:
// OPENCLAW_SYSTEMD_UNIT (full override) > config value > default
// "openclaw-gateway", with OPENCLAW_PROFILE appending a suffix to the
// non-override forms.
func TestResolveSystemdUnit_Precedence(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("OPENCLAW_SYSTEMD_UNIT", "")
		t.Setenv("OPENCLAW_PROFILE", "")
		if got := ResolveSystemdUnit(""); got != "openclaw-gateway" {
			t.Errorf("got %q, want openclaw-gateway", got)
		}
	})
	t.Run("config overrides default", func(t *testing.T) {
		t.Setenv("OPENCLAW_SYSTEMD_UNIT", "")
		t.Setenv("OPENCLAW_PROFILE", "")
		if got := ResolveSystemdUnit("custom-gw"); got != "custom-gw" {
			t.Errorf("got %q, want custom-gw", got)
		}
	})
	t.Run("env overrides config", func(t *testing.T) {
		t.Setenv("OPENCLAW_SYSTEMD_UNIT", "env-unit")
		t.Setenv("OPENCLAW_PROFILE", "work")
		if got := ResolveSystemdUnit("custom-gw"); got != "env-unit" {
			t.Errorf("got %q, want env-unit (verbatim, profile not appended)", got)
		}
	})
	t.Run("profile suffix on default", func(t *testing.T) {
		t.Setenv("OPENCLAW_SYSTEMD_UNIT", "")
		t.Setenv("OPENCLAW_PROFILE", "work")
		if got := ResolveSystemdUnit(""); got != "openclaw-gateway-work" {
			t.Errorf("got %q, want openclaw-gateway-work", got)
		}
	})
}

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
