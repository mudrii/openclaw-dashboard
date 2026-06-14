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

// TestParseTopCPU_LastSampleWins makes the "last sample wins" contract fail
// loudly if regressed: the first sample (0% idle → 100% busy) and the last
// sample (90% idle → 10% busy) yield clearly different results, so picking the
// wrong one is unmistakable.
func TestParseTopCPU_LastSampleWins(t *testing.T) {
	out := "CPU usage: 100% user, 0% sys, 0% idle\nCPU usage: 5% user, 5% sys, 90% idle\n"
	pct, err := parseTopCPU(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const want = 10.0 // last sample: 100 - 90 = 10
	if pct != want {
		t.Fatalf("pct = %v, want %v (first-sample selection would give 100)", pct, want)
	}
}

// TestParseVmStatUsed covers the page-size and page-count arithmetic:
// used = (active + wired + compressed) * pageSize.
func TestParseVmStatUsed(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		want    int64
		wantErr bool
	}{
		{
			name: "explicit 16384 page size honored",
			out: "Mach Virtual Memory Statistics: (page size of 16384 bytes)\n" +
				"Pages active:                            100.\n" +
				"Pages wired down:                         50.\n" +
				"Pages occupied by compressor:             10.\n",
			want: (100 + 50 + 10) * 16384,
		},
		{
			name: "default 4096 page size when absent",
			out: "Pages active:                            100.\n" +
				"Pages wired down:                         50.\n" +
				"Pages occupied by compressor:             10.\n",
			want: (100 + 50 + 10) * 4096,
		},
		{
			name: "partial fields — missing compressor counts as zero",
			out: "Pages active: 100\n" +
				"Pages wired down: 50\n",
			want: (100 + 50) * 4096,
		},
		{
			name: "all-zero pages returns error",
			out: "Pages active: 0\n" +
				"Pages wired down: 0\n" +
				"Pages occupied by compressor: 0\n",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVmStatUsed(tc.out)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("used = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestParseSwapUsage(t *testing.T) {
	const mib = 1024 * 1024
	tests := []struct {
		name      string
		out       string
		wantUsed  int64
		wantTotal int64
		wantErr   bool
	}{
		{
			name:      "megabytes",
			out:       "vm.swapusage: total = 4096.00M  used = 512.00M  free = 3584.00M",
			wantUsed:  512 * mib,
			wantTotal: 4096 * mib,
		},
		{
			name:      "gigabytes",
			out:       "vm.swapusage: total = 2.00G  used = 1.00G  free = 1.00G",
			wantUsed:  2 * 1024 * mib / 2,
			wantTotal: 2 * 1024 * mib,
		},
		{
			name:      "terabytes",
			out:       "vm.swapusage: total = 2.00T  used = 1.00T  free = 1.00T",
			wantUsed:  1 * 1024 * 1024 * mib,
			wantTotal: 2 * 1024 * 1024 * mib,
		},
		{
			name:      "used exceeds total is clamped",
			out:       "vm.swapusage: total = 1024.00M  used = 2048.00M  free = 0.00M",
			wantUsed:  1024 * mib,
			wantTotal: 1024 * mib,
		},
		{
			name:    "unparseable",
			out:     "vm.swapusage: not available",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			used, total, err := parseSwapUsage(tc.out)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if used != tc.wantUsed {
				t.Errorf("used = %d, want %d", used, tc.wantUsed)
			}
			if total != tc.wantTotal {
				t.Errorf("total = %d, want %d", total, tc.wantTotal)
			}
			if used > total {
				t.Errorf("used %d exceeds total %d (clamp failed)", used, total)
			}
		})
	}
}
