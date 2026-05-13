//go:build darwin

package appsystem

import "testing"

// C9b: parseTopCPU must return an error (not panic) when the matched line
// somehow lacks a capture group. Defense-in-depth against future regex changes.
func TestParseTopCPU_NoMatchReturnsError(t *testing.T) {
	out := "no cpu usage line at all\nfoo bar baz\n"
	_, err := parseTopCPU(out)
	if err == nil {
		t.Fatal("expected error for output without CPU usage line")
	}
}

func TestParseTopCPU_ValidIntegerIdle(t *testing.T) {
	out := "Processes: 1 total\nCPU usage: 0% user, 0% sys, 100% idle\n"
	pct, err := parseTopCPU(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pct != 0.0 {
		t.Fatalf("pct = %v, want 0", pct)
	}
}

func TestParseTopCPU_ValidFractionalIdle(t *testing.T) {
	// Two samples — last should win.
	out := "CPU usage: 50.00% user, 0% sys, 50.00% idle\nCPU usage: 10% user, 5% sys, 84.21% idle\n"
	pct, err := parseTopCPU(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := 15.8 // 100 - 84.21 = 15.79 → rounded to 15.8
	if pct != want {
		t.Fatalf("pct = %v, want %v", pct, want)
	}
}
