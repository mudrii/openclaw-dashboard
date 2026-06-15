package apprefresh

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

func TestGetString(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"string trimmed", "  hello  ", "hello"},
		{"empty string", "", ""},
		{"json.Number", json.Number("42"), "42"},
		// float64 formatted with -1 precision: no exponent, no trailing zeros.
		{"float64 integral", float64(1000), "1000"},
		{"float64 fractional", float64(3.5), "3.5"},
		{"float32", float32(2.5), "2.5"},
		{"int", 7, "7"},
		{"int64", int64(99), "99"},
		// Unsupported types (bool, slice, map) return "".
		{"bool unsupported", true, ""},
		{"slice unsupported", []any{1, 2}, ""},
		{"map unsupported", map[string]any{"a": 1}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := getString(tc.in); got != tc.want {
				t.Errorf("getString(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPickFirst(t *testing.T) {
	t.Run("returns first non-empty in key order", func(t *testing.T) {
		obj := map[string]any{"a": "", "b": "found", "c": "later"}
		if got := pickFirst(obj, "a", "b", "c"); got != "found" {
			t.Errorf("want found, got %q", got)
		}
	})

	t.Run("skips empty and missing keys", func(t *testing.T) {
		obj := map[string]any{"a": "", "c": "value"}
		if got := pickFirst(obj, "a", "missing", "c"); got != "value" {
			t.Errorf("want value, got %q", got)
		}
	})

	t.Run("stringifies numeric values", func(t *testing.T) {
		obj := map[string]any{"level": float64(3)}
		if got := pickFirst(obj, "level"); got != "3" {
			t.Errorf("want 3, got %q", got)
		}
	})

	t.Run("all empty yields empty", func(t *testing.T) {
		obj := map[string]any{"a": "", "b": ""}
		if got := pickFirst(obj, "a", "b"); got != "" {
			t.Errorf("want empty, got %q", got)
		}
	})
}

func TestPickLogMessage(t *testing.T) {
	t.Run("key order precedence", func(t *testing.T) {
		// message > msg > text > error > err > event > details > content.
		obj := map[string]any{"msg": "from msg", "message": "from message"}
		if got := pickLogMessage(obj); got != "from message" {
			t.Errorf("want from message (highest precedence), got %q", got)
		}
	})

	t.Run("falls through to later keys", func(t *testing.T) {
		obj := map[string]any{"event": "evt", "details": "det"}
		if got := pickLogMessage(obj); got != "evt" {
			t.Errorf("want evt (event before details), got %q", got)
		}
	})

	t.Run("whitespace-only values skipped", func(t *testing.T) {
		obj := map[string]any{"message": "   ", "msg": "real"}
		if got := pickLogMessage(obj); got != "real" {
			t.Errorf("want real, got %q", got)
		}
	})

	t.Run("string value trimmed", func(t *testing.T) {
		obj := map[string]any{"text": "  trimmed  "}
		if got := pickLogMessage(obj); got != "trimmed" {
			t.Errorf("want trimmed, got %q", got)
		}
	})

	t.Run("nested object content/text/message", func(t *testing.T) {
		obj := map[string]any{"content": map[string]any{"text": "nested text"}}
		if got := pickLogMessage(obj); got != "nested text" {
			t.Errorf("want nested text, got %q", got)
		}
	})

	t.Run("nested object content key precedence", func(t *testing.T) {
		obj := map[string]any{"error": map[string]any{
			"message": "nested message",
			"text":    "nested text",
		}}
		// nested order is content, text, message → text wins over message here.
		if got := pickLogMessage(obj); got != "nested text" {
			t.Errorf("want nested text (text before message), got %q", got)
		}
	})

	t.Run("no usable keys yields empty", func(t *testing.T) {
		obj := map[string]any{"other": "x", "level": float64(1)}
		if got := pickLogMessage(obj); got != "" {
			t.Errorf("want empty, got %q", got)
		}
	})
}

func TestParseJSONLLine(t *testing.T) {
	zero := time.Time{}

	t.Run("well-formed level/msg/timestamp", func(t *testing.T) {
		line := `{"timestamp":"2026-05-01T10:00:00Z","level":"warn","message":"cache rebuilt"}`
		rec, ok := parseJSONLLine(line, zero)
		if !ok {
			t.Fatalf("want ok=true")
		}
		if rec.Message != "cache rebuilt" {
			t.Errorf("Message = %q, want cache rebuilt", rec.Message)
		}
		if rec.Severity != "warn" {
			t.Errorf("Severity = %q, want warn", rec.Severity)
		}
		if rec.Timestamp.IsZero() {
			t.Errorf("Timestamp should be parsed, got zero")
		}
	})

	t.Run("malformed JSON falls back to plain, ok=true", func(t *testing.T) {
		line := `not json at all`
		rec, ok := parseJSONLLine(line, zero)
		if !ok {
			t.Fatalf("malformed line should still return ok=true (plain fallback)")
		}
		if rec.Message != "not json at all" {
			t.Errorf("Message = %q, want plain fallback", rec.Message)
		}
	})

	t.Run("missing timestamp uses fallback", func(t *testing.T) {
		fallback := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
		line := `{"level":"error","message":"boom"}`
		rec, ok := parseJSONLLine(line, fallback)
		if !ok {
			t.Fatalf("want ok=true")
		}
		if !rec.Timestamp.Equal(fallback) {
			t.Errorf("Timestamp = %v, want fallback %v", rec.Timestamp, fallback)
		}
		if rec.Severity != "error" {
			t.Errorf("Severity = %q, want error", rec.Severity)
		}
	})

	t.Run("severity key precedence: severity over level", func(t *testing.T) {
		line := `{"severity":"debug","level":"error","message":"hi"}`
		rec, ok := parseJSONLLine(line, zero)
		if !ok {
			t.Fatalf("want ok=true")
		}
		if rec.Severity != "debug" {
			t.Errorf("Severity = %q, want debug (severity key wins over level)", rec.Severity)
		}
	})

	t.Run("empty message defaults to raw line", func(t *testing.T) {
		line := `{"level":"info","other":"x"}`
		rec, ok := parseJSONLLine(line, zero)
		if !ok {
			t.Fatalf("want ok=true")
		}
		if rec.Message != line {
			t.Errorf("Message = %q, want raw line %q", rec.Message, line)
		}
	})
}

// TestReadMergedLogs_JSONLIntegration exercises the .jsonl parse path end to
// end through the public ReadMergedLogs entrypoint.
func TestReadMergedLogs_JSONLIntegration(t *testing.T) {
	openclawDir := t.TempDir()
	SetLogFallbackRoots(func() []string { return nil })
	t.Cleanup(func() { SetLogFallbackRoots(nil) })

	path := filepath.Join(openclawDir, "logs", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-05-01T10:00:00Z","level":"info","message":"started"}`,
		`{"timestamp":"2026-05-01T10:00:05Z","level":"error","message":"crashed"}`,
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := ReadMergedLogs(openclawDir, []string{"logs/events.jsonl"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("want 2 records, got %d: %+v", len(records), records)
	}
	if records[0].Message != "started" || records[1].Message != "crashed" {
		t.Errorf("messages = %q,%q want started,crashed", records[0].Message, records[1].Message)
	}
	if records[1].Severity != "error" {
		t.Errorf("second severity = %q, want error", records[1].Severity)
	}
}

func TestGetEffectiveLogSources_ReturnsIndependentCopy(t *testing.T) {
	cfg := appconfig.Config{}
	cfg.Logs.Sources = []string{"logs/gateway.log", "logs/gateway.err.log"}

	got := GetEffectiveLogSources(cfg)
	if len(got) != 2 {
		t.Fatalf("want 2 sources, got %d", len(got))
	}
	got[0] = "mutated"

	again := GetEffectiveLogSources(cfg)
	if again[0] != "logs/gateway.log" {
		t.Errorf("mutating result leaked into cfg: got %q", again[0])
	}
	if cfg.Logs.Sources[0] != "logs/gateway.log" {
		t.Errorf("cfg.Logs.Sources mutated: got %q", cfg.Logs.Sources[0])
	}
}

func TestGetLogRuntimeConfig(t *testing.T) {
	cfg := appconfig.Config{}
	cfg.Logs.Sources = []string{"logs/gateway.log"}
	cfg.Refresh.IntervalSeconds = 30

	got := GetLogRuntimeConfig(cfg)

	if got["logRefreshIntervalMs"] != 30*1000 {
		t.Errorf("logRefreshIntervalMs = %v, want %d", got["logRefreshIntervalMs"], 30*1000)
	}
	sources, ok := got["logSources"].([]string)
	if !ok {
		t.Fatalf("logSources type = %T, want []string", got["logSources"])
	}
	want := GetEffectiveLogSources(cfg)
	if len(sources) != len(want) || sources[0] != want[0] {
		t.Errorf("logSources = %v, want %v", sources, want)
	}
}
