package apprefresh

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReadTailLines_HugeLineBetweenNormalLines guards against a single
// multi-MiB line failing the whole file (bufio.ErrTooLong).
func TestReadTailLines_HugeLineBetweenNormalLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.log")
	huge := strings.Repeat("é", 3*1024*1024/2) // 3 MiB of 2-byte runes
	if err := os.WriteFile(path, []byte("first\r\n"+huge+"\r\nlast\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readTailLines(path, 10)
	if err != nil {
		t.Fatalf("readTailLines: %v", err)
	}
	if len(lines) != 3 || lines[0] != "first" || lines[2] != "last" {
		t.Fatalf("lines = %d, first=%.20q last=%.20q", len(lines), lines[0], lines[len(lines)-1])
	}
	if len(lines[1]) != readTailMaxLineBytes || !strings.HasPrefix(huge, lines[1]) {
		t.Fatalf("huge line len = %d, want rune-safe prefix of %d bytes", len(lines[1]), readTailMaxLineBytes)
	}
}

func TestReadTailLines_SkipsBlankAndCRLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blank.log")
	if err := os.WriteFile(path, []byte("\n\na\r\n\r\n\nb\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readTailLines(path, 10)
	if err != nil || strings.Join(lines, ",") != "a,b" {
		t.Fatalf("lines = %q, err = %v", lines, err)
	}
}

type countingReaderAt struct {
	r     io.ReaderAt
	bytes int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.bytes += int64(n)
	return n, err
}

// TestReadTailFrom_ReadsBoundedBytes guards against re-reading a whole
// multi-GB log on every poll to find its last few lines.
func TestReadTailFrom_ReadsBoundedBytes(t *testing.T) {
	var b bytes.Buffer
	for i := range 100_000 {
		fmt.Fprintf(&b, "2026-04-13T10:00:00Z line %06d padding padding padding\n", i)
	}
	data := b.Bytes()
	counter := &countingReaderAt{r: bytes.NewReader(data)}
	lines, err := readTailFrom(counter, int64(len(data)), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 5 || !strings.Contains(lines[4], "line 099999") || !strings.Contains(lines[0], "line 099995") {
		t.Fatalf("lines = %q", lines)
	}
	if limit := int64(2 * readTailChunkBytes); counter.bytes > limit {
		t.Fatalf("read %d bytes of %d, want <= %d", counter.bytes, len(data), limit)
	}
}

// TestReadMergedLogs_StackTraceKeepsFileOrder guards against timestamp-less
// continuation lines taking the file mtime and being re-sorted by raw text.
func TestReadMergedLogs_StackTraceKeepsFileOrder(t *testing.T) {
	SetLogFallbackRoots(func() []string { return nil })
	t.Cleanup(func() { SetLogFallbackRoots(nil) })
	dir := t.TempDir()
	lines := []string{
		"2026-04-13T10:00:00Z [gw] error: boom",
		"    at zeta (z.js:1)",
		"    at alpha (a.js:2)",
		"    at zeta (z.js:1)",
		"2026-04-13T10:00:05Z [gw] recovered",
	}
	writeLogLines(t, filepath.Join(dir, "logs", "gateway.log"), lines...)

	records, err := ReadMergedLogs(dir, []string{"logs/gateway.log"}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != len(lines) {
		t.Fatalf("got %d records, want %d: %+v", len(records), len(lines), records)
	}
	errorTs := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC).UnixMilli()
	for i, want := range lines {
		if records[i].Raw != want {
			t.Fatalf("records[%d].Raw = %q, want %q", i, records[i].Raw, want)
		}
	}
	for i := 1; i <= 3; i++ {
		if records[i].TimestampMs != errorTs {
			t.Errorf("continuation %d timestamp = %d, want inherited %d", i, records[i].TimestampMs, errorTs)
		}
	}
}

func TestReadMergedLogs_RedactsFileRecords(t *testing.T) {
	SetLogFallbackRoots(func() []string { return nil })
	t.Cleanup(func() { SetLogFallbackRoots(nil) })
	dir := t.TempDir()
	writeLogLines(t, filepath.Join(dir, "logs", "gateway.log"),
		"2026-04-13T10:00:00Z [gw] calling with Authorization: Bearer abcdef123456secret")

	records, err := ReadMergedLogs(dir, []string{"logs/gateway.log"}, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	for _, field := range []string{records[0].Raw, records[0].Message} {
		if strings.Contains(field, "abcdef123456secret") {
			t.Fatalf("secret leaked: %q", field)
		}
	}
}

func TestCollectJournaldRecords_Redacts(t *testing.T) {
	stubJournaldRunner(t, `{"PRIORITY":"6","MESSAGE":"token=abcdef123456secret sent","__REALTIME_TIMESTAMP":"1700000000000000"}`, nil)
	records := collectJournaldRecords(t.Context(), "unit", "logs/gateway.log", 10)
	if len(records) != 1 {
		t.Fatalf("records=%+v", records)
	}
	for _, field := range []string{records[0].Raw, records[0].Message} {
		if strings.Contains(field, "abcdef123456secret") {
			t.Fatalf("secret leaked: %q", field)
		}
	}
}

// TestReadMergedLogs_RedactsJSONLAfterParsing guards the parse-then-redact
// order: redacting first turned `"hasToken":false` into invalid JSON, which
// dropped the record's own timestamp and message.
func TestReadMergedLogs_RedactsJSONLAfterParsing(t *testing.T) {
	SetLogFallbackRoots(func() []string { return nil })
	t.Cleanup(func() { SetLogFallbackRoots(nil) })
	dir := t.TempDir()
	writeLogLines(t, filepath.Join(dir, "logs", "gateway.jsonl"),
		`{"time":"2026-09-23T10:00:00Z","level":"error","msg":"auth failed token=abcdef123456secret","hasToken":false}`)

	records, err := ReadMergedLogs(dir, []string{"logs/gateway.jsonl"}, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	r := records[0]
	if want := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC).UnixMilli(); r.TimestampMs != want {
		t.Errorf("timestamp = %d, want %d (JSON was not parsed)", r.TimestampMs, want)
	}
	if !strings.HasPrefix(r.Message, "auth failed") {
		t.Errorf("message = %q, want parsed msg field", r.Message)
	}
	for _, field := range []string{r.Raw, r.Message, r.Line} {
		if strings.Contains(field, "abcdef123456secret") {
			t.Fatalf("secret leaked: %q", field)
		}
	}
}
