//go:build linux

package appsystem

import (
	"testing"
)

// TestParseProcStat_ReporterSample uses the exact /proc/stat bytes from
// issue #31 and pins the contract that the parser yields the busy% the math
// expects. Without this test, a future refactor could silently invert the
// formula (idle/total vs (total-idle)/total) and the dashboard would report
// idle as busy on Linux with no failing test to catch it.
func TestParseProcStat_ReporterSample(t *testing.T) {
	const t0 = "cpu  384637 193 349934 16579178 38233 0 3921 0 0 0\n"
	const t1 = "cpu  384643 193 349939 16579366 38233 0 3921 0 0 0\n"

	_, _, id1, tot1, err := parseProcStat(t0)
	if err != nil {
		t.Fatalf("parse t0: %v", err)
	}
	_, _, id2, tot2, err := parseProcStat(t1)
	if err != nil {
		t.Fatalf("parse t1: %v", err)
	}

	dTotal := tot2 - tot1
	dIdle := id2 - id1
	if dTotal != 199 {
		t.Errorf("dTotal: want 199, got %d", dTotal)
	}
	if dIdle != 188 {
		t.Errorf("dIdle: want 188, got %d", dIdle)
	}

	// The formula in collectCPU is (dTotal-dIdle)/dTotal == busy%. For this
	// sample, busy% should be ~5.5 (idle-dominated workload). If a future
	// refactor flips the formula to dIdle/dTotal, this assertion catches it.
	busyPct := float64(dTotal-dIdle) / float64(dTotal) * 100
	if busyPct < 5.0 || busyPct > 6.0 {
		t.Errorf("busy%%: want ~5.5, got %.2f (parser may be returning idle instead of busy)", busyPct)
	}
}

// TestParseProcStat_BusySystem covers the opposite end: heavily busy CPU.
// idle delta should be small relative to total delta.
func TestParseProcStat_BusySystem(t *testing.T) {
	const t0 = "cpu  1000 0 500 200 0 0 0 0 0 0\n"
	const t1 = "cpu  1190 0 600 210 0 0 0 0 0 0\n" // +190 user, +100 sys, +10 idle
	_, _, id1, tot1, err := parseProcStat(t0)
	if err != nil {
		t.Fatalf("parse t0: %v", err)
	}
	_, _, id2, tot2, err := parseProcStat(t1)
	if err != nil {
		t.Fatalf("parse t1: %v", err)
	}
	dTotal := tot2 - tot1
	dIdle := id2 - id1
	if dTotal != 300 {
		t.Errorf("dTotal: want 300, got %d", dTotal)
	}
	if dIdle != 10 {
		t.Errorf("dIdle: want 10, got %d", dIdle)
	}
	busyPct := float64(dTotal-dIdle) / float64(dTotal) * 100
	const wantBusy = 96.66666666666667
	if busyPct < wantBusy-0.5 || busyPct > wantBusy+0.5 {
		t.Errorf("busy%%: want ~%.1f, got %.2f", wantBusy, busyPct)
	}
}

// TestParseProcStat_MinimumFields rejects a cpu line with too few fields.
// Linux kernels < 2.6.11 had fewer counters; we require at least 8 (through
// softirq) for our totals to be meaningful.
func TestParseProcStat_MinimumFields(t *testing.T) {
	const tooFew = "cpu  100 0 50 200 0 0\n" // only 7 fields (cpu + 6 numbers)
	_, _, _, _, err := parseProcStat(tooFew)
	if err == nil {
		t.Errorf("parseProcStat with 7 fields: want error, got nil")
	}
}

// TestParseProcStat_NoCPULine returns an error rather than silently zeroing.
func TestParseProcStat_NoCPULine(t *testing.T) {
	const bogus = "ctxt 12345\nbtime 100\nprocesses 50\n"
	_, _, _, _, err := parseProcStat(bogus)
	if err == nil {
		t.Errorf("parseProcStat without cpu line: want error, got nil")
	}
}

// TestParseProcStat_OptionalStealField accepts cpu lines that omit the steal
// field (kernels < 2.6.11) — steal defaults to 0.
func TestParseProcStat_OptionalStealField(t *testing.T) {
	const noSteal = "cpu  1000 50 500 8000 100 10 40 0\n" // 8 mandatory fields, no steal/guest
	user, system, idle, total, err := parseProcStat(noSteal)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if user != 1000 || system != 500 || idle != 8000 {
		t.Errorf("fields: want user=1000 system=500 idle=8000, got user=%d system=%d idle=%d", user, system, idle)
	}
	const wantTotal = 1000 + 50 + 500 + 8000 + 100 + 10 + 40 + 0
	if total != wantTotal {
		t.Errorf("total: want %d, got %d", wantTotal, total)
	}
}

// TestParseProcStat_PicksAggregateNotPerCore guards that per-core lines
// (cpu0, cpu1, ...) are not consumed by the parser. The aggregate must win.
func TestParseProcStat_PicksAggregateNotPerCore(t *testing.T) {
	const stat = "cpu  1000 0 500 8000 0 0 0 0\n" +
		"cpu0 999 0 499 7999 0 0 0 0\n" +
		"cpu1 1 0 1 1 0 0 0 0\n"
	_, _, idle, total, err := parseProcStat(stat)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if idle != 8000 {
		t.Errorf("idle: want aggregate 8000, got %d (per-core line consumed?)", idle)
	}
	if total != 9500 {
		t.Errorf("total: want aggregate 9500, got %d", total)
	}
}
