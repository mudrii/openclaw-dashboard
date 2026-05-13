package appservice

import (
	"strings"
	"testing"
	"time"
)

func TestFormatStatus_running(t *testing.T) {
	st := ServiceStatus{
		Running:   true,
		PID:       12345,
		Uptime:    3*time.Hour + 12*time.Minute,
		Port:      8080,
		AutoStart: true,
		Backend:   "LaunchAgent",
		LogLines:  []string{"[dashboard] started", "[dashboard] ready"},
	}
	got := FormatStatus("v2026.3.23", st)

	for _, want := range []string{
		"openclaw-dashboard v2026.3.23",
		"Status:     running",
		"PID:        12345",
		"Uptime:     3h 12m",
		"Port:       8080",
		"Auto-start: enabled (LaunchAgent)",
		"--- recent log ---",
		"[dashboard] started",
		"[dashboard] ready",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatStatus missing %q\ngot:\n%s", want, got)
		}
	}
}

func TestFormatStatus_stopped(t *testing.T) {
	st := ServiceStatus{
		Running:   false,
		AutoStart: false,
		Backend:   "LaunchAgent",
	}
	got := FormatStatus("v2026.3.23", st)

	for _, want := range []string{
		"Status:     stopped",
		"Auto-start: disabled",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatStatus missing %q\ngot:\n%s", want, got)
		}
	}
	if strings.Contains(got, "PID:") {
		t.Errorf("stopped status should not show PID, got:\n%s", got)
	}
	if strings.Contains(got, "Uptime:") {
		t.Errorf("stopped status should not show Uptime, got:\n%s", got)
	}
}

func TestFormatStatus_noLogLines(t *testing.T) {
	st := ServiceStatus{Running: true, PID: 1}
	got := FormatStatus("v1.0", st)
	if strings.Contains(got, "recent log") {
		t.Errorf("should not show log section with no log lines, got:\n%s", got)
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"hours and minutes", 3*time.Hour + 12*time.Minute, "3h 12m"},
		{"minutes only", 45 * time.Minute, "45m"},
		{"zero", 0, "0s"},
		{"negative", -1 * time.Second, "—"},
		{"seconds only", 30 * time.Second, "30s"},
		{"exact hour", 1 * time.Hour, "1h 0m"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatUptime(tc.d)
			if got != tc.want {
				t.Errorf("formatUptime(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}
