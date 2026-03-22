package dashboard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	cfg := loadConfig(dir) // no config.json — should use defaults

	if cfg.Server.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Fatalf("expected default host 127.0.0.1, got %s", cfg.Server.Host)
	}
	if cfg.AI.MaxHistory != 6 {
		t.Fatalf("expected default MaxHistory 6, got %d", cfg.AI.MaxHistory)
	}
	if cfg.Refresh.IntervalSeconds != 30 {
		t.Fatalf("expected default interval 30, got %d", cfg.Refresh.IntervalSeconds)
	}
	if cfg.Theme.Preset != "midnight" {
		t.Fatalf("expected default theme midnight, got %s", cfg.Theme.Preset)
	}
}

func TestLoadConfig_Override(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{
		"server": {"port": 9090, "host": "0.0.0.0"},
		"theme": {"preset": "solar"},
		"refresh": {"intervalSeconds": 60}
	}`), 0644)

	cfg := loadConfig(dir)

	if cfg.Server.Port != 9090 {
		t.Fatalf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Fatalf("expected host 0.0.0.0, got %s", cfg.Server.Host)
	}
	if cfg.Theme.Preset != "solar" {
		t.Fatalf("expected theme solar, got %s", cfg.Theme.Preset)
	}
	if cfg.Refresh.IntervalSeconds != 60 {
		t.Fatalf("expected interval 60, got %d", cfg.Refresh.IntervalSeconds)
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{bad json`), 0644)

	cfg := loadConfig(dir)

	// Should fall back to defaults
	if cfg.Server.Port != 8080 {
		t.Fatalf("invalid JSON should use default port, got %d", cfg.Server.Port)
	}
}

func TestLoadConfig_ZeroValuesClamped(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{
		"server": {"port": 0},
		"ai": {"maxHistory": -1, "gatewayPort": 0},
		"refresh": {"intervalSeconds": -5}
	}`), 0644)

	cfg := loadConfig(dir)

	if cfg.Server.Port != 8080 {
		t.Fatalf("zero port should clamp to 8080, got %d", cfg.Server.Port)
	}
	if cfg.AI.MaxHistory != 6 {
		t.Fatalf("negative maxHistory should clamp to 6, got %d", cfg.AI.MaxHistory)
	}
	if cfg.AI.GatewayPort != 18789 {
		t.Fatalf("zero gateway port should clamp to 18789, got %d", cfg.AI.GatewayPort)
	}
	if cfg.Refresh.IntervalSeconds != 30 {
		t.Fatalf("negative interval should clamp to 30, got %d", cfg.Refresh.IntervalSeconds)
	}
}

func TestReadDotenv_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte("KEY1=value1\nKEY2=value2\n"), 0644)

	env := readDotenv(path)

	if env["KEY1"] != "value1" {
		t.Fatalf("expected value1, got %q", env["KEY1"])
	}
	if env["KEY2"] != "value2" {
		t.Fatalf("expected value2, got %q", env["KEY2"])
	}
}

func TestReadDotenv_CommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte("# comment\n\nKEY=val\n"), 0644)

	env := readDotenv(path)

	if len(env) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(env))
	}
	if env["KEY"] != "val" {
		t.Fatalf("expected val, got %q", env["KEY"])
	}
}

func TestReadDotenv_EqualsInValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte("TOKEN=abc=def=ghi\n"), 0644)

	env := readDotenv(path)

	if env["TOKEN"] != "abc=def=ghi" {
		t.Fatalf("expected abc=def=ghi, got %q", env["TOKEN"])
	}
}

func TestReadDotenv_QuotedValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(`KEY="quoted value"`+"\n"), 0644)

	env := readDotenv(path)

	// readDotenv should strip surrounding quotes
	if env["KEY"] != "quoted value" {
		t.Fatalf("expected 'quoted value' (without quotes), got %q", env["KEY"])
	}
}

func TestReadDotenv_SingleQuotedValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(`KEY='single quoted'`+"\n"), 0644)

	env := readDotenv(path)

	if env["KEY"] != "single quoted" {
		t.Fatalf("expected 'single quoted' (without quotes), got %q", env["KEY"])
	}
}

func TestReadDotenv_MissingFile(t *testing.T) {
	env := readDotenv("/nonexistent/.env")

	if len(env) != 0 {
		t.Fatal("missing file should return empty map")
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot get home dir")
	}

	result := expandHome("~/test/path")
	expected := filepath.Join(home, "test/path")
	if result != expected {
		t.Fatalf("expected %s, got %s", expected, result)
	}

	// Non-home path should be unchanged
	result2 := expandHome("/absolute/path")
	if result2 != "/absolute/path" {
		t.Fatalf("expected /absolute/path, got %s", result2)
	}
}
