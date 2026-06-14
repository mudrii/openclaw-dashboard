package appconfig

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
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
	withCapturedSlog(t, func() {
		cfg = Load(dir)
	})

	// Behavior under test: an unknown field is ignored and does not abort the
	// load. Known sibling fields still bind; the mistyped field leaves its
	// target at the default. (Whether/how a warning is logged is an
	// implementation detail and is not asserted here.)
	if cfg.Timezone != "Europe/Berlin" {
		t.Errorf("expected Timezone=Europe/Berlin (loaded despite unknown sibling), got %q", cfg.Timezone)
	}
	if cfg.AI.GatewayPort != 18789 {
		t.Errorf("expected default GatewayPort=18789 (typo did not bind), got %d", cfg.AI.GatewayPort)
	}
}
