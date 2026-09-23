package appconfig

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestLoad_LegacyLogAliasesAlone verifies the snake_case aliases apply when the
// canonical keys are simply omitted (the common legacy config shape), even
// though Default() pre-fills the canonical fields.
func TestLoad_LegacyLogAliasesAlone(t *testing.T) {
	dir := writeConfig(t, `{"logs":{"log_sources":["x.log"],"log_tail_lines":500,"log_fast_refresh_ms":5000,"error_feed_window_hours":48}}`)
	cfg := Load(dir)
	if !slices.Equal(cfg.Logs.Sources, []string{"x.log"}) {
		t.Errorf("Logs.Sources = %v, want [x.log]", cfg.Logs.Sources)
	}
	if cfg.Logs.TailLines != 500 {
		t.Errorf("Logs.TailLines = %d, want 500", cfg.Logs.TailLines)
	}
	if cfg.Logs.FastRefreshMs != 5000 {
		t.Errorf("Logs.FastRefreshMs = %d, want 5000", cfg.Logs.FastRefreshMs)
	}
	if cfg.Logs.ErrorWindowHours != 48 {
		t.Errorf("Logs.ErrorWindowHours = %d, want 48", cfg.Logs.ErrorWindowHours)
	}
}

// TestLoad_CanonicalLogKeysBeatAliases verifies a present canonical key wins
// over its legacy alias.
func TestLoad_CanonicalLogKeysBeatAliases(t *testing.T) {
	dir := writeConfig(t, `{"logs":{"sources":["canon.log"],"tailLines":300,"fastRefreshMs":2000,"errorWindowHours":12,`+
		`"log_sources":["x.log"],"log_tail_lines":500,"log_fast_refresh_ms":5000,"error_feed_window_hours":48}}`)
	cfg := Load(dir)
	if !slices.Equal(cfg.Logs.Sources, []string{"canon.log"}) || cfg.Logs.TailLines != 300 ||
		cfg.Logs.FastRefreshMs != 2000 || cfg.Logs.ErrorWindowHours != 12 {
		t.Fatalf("canonical keys lost to aliases: %+v", cfg.Logs)
	}
}

func TestLoad_MaxHistoryClamp(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want int
	}{
		{"omitted", `{}`, 6},
		{"zero", `{"ai":{"maxHistory":0}}`, 6},
		{"negative", `{"ai":{"maxHistory":-3}}`, 6},
		{"min", `{"ai":{"maxHistory":1}}`, 1},
		{"in range", `{"ai":{"maxHistory":20}}`, 20},
		{"max", `{"ai":{"maxHistory":50}}`, 50},
		{"above max", `{"ai":{"maxHistory":51}}`, 50},
		{"huge", `{"ai":{"maxHistory":1000000}}`, 50},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Load(writeConfig(t, tt.body)).AI.MaxHistory; got != tt.want {
				t.Fatalf("AI.MaxHistory = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestLoad_UnreadableConfigDoesNotFallBack verifies that only a missing
// config.json falls back to assets/runtime/config.json; any other open error
// uses defaults and warns instead of silently loading a different file.
func TestLoad_UnreadableConfigDoesNotFallBack(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	dir := writeConfig(t, `{"timezone":"Europe/Berlin"}`)
	path := filepath.Join(dir, "config.json")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	runtimeDir := filepath.Join(dir, "assets", "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.json"), []byte(`{"timezone":"US/Eastern"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	logs := withCapturedSlog(t, func() { cfg = Load(dir) })
	if cfg.Timezone != Default().Timezone {
		t.Fatalf("Timezone = %q, want default %q (no fallback on permission error)", cfg.Timezone, Default().Timezone)
	}
	if !strings.Contains(logs, "config.json") || !strings.Contains(logs, "level=WARN") {
		t.Fatalf("missing warning for unreadable config; logs:\n%s", logs)
	}
}

func TestReadDotenv_InlineCommentsAndQuotes(t *testing.T) {
	body := strings.Join([]string{
		"A=abc # comment",
		`B="q" # c`,
		`C='single # kept' # c`,
		"D=abc\t# tab comment",
		"E=pass#word",
		"F=#leading",
		"G= # only comment",
		`H="unterminated`,
		`I="a#b"`,
		"export\tJ=tab-export",
		"exportK=literal",
		"notexport=val",
		`L="with "inner" quote"`,
		"M=a=b # c",
		`N="ab\"cd" # c`,
	}, "\n")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ReadDotenv(path)
	for key, want := range map[string]string{
		"A":         "abc",
		"B":         "q",
		"C":         "single # kept",
		"D":         "abc",
		"E":         "pass#word",
		"F":         "#leading",
		"G":         "",
		"H":         `"unterminated`,
		"I":         "a#b",
		"J":         "tab-export",
		"exportK":   "literal",
		"notexport": "val",
		"L":         "with ",
		"M":         "a=b",
		"N":         `ab\"cd`,
	} {
		if v, ok := got[key]; !ok || v != want {
			t.Errorf("ReadDotenv[%q] = %q (present=%v), want %q", key, v, ok, want)
		}
	}
}

func TestGatewayTokenFromJSON5Config(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "")
	root := t.TempDir()
	body := "{\n  // gateway\n  \"gateway\": {\"auth\": {\"token\": \"json5-token\",},},\n}\n"
	if err := os.WriteFile(filepath.Join(root, "openclaw.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveGatewayToken(filepath.Join(root, "missing.env"), root); got != "json5-token" {
		t.Fatalf("token = %q, want json5-token", got)
	}
}

func TestGatewayTokenWarnsOnUnparseableConfig(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "")
	root := t.TempDir()
	path := filepath.Join(root, "openclaw.json")
	if err := os.WriteFile(path, []byte("{gateway: {auth: {token: 'x'}}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	var token string
	logs := withCapturedSlog(t, func() { token = ResolveGatewayToken(filepath.Join(root, "missing.env"), root) })
	if token != "" {
		t.Fatalf("token = %q, want empty", token)
	}
	if !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, path) {
		t.Fatalf("missing parse warning with path; logs:\n%s", logs)
	}
}
