package apprefresh

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionModelFromLine(t *testing.T) {
	t.Run("valid model_change returns provider slash modelId", func(t *testing.T) {
		line := `{"type":"model_change","provider":"openai","modelId":"gpt-5.4"}`
		got, ok := sessionModelFromLine(line)
		if !ok {
			t.Fatalf("ok=false, want true")
		}
		if got != "openai/gpt-5.4" {
			t.Fatalf("got %q, want %q", got, "openai/gpt-5.4")
		}
	})

	t.Run("non model_change type returns false", func(t *testing.T) {
		line := `{"type":"message","provider":"openai","modelId":"gpt-5.4"}`
		got, ok := sessionModelFromLine(line)
		if ok || got != "" {
			t.Fatalf("got (%q,%v), want (\"\",false)", got, ok)
		}
	})

	t.Run("malformed json returns false", func(t *testing.T) {
		line := `{"type":"model_change","provider":}`
		got, ok := sessionModelFromLine(line)
		if ok || got != "" {
			t.Fatalf("got (%q,%v), want (\"\",false)", got, ok)
		}
	})

	t.Run("empty line returns false", func(t *testing.T) {
		got, ok := sessionModelFromLine("")
		if ok || got != "" {
			t.Fatalf("got (%q,%v), want (\"\",false)", got, ok)
		}
	})

	t.Run("whitespace only returns false", func(t *testing.T) {
		got, ok := sessionModelFromLine("   \t  ")
		if ok || got != "" {
			t.Fatalf("got (%q,%v), want (\"\",false)", got, ok)
		}
	})

	t.Run("missing provider returns false", func(t *testing.T) {
		line := `{"type":"model_change","modelId":"gpt-5.4"}`
		got, ok := sessionModelFromLine(line)
		if ok || got != "" {
			t.Fatalf("got (%q,%v), want (\"\",false)", got, ok)
		}
	})

	t.Run("missing modelId returns false", func(t *testing.T) {
		line := `{"type":"model_change","provider":"openai"}`
		got, ok := sessionModelFromLine(line)
		if ok || got != "" {
			t.Fatalf("got (%q,%v), want (\"\",false)", got, ok)
		}
	})

	t.Run("empty provider returns false", func(t *testing.T) {
		line := `{"type":"model_change","provider":"","modelId":"gpt-5.4"}`
		got, ok := sessionModelFromLine(line)
		if ok || got != "" {
			t.Fatalf("got (%q,%v), want (\"\",false)", got, ok)
		}
	})
}

func TestReadLastSessionModel(t *testing.T) {
	t.Run("nonexistent path", func(t *testing.T) {
		dir := t.TempDir()
		got, ok := readLastSessionModel(filepath.Join(dir, "does-not-exist.jsonl"))
		if ok || got != "" {
			t.Fatalf("got (%q,%v), want (\"\",false)", got, ok)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "empty.jsonl")
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		got, ok := readLastSessionModel(p)
		if ok || got != "" {
			t.Fatalf("got (%q,%v), want (\"\",false)", got, ok)
		}
	})

	t.Run("small file single model_change", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "s.jsonl")
		content := `{"type":"model_change","provider":"anthropic","modelId":"claude-opus"}` + "\n"
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		got, ok := readLastSessionModel(p)
		if !ok || got != "anthropic/claude-opus" {
			t.Fatalf("got (%q,%v), want (anthropic/claude-opus,true)", got, ok)
		}
	})

	t.Run("only non model_change events", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "n.jsonl")
		var b bytes.Buffer
		b.WriteString(`{"type":"message","text":"hi"}` + "\n")
		b.WriteString(`{"type":"tool_call","name":"x"}` + "\n")
		if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		got, ok := readLastSessionModel(p)
		if ok || got != "" {
			t.Fatalf("got (%q,%v), want (\"\",false)", got, ok)
		}
	})

	t.Run("multi chunk model_change at start", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "multi.jsonl")
		var b bytes.Buffer
		b.WriteString(`{"type":"model_change","provider":"openai","modelId":"gpt-first"}` + "\n")
		filler := `{"type":"message","text":"` + strings.Repeat("x", 200) + `"}` + "\n"
		// ~100KB of filler after the model_change.
		for b.Len() < 100*1024 {
			b.WriteString(filler)
		}
		if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		if b.Len() < 64*1024 {
			t.Fatalf("test fixture too small: %d", b.Len())
		}
		got, ok := readLastSessionModel(p)
		if !ok || got != "openai/gpt-first" {
			t.Fatalf("got (%q,%v), want (openai/gpt-first,true)", got, ok)
		}
	})

	t.Run("model_change straddles 64KB boundary", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "straddle.jsonl")
		const chunkSize = 64 * 1024
		modelLine := `{"type":"model_change","provider":"openai","modelId":"gpt-straddle"}`
		// Backward reader reads bytes [fileSize-chunkSize, fileSize) first,
		// then [max(fileSize-2*chunkSize,0), fileSize-chunkSize), etc.
		// The boundary we need the model_change line to cross is
		// fileSize - chunkSize. We construct a file of size 2*chunkSize so
		// the boundary sits at offset chunkSize. Place modelLine starting
		// ~30 bytes before chunkSize so it spans the boundary.
		var b bytes.Buffer
		prefixLine := `{"type":"noop","pad":"` + strings.Repeat("a", 100) + `"}` + "\n"
		targetStart := chunkSize - 30
		for b.Len()+len(prefixLine) <= targetStart {
			b.WriteString(prefixLine)
		}
		// Pad with a shorter no-op line ending at exactly targetStart.
		padLen := targetStart - b.Len()
		if padLen > 0 {
			// Build a JSON line of total length padLen (including trailing \n).
			// Minimum viable: `{"a":"...padding..."}\n` — needs padLen >= 9.
			if padLen < 9 {
				// Adjust: drop one prefix line and recompute.
				cur := b.Bytes()
				b.Reset()
				if len(cur) >= len(prefixLine) {
					b.Write(cur[:len(cur)-len(prefixLine)])
				}
				padLen = targetStart - b.Len()
			}
			inner := padLen - len(`{"a":"`) - len(`"}`) - 1
			if inner < 1 {
				inner = 1
			}
			padLine := `{"a":"` + strings.Repeat("z", inner) + `"}` + "\n"
			// Final tiny adjust by trimming or extending inner.
			for len(padLine) < padLen {
				inner++
				padLine = `{"a":"` + strings.Repeat("z", inner) + `"}` + "\n"
			}
			for len(padLine) > padLen && inner > 1 {
				inner--
				padLine = `{"a":"` + strings.Repeat("z", inner) + `"}` + "\n"
			}
			b.WriteString(padLine)
		}
		if b.Len() != targetStart {
			// Acceptable drift: as long as modelLine straddles the boundary,
			// we still get coverage. Log only.
			t.Logf("prefix length %d != target %d (acceptable if straddles)", b.Len(), targetStart)
		}
		b.WriteString(modelLine)
		b.WriteString("\n")
		// Pad to total size = 2 * chunkSize so boundary is at chunkSize.
		suffixLine := `{"type":"message","text":"` + strings.Repeat("y", 200) + `"}` + "\n"
		for b.Len() < 2*chunkSize {
			b.WriteString(suffixLine)
		}
		// Verify modelLine straddles the boundary; otherwise test isn't proving the property.
		data := b.Bytes()
		idx := bytes.Index(data, []byte(modelLine))
		if idx < 0 || idx >= chunkSize || idx+len(modelLine) <= chunkSize {
			t.Fatalf("modelLine at offset %d does not straddle boundary %d (line len %d)", idx, chunkSize, len(modelLine))
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		got, ok := readLastSessionModel(p)
		if !ok || got != "openai/gpt-straddle" {
			t.Fatalf("got (%q,%v), want (openai/gpt-straddle,true)", got, ok)
		}
	})

	t.Run("no trailing newline last line is model_change", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "no-nl.jsonl")
		content := `{"type":"message","text":"hi"}` + "\n" +
			`{"type":"model_change","provider":"openai","modelId":"gpt-last"}`
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		got, ok := readLastSessionModel(p)
		if !ok || got != "openai/gpt-last" {
			t.Fatalf("got (%q,%v), want (openai/gpt-last,true)", got, ok)
		}
	})
}

func TestGetSessionModel(t *testing.T) {
	t.Run("empty sessionID agent in defaults", func(t *testing.T) {
		defaults := map[string]string{"main": "openai/gpt-default"}
		got := getSessionModel(t.TempDir(), "main", "", defaults)
		if got != "openai/gpt-default" {
			t.Fatalf("got %q, want openai/gpt-default", got)
		}
	})

	t.Run("empty sessionID agent missing returns unknown", func(t *testing.T) {
		got := getSessionModel(t.TempDir(), "ghost", "", map[string]string{})
		if got != "unknown" {
			t.Fatalf("got %q, want unknown", got)
		}
	})

	t.Run("sessionID present jsonl has model_change wins over defaults", func(t *testing.T) {
		base := t.TempDir()
		agent := "main"
		sid := "sess-1"
		dir := filepath.Join(base, agent, "sessions")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := `{"type":"model_change","provider":"anthropic","modelId":"claude-sonnet"}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		defaults := map[string]string{"main": "openai/gpt-default"}
		got := getSessionModel(base, agent, sid, defaults)
		if got != "anthropic/claude-sonnet" {
			t.Fatalf("got %q, want anthropic/claude-sonnet", got)
		}
	})

	t.Run("sessionID present jsonl missing falls back to defaults", func(t *testing.T) {
		base := t.TempDir()
		defaults := map[string]string{"main": "openai/gpt-default"}
		got := getSessionModel(base, "main", "missing-sid", defaults)
		if got != "openai/gpt-default" {
			t.Fatalf("got %q, want openai/gpt-default", got)
		}
	})

	t.Run("sessionID present jsonl no model_change falls back to defaults", func(t *testing.T) {
		base := t.TempDir()
		agent := "work"
		sid := "sess-x"
		dir := filepath.Join(base, agent, "sessions")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := `{"type":"message","text":"hi"}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		defaults := map[string]string{"work": "openai/gpt-fallback"}
		got := getSessionModel(base, agent, sid, defaults)
		if got != "openai/gpt-fallback" {
			t.Fatalf("got %q, want openai/gpt-fallback", got)
		}
	})

	t.Run("sessionID present jsonl missing agent also missing returns unknown", func(t *testing.T) {
		base := t.TempDir()
		got := getSessionModel(base, "ghost", "sid", map[string]string{})
		if got != "unknown" {
			t.Fatalf("got %q, want unknown", got)
		}
	})
}
