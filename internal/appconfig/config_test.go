package appconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefault_AllFieldsPopulated(t *testing.T) {
	cfg := Default()
	if cfg.Server.Port == 0 {
		t.Error("Port should not be zero")
	}
	if cfg.Server.Host == "" {
		t.Error("Host should not be empty")
	}
	if cfg.AI.GatewayPort == 0 {
		t.Error("GatewayPort should not be zero")
	}
	if cfg.Timezone == "" {
		t.Error("Timezone should not be empty")
	}
	if cfg.Refresh.IntervalSeconds == 0 {
		t.Error("RefreshSec should not be zero")
	}
	if cfg.System.PollSeconds == 0 {
		t.Error("PollSeconds should not be zero")
	}
	if cfg.System.CPU.Warn == 0 || cfg.System.CPU.Critical == 0 {
		t.Error("CPU thresholds should not be zero")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	cfg := Load(t.TempDir())
	def := Default()
	if cfg.Server.Port != def.Server.Port {
		t.Errorf("expected default port %d, got %d", def.Server.Port, cfg.Server.Port)
	}
	if cfg.Timezone != def.Timezone {
		t.Errorf("expected default timezone %q, got %q", def.Timezone, cfg.Timezone)
	}
}

func TestLoad_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	data := `{"timezone":"US/Pacific","server":{"port":9090,"host":"0.0.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load(dir)
	if cfg.Timezone != "US/Pacific" {
		t.Errorf("expected US/Pacific, got %q", cfg.Timezone)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("expected 9090, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected 0.0.0.0, got %q", cfg.Server.Host)
	}
}

func TestLoad_PartialJSON(t *testing.T) {
	dir := t.TempDir()
	data := `{"timezone":"Asia/Tokyo"}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load(dir)
	if cfg.Timezone != "Asia/Tokyo" {
		t.Errorf("expected Asia/Tokyo, got %q", cfg.Timezone)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Refresh.IntervalSeconds != 30 {
		t.Errorf("expected default refresh 30, got %d", cfg.Refresh.IntervalSeconds)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load(dir)
	def := Default()
	if cfg.Server.Port != def.Server.Port {
		t.Errorf("expected default port %d on invalid JSON, got %d", def.Server.Port, cfg.Server.Port)
	}
}

func TestLoad_SystemThresholdClamping(t *testing.T) {
	dir := t.TempDir()
	// Set critical <= warn to trigger clamping
	data := `{"system":{"enabled":true,"warnPercent":70,"criticalPercent":50,"cpu":{"warn":80,"critical":50}}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load(dir)
	if cfg.System.CriticalPercent <= cfg.System.WarnPercent {
		t.Errorf("critical (%g) should be > warn (%g) after clamping", cfg.System.CriticalPercent, cfg.System.WarnPercent)
	}
	if cfg.System.CPU.Critical <= cfg.System.CPU.Warn {
		t.Errorf("CPU critical (%g) should be > warn (%g) after clamping", cfg.System.CPU.Critical, cfg.System.CPU.Warn)
	}
}

func TestReadDotenv_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "FOO=bar\nBAZ=qux\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := ReadDotenv(path)
	if m["FOO"] != "bar" {
		t.Errorf("expected FOO=bar, got %q", m["FOO"])
	}
	if m["BAZ"] != "qux" {
		t.Errorf("expected BAZ=qux, got %q", m["BAZ"])
	}
}

func TestReadDotenv_MissingFile(t *testing.T) {
	m := ReadDotenv("/nonexistent/.env")
	if len(m) != 0 {
		t.Errorf("expected empty map for missing file, got %d entries", len(m))
	}
}

func TestReadDotenv_Comments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# this is a comment\nKEY=val\n# another comment\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := ReadDotenv(path)
	if len(m) != 1 {
		t.Errorf("expected 1 entry, got %d", len(m))
	}
	if m["KEY"] != "val" {
		t.Errorf("expected KEY=val, got %q", m["KEY"])
	}
}

func TestReadDotenv_ExportPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "export FOO=bar\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := ReadDotenv(path)
	// ReadDotenv does not strip "export " prefix; the key includes it
	if v, ok := m["export FOO"]; !ok || v != "bar" {
		t.Errorf("expected key 'export FOO'=bar, got map %v", m)
	}
}

func TestReadDotenv_QuotedValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "A=\"hello world\"\nB='single quoted'\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := ReadDotenv(path)
	if m["A"] != "hello world" {
		t.Errorf("expected unquoted double-quote value, got %q", m["A"])
	}
	if m["B"] != "single quoted" {
		t.Errorf("expected unquoted single-quote value, got %q", m["B"])
	}
}

func TestExpandHome_WithTilde(t *testing.T) {
	result := ExpandHome("~/some/path")
	if strings.HasPrefix(result, "~/") {
		t.Errorf("tilde should be expanded, got %q", result)
	}
	if !strings.HasSuffix(result, "/some/path") {
		t.Errorf("expected path to end with /some/path, got %q", result)
	}
}

func TestExpandHome_WithoutTilde(t *testing.T) {
	result := ExpandHome("/absolute/path")
	if result != "/absolute/path" {
		t.Errorf("absolute path should be unchanged, got %q", result)
	}
}
