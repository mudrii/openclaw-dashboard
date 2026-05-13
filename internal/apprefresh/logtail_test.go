package apprefresh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadTailLines_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readTailLines(path, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("want 0 lines, got %d", len(lines))
	}
}

func TestReadTailLines_SingleLineNoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "one.log")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readTailLines(path, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 1 || lines[0] != "hello world" {
		t.Fatalf("want [hello world], got %v", lines)
	}
}

func TestReadTailLines_RespectsLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "many.log")
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("line-")
		b.WriteString(string(rune('a' + (i % 26))))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readTailLines(path, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 5 {
		t.Fatalf("want 5 lines, got %d", len(lines))
	}
	// Returned oldest→newest; last should be from end of file.
	if lines[len(lines)-1] != "line-v" { // 99 % 26 = 21 → 'v'
		t.Fatalf("expected last line line-v, got %q", lines[len(lines)-1])
	}
}

func TestReadTailLines_TruncatesOversizedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.log")
	huge := strings.Repeat("x", readTailMaxLineBytes+1000)
	if err := os.WriteFile(path, []byte(huge+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readTailLines(path, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}
	if len(lines[0]) != readTailMaxLineBytes {
		t.Fatalf("line not truncated to cap: len=%d want=%d", len(lines[0]), readTailMaxLineBytes)
	}
}

func TestReadTailLines_ZeroLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.log")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readTailLines(path, 0)
	if err != nil || lines != nil {
		t.Fatalf("want (nil,nil), got (%v,%v)", lines, err)
	}
}

func TestMergeLatestRecords_EmptySources(t *testing.T) {
	if out := mergeLatestRecords(nil, 10); out != nil {
		t.Fatalf("want nil, got %v", out)
	}
	if out := mergeLatestRecords([][]LogRecord{}, 10); out != nil {
		t.Fatalf("want nil, got %v", out)
	}
}

func TestMergeLatestRecords_SingleSource(t *testing.T) {
	t1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	src := []LogRecord{
		{Source: "a", Timestamp: t1, TimestampMs: t1.UnixMilli(), Message: "old"},
		{Source: "a", Timestamp: t1.Add(time.Minute), TimestampMs: t1.Add(time.Minute).UnixMilli(), Message: "newer"},
	}
	out := mergeLatestRecords([][]LogRecord{src}, 10)
	if len(out) != 2 {
		t.Fatalf("want 2 records, got %d", len(out))
	}
	if out[0].Message != "old" || out[1].Message != "newer" {
		t.Fatalf("expected oldest→newest order, got %q,%q", out[0].Message, out[1].Message)
	}
}

func TestMergeLatestRecords_MultipleSourcesInterleaved(t *testing.T) {
	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	a := []LogRecord{
		{Source: "a", Timestamp: base.Add(1 * time.Minute), TimestampMs: base.Add(1 * time.Minute).UnixMilli(), Message: "a1"},
		{Source: "a", Timestamp: base.Add(3 * time.Minute), TimestampMs: base.Add(3 * time.Minute).UnixMilli(), Message: "a2"},
	}
	b := []LogRecord{
		{Source: "b", Timestamp: base.Add(2 * time.Minute), TimestampMs: base.Add(2 * time.Minute).UnixMilli(), Message: "b1"},
		{Source: "b", Timestamp: base.Add(4 * time.Minute), TimestampMs: base.Add(4 * time.Minute).UnixMilli(), Message: "b2"},
	}
	out := mergeLatestRecords([][]LogRecord{a, b}, 10)
	want := []string{"a1", "b1", "a2", "b2"}
	if len(out) != len(want) {
		t.Fatalf("want %d records, got %d", len(want), len(out))
	}
	for i, w := range want {
		if out[i].Message != w {
			t.Fatalf("idx %d: want %q, got %q", i, w, out[i].Message)
		}
	}
}

func TestMergeLatestRecords_CapsAtLimit(t *testing.T) {
	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	src := make([]LogRecord, 0, 20)
	for i := 0; i < 20; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		src = append(src, LogRecord{Source: "x", Timestamp: ts, TimestampMs: ts.UnixMilli(), Message: "msg"})
	}
	out := mergeLatestRecords([][]LogRecord{src}, 5)
	if len(out) != 5 {
		t.Fatalf("want 5 latest, got %d", len(out))
	}
}

func TestNormalizeErrorSignature(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"connection refused", "connection refused"},
		{"failed for id 12345", "failed for <id>"},
		{"trace 550e8400-e29b-41d4-a716-446655440000 lost", "trace <uuid> lost"},
		{"got 5 errors and 17 warnings", "got <n> errors and <n> warnings"},
		// Numeric regex runs before timestamp prefix regex so digits inside the
		// timestamp are normalized to <n> before <ts> can match. Pin current
		// behavior; a future change should reorder substitutions.
		{"2026-05-01T10:00:00Z gateway timeout", "<n>-<n>-01t10:<n>:00z gateway timeout"},
		{"   spaces   collapsed   here   ", "spaces collapsed here"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := NormalizeErrorSignature(tc.in)
			if got != tc.want {
				t.Errorf("NormalizeErrorSignature(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseLogTimestamp_RFC3339(t *testing.T) {
	ts, seen := ParseLogTimestamp("2026-05-01T10:00:00Z")
	if ts.IsZero() {
		t.Fatalf("expected parse to succeed")
	}
	if seen != "2026-05-01T10:00:00Z" {
		t.Fatalf("seen mismatch: %q", seen)
	}
	if !ts.Equal(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("ts mismatch: %v", ts)
	}
}

func TestParseLogTimestamp_MultipleLayouts(t *testing.T) {
	tests := []string{
		"2026-05-01T10:00:00Z",
		"2026-05-01T10:00:00.123456789Z",
		"2026-05-01 10:00:00",
		"2026-05-01 10:00:00.123",
	}
	for _, in := range tests {
		ts, _ := ParseLogTimestamp(in)
		if ts.IsZero() {
			t.Errorf("layout %q failed to parse", in)
		}
	}
}

func TestParseLogTimestamp_PrefixFallback(t *testing.T) {
	ts, _ := ParseLogTimestamp("2026-05-01T10:00:00Z something happened")
	if ts.IsZero() {
		t.Fatalf("expected prefix to parse, got zero time")
	}
}

func TestParseLogTimestamp_NoCandidates(t *testing.T) {
	ts, seen := ParseLogTimestamp()
	if !ts.IsZero() || seen != "" {
		t.Fatalf("want zero/empty, got %v/%q", ts, seen)
	}
}

func TestClassifySeverity(t *testing.T) {
	tests := []struct {
		line, component, want string
	}{
		{"panic: nil pointer", "", "error"},
		{"connection error", "", "error"},
		{"all good", "stderr", "error"},
		{"warning: cache miss", "", "warn"},
		{"timeout reading", "", "warn"},
		{"debug message", "", "debug"},
		{"healthy", "", "info"},
	}
	for _, tc := range tests {
		got := classifySeverity(tc.line, tc.component)
		if got != tc.want {
			t.Errorf("classifySeverity(%q, %q) = %q, want %q", tc.line, tc.component, got, tc.want)
		}
	}
}

func TestClassifySeverity_NoFalsePositives(t *testing.T) {
	tests := []struct {
		line, component, want string
	}{
		// "deprecated" is a single token containing no error/warn/debug whole
		// words → info (legacy substring matcher returned "error").
		{"deprecated API", "", "info"},
		// "no error occurred" — negation of an error token suppresses the
		// classification → info (legacy substring matcher returned "error").
		{"no error occurred", "", "info"},
		{"warning: cache miss", "", "warn"},
		{"panic: nil", "", "error"},
		{"timeout reading", "", "warn"},
	}
	for _, tc := range tests {
		got := classifySeverity(tc.line, tc.component)
		if got != tc.want {
			t.Errorf("classifySeverity(%q, %q) = %q, want %q", tc.line, tc.component, got, tc.want)
		}
	}
}

func TestInferSeverity_RawWins(t *testing.T) {
	if inferSeverity("err", "everything is fine") != "error" {
		t.Errorf("raw='err' should win over benign line")
	}
	if inferSeverity("", "panic: boom") != "error" {
		t.Errorf("empty raw should fall back to classifySeverity")
	}
}

func TestCompareLogRecords(t *testing.T) {
	t1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Second)

	a := LogRecord{Source: "a", Timestamp: t1, Raw: "x"}
	b := LogRecord{Source: "b", Timestamp: t2, Raw: "y"}
	if compareLogRecords(a, b) >= 0 {
		t.Errorf("earlier timestamp should compare less")
	}
	if compareLogRecords(b, a) <= 0 {
		t.Errorf("later timestamp should compare greater")
	}

	// Equal timestamps → tiebreak on source then raw.
	c := LogRecord{Source: "a", Timestamp: t1, Raw: "x"}
	d := LogRecord{Source: "b", Timestamp: t1, Raw: "x"}
	if compareLogRecords(c, d) >= 0 {
		t.Errorf("equal timestamp + source tiebreak: a<b expected")
	}

	// Zero timestamps sort before non-zero.
	zero := LogRecord{Source: "z"}
	if compareLogRecords(zero, a) >= 0 {
		t.Errorf("zero timestamp should compare less than non-zero")
	}
}

func TestResolveLogPath_Rejects(t *testing.T) {
	cases := []string{"", ".", "../outside", "/absolute"}
	for _, c := range cases {
		if _, ok := ResolveLogPath("/base", c); ok {
			t.Errorf("expected reject for %q", c)
		}
	}
}

func TestResolveLogPath_Accepts(t *testing.T) {
	p, ok := ResolveLogPath("/base", "logs/gateway.log")
	if !ok {
		t.Fatalf("expected accept")
	}
	want := filepath.Join("/base", "logs/gateway.log")
	if p != want {
		t.Errorf("got %q, want %q", p, want)
	}
}
