package appconfig

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// withCapturedSlog redirects slog default output for the duration of fn.
// Returns whatever was logged so tests can assert on warnings.
func withCapturedSlog(t *testing.T, fn func()) string {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	fn()
	return buf.String()
}

// TestLoad_WarnsOnUnknownField verifies that a typo in config.json
// (e.g. "gatewayPot" instead of "gatewayPort") emits a warning identifying
// the offending field while still loading the rest of the config with
// defaults. Catches silent config drift.
func TestLoad_WarnsOnUnknownField(t *testing.T) {
	dir := t.TempDir()
	data := `{"timezone":"Europe/Berlin","ai":{"gatewayPot":12345}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	logs := withCapturedSlog(t, func() {
		cfg = Load(dir)
	})

	// Behavior under test: an unknown field is ignored and does not abort the
	// load. Known sibling fields still bind; the mistyped field leaves its
	// target at the default.
	if cfg.Timezone != "Europe/Berlin" {
		t.Errorf("expected Timezone=Europe/Berlin (loaded despite unknown sibling), got %q", cfg.Timezone)
	}
	if cfg.AI.GatewayPort != 18789 {
		t.Errorf("expected default GatewayPort=18789 (typo did not bind), got %d", cfg.AI.GatewayPort)
	}
	if !strings.Contains(logs, "[dashboard] unknown config key") || !strings.Contains(logs, "field=ai.gatewayPot") {
		t.Fatalf("unknown-key warning missing field path; logs:\n%s", logs)
	}
}

func TestConfigurationGuideFullExampleMatchesConfigSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "CONFIGURATION.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "### Full Example")
	if start < 0 {
		t.Fatal("Full Example heading not found")
	}
	codeStart := strings.Index(text[start:], "```json")
	if codeStart < 0 {
		t.Fatal("Full Example JSON block not found")
	}
	codeStart = start + codeStart + len("```json")
	codeEnd := strings.Index(text[codeStart:], "```")
	if codeEnd < 0 {
		t.Fatal("Full Example JSON block is unterminated")
	}
	var example map[string]any
	if err := json.Unmarshal([]byte(text[codeStart:codeStart+codeEnd]), &example); err != nil {
		t.Fatalf("Full Example JSON is invalid: %v", err)
	}

	want := jsonFields(reflect.TypeOf(Config{}))
	for key := range want {
		if _, ok := example[key]; !ok {
			t.Errorf("Full Example missing top-level config key %q", key)
		}
	}
	for key := range example {
		if _, ok := want[key]; !ok {
			t.Errorf("Full Example includes unknown top-level key %q", key)
		}
	}
}
