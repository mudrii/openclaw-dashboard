package appconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes config.json into a fresh tempdir and returns the dir.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestLoad_DeepStatusBinds verifies system.deepStatus binds to DeepStatus and
// does not trigger an unknown-key warning.
func TestLoad_DeepStatusBinds(t *testing.T) {
	dir := writeConfig(t, `{"system":{"deepStatus":true}}`)
	var cfg Config
	logs := withCapturedSlog(t, func() { cfg = Load(dir) })
	if !cfg.System.DeepStatus {
		t.Errorf("DeepStatus = false, want true")
	}
	if strings.Contains(logs, "unknown config key") {
		t.Errorf("unexpected unknown config key warning:\n%s", logs)
	}
}

// TestLoad_SystemdUnitBinds verifies logs.systemdUnit binds to SystemdUnit and
// does not trigger an unknown-key warning.
func TestLoad_SystemdUnitBinds(t *testing.T) {
	dir := writeConfig(t, `{"logs":{"systemdUnit":"foo.service"}}`)
	var cfg Config
	logs := withCapturedSlog(t, func() { cfg = Load(dir) })
	if cfg.Logs.SystemdUnit != "foo.service" {
		t.Errorf("SystemdUnit = %q, want %q", cfg.Logs.SystemdUnit, "foo.service")
	}
	if strings.Contains(logs, "unknown config key") {
		t.Errorf("unexpected unknown config key warning:\n%s", logs)
	}
}

// TestDefault_NewFieldsZero confirms the new fields default to their zero value.
func TestDefault_NewFieldsZero(t *testing.T) {
	d := Default()
	if d.System.DeepStatus {
		t.Errorf("Default DeepStatus = true, want false")
	}
	if d.Logs.SystemdUnit != "" {
		t.Errorf("Default SystemdUnit = %q, want empty", d.Logs.SystemdUnit)
	}
}

// TestLoad_CriticalPercentClamp covers the critical-vs-warn arming branch
// (config.go:341-350): a critical below warn re-arms to warn+15 (capped 100),
// and an out-of-range critical resets the same way.
func TestLoad_CriticalPercentClamp(t *testing.T) {
	tests := []struct {
		name string
		body string
		want float64
	}{
		{
			// warn 98 (<100 so kept), critical 50 <= warn → warn>=95 branch → 100.
			name: "criticalBelowWarn_highWarn_clampsTo100",
			body: `{"system":{"warnPercent":98,"criticalPercent":50}}`,
			want: 100,
		},
		{
			// critical 150 > 100 → reset; warn 70 < 95 → warn+15 = 85.
			name: "criticalAbove100_resetsToWarnPlus15",
			body: `{"system":{"warnPercent":70,"criticalPercent":150}}`,
			want: 85,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Load(writeConfig(t, tc.body))
			if cfg.System.CriticalPercent != tc.want {
				t.Errorf("CriticalPercent = %v, want %v", cfg.System.CriticalPercent, tc.want)
			}
		})
	}
}

// TestLoad_LogsUpperBoundClamps covers the upper-bound resets (config.go:307-318).
func TestLoad_LogsUpperBoundClamps(t *testing.T) {
	cfg := Load(writeConfig(t, `{"logs":{"tailLines":2000,"maxErrorSignatures":20000,"errorWindowHours":200}}`))
	if cfg.Logs.TailLines != 200 {
		t.Errorf("TailLines = %d, want 200", cfg.Logs.TailLines)
	}
	if cfg.Logs.MaxErrorSignatures != 1000 {
		t.Errorf("MaxErrorSignatures = %d, want 1000", cfg.Logs.MaxErrorSignatures)
	}
	if cfg.Logs.ErrorWindowHours != 24 {
		t.Errorf("ErrorWindowHours = %d, want 24", cfg.Logs.ErrorWindowHours)
	}
}

// TestReadDotenv_EdgeCases covers line-parsing edge cases: no '=', empty key,
// values containing '=', and quoted vs unquoted values.
func TestReadDotenv_EdgeCases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := strings.Join([]string{
		"noequalshere",         // skipped: no '='
		"=orphanvalue",         // skipped: empty key
		"PAIR=a=b=c",           // value containing '='
		`QUOTED="hello world"`, // double-quoted, stripped
		"SINGLE='single val'",  // single-quoted, stripped
		"PLAIN=plainval",       // unquoted
		"# comment",            // skipped
		"",                     // skipped
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ReadDotenv(path)

	if _, ok := got["noequalshere"]; ok {
		t.Errorf("line without '=' should be skipped, got key present")
	}
	if _, ok := got[""]; ok {
		t.Errorf("empty key should be skipped, got key present")
	}
	want := map[string]string{
		"PAIR":   "a=b=c",
		"QUOTED": "hello world",
		"SINGLE": "single val",
		"PLAIN":  "plainval",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("ReadDotenv[%q] = %q, want %q", k, got[k], v)
		}
	}
}
