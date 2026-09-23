package apprefresh

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// referenceLastSessionModel is the original string-splitting, decode-every-line
// reader. scanLastSessionModel must agree with it on every input.
func referenceLastSessionModel(data []byte) (string, bool) {
	const chunkSize int64 = 64 * 1024
	r := bytes.NewReader(data)
	var tail string
	for end := int64(len(data)); end > 0; {
		start := max(end-chunkSize, 0)
		buf := make([]byte, end-start)
		if _, err := r.ReadAt(buf, start); err != nil {
			return "", false
		}
		lines := strings.Split(string(buf)+tail, "\n")
		if start > 0 {
			tail = lines[0]
			lines = lines[1:]
		} else {
			tail = ""
		}
		for _, line := range slices.Backward(lines) {
			if model, ok := sessionModelFromLine(line); ok {
				return model, true
			}
		}
		end = start
	}
	return sessionModelFromLine(tail)
}

func TestScanLastSessionModelMatchesReference(t *testing.T) {
	msg := func(n int) string {
		return fmt.Sprintf(`{"type":"message","message":{"role":"assistant","content":"%s"}}`, strings.Repeat("x", n))
	}
	cases := map[string]string{
		"empty":             "",
		"only newlines":     "\n\n\n",
		"single change":     `{"type":"model_change","provider":"openai","modelId":"gpt-5"}`,
		"latest wins":       `{"type":"model_change","provider":"openai","modelId":"gpt-4o"}` + "\n" + msg(10) + "\n" + `{"type":"model_change","provider":"openai","modelId":"gpt-5"}` + "\n",
		"crlf":              `{"type":"model_change","provider":"a","modelId":"b"}` + "\r\n" + msg(5) + "\r\n",
		"unicode escape":    `{"type":"model\u005fchange","provider":"a","modelId":"b"}` + "\n" + msg(3),
		"escaped all":       `{"\u0074ype":"\u006d\u006f\u0064\u0065\u006c\u005f\u0063\u0068\u0061\u006e\u0067\u0065","provider":"p","modelId":"m"}`,
		"marker in text":    `{"type":"message","text":"model_change"}` + "\n",
		"incomplete change": `{"type":"model_change","provider":"","modelId":"m"}`,
		"malformed":         `{"type":"model_change","provider":}` + "\n" + msg(1),
		"spans chunk": `{"type":"model_change","provider":"x","modelId":"y"}` + "\n" + msg(64*1024-40) + "\n" +
			msg(64*1024+100),
		"change spans chunk": msg(64*1024-30) + "\n" + `{"type":"model_change","provider":"x","modelId":"span"}` + "\n" + msg(64*1024-20),
		"huge last line":     `{"type":"model_change","provider":"x","modelId":"z"}` + "\n" + msg(300*1024),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			data := []byte(content)
			wantModel, wantOK := referenceLastSessionModel(data)
			gotModel, gotOK := scanLastSessionModelOK(t, data)
			if gotModel != wantModel || gotOK != wantOK {
				t.Fatalf("scan = (%q, %v), reference = (%q, %v)", gotModel, gotOK, wantModel, wantOK)
			}
		})
	}
}

func TestScanLastSessionModelDecodesEscapedType(t *testing.T) {
	data := []byte(`{"type":"model\u005fchange","provider":"p","modelId":"m"}` + "\n" + `{"type":"message"}`)
	got, ok := scanLastSessionModelOK(t, data)
	if !ok || got != "p/m" {
		t.Fatalf("scan = (%q, %v), want (\"p/m\", true)", got, ok)
	}
}

func TestScanLastSessionModelRandomizedMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	lines := []string{
		`{"type":"model_change","provider":"openai","modelId":"gpt-5"}`,
		`{"type":"model_change","provider":"anthropic","modelId":"claude-opus-4-6"}`,
		`{"type":"\u006dodel\u005fchange","provider":"esc","modelId":"aped"}`,
		`{"type":"model_change","provider":"","modelId":"x"}`,
		`{"type":"message","note":"model_change"}`,
		`not json at all`,
		"",
		"   ",
	}
	for i := range 300 {
		var b strings.Builder
		for range rng.IntN(40) {
			if rng.IntN(4) == 0 {
				b.WriteString(lines[rng.IntN(len(lines))])
			} else {
				fmt.Fprintf(&b, `{"type":"message","content":"%s"}`, strings.Repeat("y", rng.IntN(20000)))
			}
			if rng.IntN(5) == 0 {
				b.WriteString("\r")
			}
			b.WriteString("\n")
		}
		data := []byte(b.String())
		if rng.IntN(2) == 0 && len(data) > 0 {
			data = data[:len(data)-1]
		}
		wantModel, wantOK := referenceLastSessionModel(data)
		gotModel, gotOK := scanLastSessionModelOK(t, data)
		if gotModel != wantModel || gotOK != wantOK {
			t.Fatalf("case %d: scan = (%q, %v), reference = (%q, %v)", i, gotModel, gotOK, wantModel, wantOK)
		}
	}
}

func TestTranscriptModelReaderReusesUnchangedScans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	write := func(content string, mtime time.Time) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	pass := func(prev map[string]transcriptModelScan) (*transcriptModelReader, string, bool) {
		t.Helper()
		r := &transcriptModelReader{prev: prev, next: map[string]transcriptModelScan{}}
		model, ok := r.read(path)
		return r, model, ok
	}
	mtime := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	write(`{"type":"model_change","provider":"a","modelId":"one"}`, mtime)
	first, model, ok := pass(nil)
	if !ok || model != "a/one" {
		t.Fatalf("first pass = (%q, %v), want (\"a/one\", true)", model, ok)
	}

	// Same size and mtime: the previous scan stands in for the file.
	write(`{"type":"model_change","provider":"a","modelId":"two"}`, mtime)
	second, model, ok := pass(first.next)
	if !ok || model != "a/one" {
		t.Fatalf("unchanged pass = (%q, %v), want cached (\"a/one\", true)", model, ok)
	}

	// A size or mtime change rescans.
	write(`{"type":"model_change","provider":"a","modelId":"three"}`, mtime)
	third, model, ok := pass(second.next)
	if !ok || model != "a/three" {
		t.Fatalf("grown pass = (%q, %v), want (\"a/three\", true)", model, ok)
	}
	write(`{"type":"model_change","provider":"b","modelId":"three"}`, mtime.Add(time.Second))
	fourth, model, ok := pass(third.next)
	if !ok || model != "b/three" {
		t.Fatalf("touched pass = (%q, %v), want (\"b/three\", true)", model, ok)
	}

	// A deleted transcript misses and is not carried forward.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	fifth, model, ok := pass(fourth.next)
	if ok || model != "" {
		t.Fatalf("deleted pass = (%q, %v), want (\"\", false)", model, ok)
	}
	if len(fifth.next) != 0 {
		t.Fatalf("deleted transcript carried into next pass: %v", fifth.next)
	}
}

func scanLastSessionModelOK(t *testing.T, data []byte) (string, bool) {
	t.Helper()
	model, ok, err := scanLastSessionModel(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("scanLastSessionModel error = %v", err)
	}
	return model, ok
}
