package appconfig

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestLoad_EmptySourcesFallsBackToDefaults verifies that an explicitly empty
// logs.sources array is treated like an omitted one: it falls back to the
// built-in defaults so the log feed is never left with zero sources.
func TestLoad_EmptySourcesFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	data := `{"logs":{"sources":[]}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load(dir)
	want := Default().Logs.Sources
	if len(want) == 0 {
		t.Fatal("precondition: default sources must be non-empty")
	}
	if !slices.Equal(cfg.Logs.Sources, want) {
		t.Errorf("Logs.Sources = %v, want default %v", cfg.Logs.Sources, want)
	}
}

// TestLoad_LegacyLogAliases verifies the snake_case legacy aliases populate the
// canonical camelCase fields when the canonical ones are absent/empty.
func TestLoad_LegacyLogAliases(t *testing.T) {
	dir := t.TempDir()
	// The canonical fields must be explicitly zeroed for the legacy aliases to
	// take effect — Default() seeds them non-empty/non-zero, and an omitted key
	// would leave those defaults intact (the alias only fires on <=0 / empty).
	data := `{"logs":{"sources":[],"tailLines":0,"fastRefreshMs":0,"errorWindowHours":0,"log_sources":["a.log"],"log_tail_lines":321,"log_fast_refresh_ms":4321,"error_feed_window_hours":12}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load(dir)
	if !slices.Equal(cfg.Logs.Sources, []string{"a.log"}) {
		t.Errorf("Logs.Sources = %v, want [a.log] from log_sources alias", cfg.Logs.Sources)
	}
	if cfg.Logs.TailLines != 321 {
		t.Errorf("Logs.TailLines = %d, want 321 from log_tail_lines alias", cfg.Logs.TailLines)
	}
	if cfg.Logs.FastRefreshMs != 4321 {
		t.Errorf("Logs.FastRefreshMs = %d, want 4321 from log_fast_refresh_ms alias", cfg.Logs.FastRefreshMs)
	}
	if cfg.Logs.ErrorWindowHours != 12 {
		t.Errorf("Logs.ErrorWindowHours = %d, want 12 from error_feed_window_hours alias", cfg.Logs.ErrorWindowHours)
	}
}

// TestLoad_DoubleOpenFromAssetsRuntime verifies the second open path: when no
// top-level config.json exists but <dir>/assets/runtime/config.json does, it is
// loaded.
func TestLoad_DoubleOpenFromAssetsRuntime(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "assets", "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"timezone":"US/Eastern"}`
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load(dir)
	if cfg.Timezone != "US/Eastern" {
		t.Errorf("Timezone = %q, want US/Eastern loaded from assets/runtime/config.json", cfg.Timezone)
	}
}

// TestLoad_ReadErrorFallsBackToDefaults verifies that a config.json that is a
// directory (so os.Open succeeds but io.ReadAll fails) falls back to defaults
// without panicking.
func TestLoad_ReadErrorFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	// Make config.json a directory: Open succeeds, ReadAll returns an error.
	if err := os.Mkdir(filepath.Join(dir, "config.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Load(dir)
	def := Default()
	if cfg.Server.Port != def.Server.Port {
		t.Errorf("Server.Port = %d, want default %d on read error", cfg.Server.Port, def.Server.Port)
	}
	if cfg.Timezone != def.Timezone {
		t.Errorf("Timezone = %q, want default %q on read error", cfg.Timezone, def.Timezone)
	}
}
