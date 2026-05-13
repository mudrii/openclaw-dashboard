package appconfig

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
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

	if !strings.Contains(logs, "unknown config key") {
		t.Fatalf("expected 'unknown config key' warning, got logs:\n%s", logs)
	}
	if !strings.Contains(logs, "gatewayPot") {
		t.Fatalf("expected warning to name the unknown field 'gatewayPot', got logs:\n%s", logs)
	}
	// Known sibling fields should still load.
	if cfg.Timezone != "Europe/Berlin" {
		t.Errorf("expected Timezone=Europe/Berlin (loaded despite unknown sibling), got %q", cfg.Timezone)
	}
	// Unknown field doesn't populate anything; defaults apply.
	if cfg.AI.GatewayPort != 18789 {
		t.Errorf("expected default GatewayPort=18789 (typo did not bind), got %d", cfg.AI.GatewayPort)
	}
}
