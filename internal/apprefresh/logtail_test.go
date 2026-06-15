package apprefresh

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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
		// Unique per-line content exercises the ring-buffer wrap-around: with a
		// limit of 5 and 100 lines, write wraps the ring 20 times, so a correct
		// implementation must return exactly the last 5 distinct lines in order.
		fmt.Fprintf(&b, "line-%03d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readTailLines(path, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"line-095", "line-096", "line-097", "line-098", "line-099"}
	if len(lines) != len(want) {
		t.Fatalf("want %d lines, got %d: %v", len(want), len(lines), lines)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("tail window[%d] = %q, want %q (full window: %v)", i, lines[i], w, lines)
		}
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
	// Cases with stable, intended output.
	exact := []struct {
		in, want string
	}{
		{"connection refused", "connection refused"},
		{"failed for id 12345", "failed for <id>"},
		{"trace 550e8400-e29b-41d4-a716-446655440000 lost", "trace <uuid> lost"},
		{"got 5 errors and 17 warnings", "got <n> errors and <n> warnings"},
		{"   spaces   collapsed   here   ", "spaces collapsed here"},
	}
	for _, tc := range exact {
		t.Run(tc.in, func(t *testing.T) {
			if got := NormalizeErrorSignature(tc.in); got != tc.want {
				t.Errorf("NormalizeErrorSignature(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	// Invariant: regardless of substitution order (there is a known ordering
	// quirk where numeric masking runs before the timestamp prefix), the result
	// must NOT leak volatile tokens — no literal UUID and no multi-digit run may
	// survive. Asserting the invariant instead of the exact buggy string lets a
	// future ordering fix pass without editing this test.
	reMultiDigit := regexp.MustCompile(`[0-9]{2,}`)
	t.Run("volatile tokens masked invariant", func(t *testing.T) {
		// Inputs where the volatile tokens are bounded by word boundaries, so
		// the \b\d+\b numeric mask and UUID mask both apply cleanly.
		inputs := []string{
			"req 9f1c2b3a-4d5e-6f70-8190-a1b2c3d4e5f6 failed after 4500 ms",
			"job 42 retry 1337 abandoned",
		}
		for _, in := range inputs {
			got := NormalizeErrorSignature(in)
			if reUUID.MatchString(got) {
				t.Errorf("UUID survived normalization: %q → %q", in, got)
			}
			if reMultiDigit.MatchString(got) {
				t.Errorf("multi-digit run survived normalization: %q → %q", in, got)
			}
		}
	})

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

// TestParseLogTimestamp_InvalidShapeNoInfiniteRecursion guards the recursion
// fix: a string the prefix regex accepts on shape but time.Parse rejects on
// value (bad month/day/time) must not recurse forever. Before the fix this
// caused a stack overflow because the extracted prefix equalled the input.
func TestParseLogTimestamp_InvalidShapeNoInfiniteRecursion(t *testing.T) {
	for _, raw := range []string{
		"2026-13-45T25:61:99",
		"2026-99-99 99:99:99.999999999",
		"0000-00-00T00:00:00+99:99",
	} {
		ts, seen := ParseLogTimestamp(raw)
		if !ts.IsZero() || seen != "" {
			t.Errorf("%q: want zero/empty (unparseable), got %v/%q", raw, ts, seen)
		}
	}
}

// TestParseLogTimestamp_TZLessUsesLocal guards against regression of the bug
// where time.Parse on a TZ-less layout defaulted to UTC. Gateway logs are
// emitted in local time; if the parser interprets them as UTC, chart buckets
// drift by the local UTC offset.
func TestParseLogTimestamp_TZLessIsLocalNotUTC(t *testing.T) {
	const raw = "2026-05-01 10:00:00"
	ts, _ := ParseLogTimestamp(raw)
	if ts.IsZero() {
		t.Fatalf("expected parse to succeed")
	}
	if ts.Location() != time.Local {
		t.Fatalf("location: want time.Local, got %v", ts.Location())
	}
	want := time.Date(2026, 5, 1, 10, 0, 0, 0, time.Local)
	if !ts.Equal(want) {
		t.Fatalf("ts: want %v (Local), got %v (%v)", want, ts, ts.Location())
	}
}

// TestParseLogTimestamp_RFC3339OffsetPreserved guards that the location
// switch did not break inputs that carry their own offset.
func TestParseLogTimestamp_RFC3339OffsetPreserved(t *testing.T) {
	const raw = "2026-05-01T10:00:00+03:00"
	ts, _ := ParseLogTimestamp(raw)
	if ts.IsZero() {
		t.Fatalf("expected parse to succeed")
	}
	// Expected absolute instant: 07:00:00 UTC.
	want := time.Date(2026, 5, 1, 7, 0, 0, 0, time.UTC)
	if !ts.Equal(want) {
		t.Fatalf("ts: want %v, got %v", want, ts)
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
		t.Run(tc.line, func(t *testing.T) {
			got := classifySeverity(tc.line, tc.component)
			if got != tc.want {
				t.Errorf("classifySeverity(%q, %q) = %q, want %q", tc.line, tc.component, got, tc.want)
			}
		})
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
		t.Run(tc.line, func(t *testing.T) {
			got := classifySeverity(tc.line, tc.component)
			if got != tc.want {
				t.Errorf("classifySeverity(%q, %q) = %q, want %q", tc.line, tc.component, got, tc.want)
			}
		})
	}
}

func TestInferSeverity_RawWins(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		line string
		want string
	}{
		{"err raw beats benign line", "err", "everything is fine", "error"},
		{"error raw", "error", "fine", "error"},
		{"fatal raw", "fatal", "fine", "error"},
		{"panic raw", "panic", "fine", "error"},
		// stale/missing/unavailable/timeout are mapped to "error" by the raw
		// switch (not "warn"), even though classifySeverity treats them as warn.
		{"stale raw maps to error", "stale", "fine", "error"},
		{"missing raw maps to error", "missing", "fine", "error"},
		{"unavailable raw maps to error", "unavailable", "fine", "error"},
		{"timeout raw maps to error", "timeout", "fine", "error"},
		{"warn raw", "warn", "fine", "warn"},
		{"warning raw", "warning", "fine", "warn"},
		{"debug raw", "debug", "fine", "debug"},
		{"mixed case raw", "  ERROR  ", "fine", "error"},
		{"whitespace trimmed warn", "  warn  ", "fine", "warn"},
		// Empty/unknown raw falls back to classifySeverity over the line.
		{"empty raw falls back to line", "", "panic: boom", "error"},
		{"unknown raw falls back to benign line", "trace", "all good", "info"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := inferSeverity(tc.raw, tc.line); got != tc.want {
				t.Errorf("inferSeverity(%q, %q) = %q, want %q", tc.raw, tc.line, got, tc.want)
			}
		})
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

func TestCandidateLogPaths_FallbackPriorityAndGuards(t *testing.T) {
	root := filepath.Join(t.TempDir(), "fallback")
	SetLogFallbackRoots(func() []string { return []string{root} })
	t.Cleanup(func() { SetLogFallbackRoots(nil) })

	got := candidateLogPaths("/base", "logs/gateway.log")
	want := []string{
		filepath.Join("/base", "logs", "gateway.log"),
		filepath.Join(root, "gateway.log"),
	}
	if len(got) != len(want) {
		t.Fatalf("candidateLogPaths len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidateLogPaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	for _, source := range []string{"../outside.log", "/absolute.log"} {
		if got := candidateLogPaths("/base", source); len(got) != 0 {
			t.Fatalf("candidateLogPaths(%q) = %v, want empty", source, got)
		}
	}
}

func TestReadMergedLogs_SortsSingleSourceAcrossPrimaryAndFallback(t *testing.T) {
	openclawDir := t.TempDir()
	fallbackDir := t.TempDir()
	SetLogFallbackRoots(func() []string { return []string{fallbackDir} })
	t.Cleanup(func() { SetLogFallbackRoots(nil) })

	writeLogLines(t, filepath.Join(openclawDir, "logs", "gateway.log"),
		"2026-04-13T10:00:00Z primary oldest",
		"2026-04-13T10:00:04Z primary newest",
	)
	writeLogLines(t, filepath.Join(fallbackDir, "gateway.log"),
		"2026-04-13T10:00:01Z fb a",
		"2026-04-13T10:00:02Z fb b",
		"2026-04-13T10:00:03Z fb c",
	)

	records, err := ReadMergedLogs(openclawDir, []string{"logs/gateway.log"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(records))
	}
	wantMessages := []string{"fb b", "fb c", "primary newest"}
	for i, want := range wantMessages {
		if records[i].Message != want {
			t.Fatalf("records[%d].Message = %q, want %q", i, records[i].Message, want)
		}
		if i > 0 && records[i-1].TimestampMs > records[i].TimestampMs {
			t.Fatalf("records out of order at %d: %d > %d", i, records[i-1].TimestampMs, records[i].TimestampMs)
		}
	}
}

func TestReadMergedLogs_DedupesOverlappingPrimaryAndFallbackLines(t *testing.T) {
	openclawDir := t.TempDir()
	fallbackDir := t.TempDir()
	SetLogFallbackRoots(func() []string { return []string{fallbackDir} })
	t.Cleanup(func() { SetLogFallbackRoots(nil) })

	overlap := "2026-04-13T10:00:02Z shared migration line"
	writeLogLines(t, filepath.Join(openclawDir, "logs", "gateway.log"),
		"2026-04-13T10:00:01Z primary only",
		overlap,
	)
	writeLogLines(t, filepath.Join(fallbackDir, "gateway.log"),
		overlap,
		"2026-04-13T10:00:03Z fallback only",
	)

	records, err := ReadMergedLogs(openclawDir, []string{"logs/gateway.log"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	wantMessages := []string{"primary only", "shared migration line", "fallback only"}
	if len(records) != len(wantMessages) {
		t.Fatalf("len(records) = %d, want %d: %+v", len(records), len(wantMessages), records)
	}
	for i, want := range wantMessages {
		if records[i].Message != want {
			t.Fatalf("records[%d].Message = %q, want %q", i, records[i].Message, want)
		}
	}
}

func TestLogFallbackRoots_ReturnsCopy(t *testing.T) {
	root := filepath.Join(t.TempDir(), "logs")
	SetLogFallbackRoots(func() []string { return []string{root} })
	t.Cleanup(func() { SetLogFallbackRoots(nil) })

	got := LogFallbackRoots()
	if len(got) != 1 || got[0] != root {
		t.Fatalf("LogFallbackRoots() = %v, want [%q]", got, root)
	}
	got[0] = "mutated"

	got = LogFallbackRoots()
	if len(got) != 1 || got[0] != root {
		t.Fatalf("LogFallbackRoots() after caller mutation = %v, want [%q]", got, root)
	}
}

func TestLogFallbackRoots_ConcurrentAccess(t *testing.T) {
	t.Cleanup(func() { SetLogFallbackRoots(nil) })

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			SetLogFallbackRoots(func() []string { return []string{"/tmp/openclaw-logs"} })
		}()
		go func() {
			defer wg.Done()
			_ = candidateLogPaths("/tmp/openclaw", "logs/gateway.log")
		}()
	}
	wg.Wait()
}

func writeLogLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
