package appconfig

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestColdPathTimeoutMs_Clamp pins the accepted range for ColdPathTimeoutMs:
// 200ms to 30000ms inclusive. Out-of-range values fall back to 8000ms. The
// 30s upper bound (raised from 15s, see issue #31) accommodates runtimes
// where openclaw status --json is wrapped in docker exec and takes ~16s.
// Default raised from 4000 to 8000 to survive busy hosts where `top -l 2`
// can exceed the previous 4s budget.
func TestColdPathTimeoutMs_Clamp(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"below 200 resets to default", 199, 8000},
		{"200 accepted (lower bound)", 200, 200},
		{"in-range value preserved", 5000, 5000},
		{"previously rejected 16000 now accepted", 16000, 16000},
		{"upper bound 30000 accepted", 30000, 30000},
		{"above 30000 resets to default", 30001, 8000},
		{"absurd value resets to default", 999999, 8000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			content := []byte(`{"system":{"coldPathTimeoutMs":` + strconv.Itoa(tc.in) + `}}`)
			if err := os.WriteFile(filepath.Join(dir, "config.json"), content, 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			cfg := Load(dir)
			if cfg.System.ColdPathTimeoutMs != tc.want {
				t.Errorf("input %d: want %d, got %d", tc.in, tc.want, cfg.System.ColdPathTimeoutMs)
			}
		})
	}
}

func TestCPUTimeoutMs_Clamp(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"below 500 resets to default", 499, 6000},
		{"500 accepted (lower bound)", 500, 500},
		{"in-range value preserved", 7000, 7000},
		{"upper bound 20000 accepted", 20000, 20000},
		{"above 20000 resets to default", 20001, 6000},
		{"absurd value resets to default", 999999, 6000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			content := []byte(`{"system":{"cpuTimeoutMs":` + strconv.Itoa(tc.in) + `}}`)
			if err := os.WriteFile(filepath.Join(dir, "config.json"), content, 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			cfg := Load(dir)
			if cfg.System.CPUTimeoutMs != tc.want {
				t.Errorf("input %d: want %d, got %d", tc.in, tc.want, cfg.System.CPUTimeoutMs)
			}
		})
	}
}
