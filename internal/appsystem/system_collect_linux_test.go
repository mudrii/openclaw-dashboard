//go:build linux

package appsystem

import (
	"context"
	"testing"
	"time"
)

// B4: Guard against uint64 underflow when MemAvailable > MemTotal
// (kernel race / virtualized environments can briefly report this).
func TestRamFromMeminfo_NoUnderflowWhenAvailableExceedsTotal(t *testing.T) {
	info := map[string]uint64{
		"MemTotal":     1024,
		"MemAvailable": 2048, // larger than total — must clamp, not wrap
	}
	got := ramFromMeminfo(info)
	if got.UsedBytes != 0 {
		t.Fatalf("UsedBytes = %d, want 0 (clamped)", got.UsedBytes)
	}
	if got.TotalBytes != 1024*1024 {
		t.Fatalf("TotalBytes = %d, want %d", got.TotalBytes, 1024*1024)
	}
	if got.Percent != 0.0 {
		t.Fatalf("Percent = %v, want 0", got.Percent)
	}
}

func TestRamFromMeminfo_TypicalCase(t *testing.T) {
	info := map[string]uint64{
		"MemTotal":     2048,
		"MemAvailable": 512,
	}
	got := ramFromMeminfo(info)
	wantUsed := int64((2048 - 512) * 1024)
	if got.UsedBytes != wantUsed {
		t.Fatalf("UsedBytes = %d, want %d", got.UsedBytes, wantUsed)
	}
}

func TestRamFromMeminfo_FallsBackToMemFreeWhenAvailableMissing(t *testing.T) {
	info := map[string]uint64{
		"MemTotal": 2048,
		"MemFree":  1024,
	}
	got := ramFromMeminfo(info)
	wantUsed := int64((2048 - 1024) * 1024)
	if got.UsedBytes != wantUsed {
		t.Fatalf("UsedBytes = %d, want %d", got.UsedBytes, wantUsed)
	}
}

func TestSwapFromMeminfo_NoUnderflowWhenFreeExceedsTotal(t *testing.T) {
	info := map[string]uint64{
		"SwapTotal": 1024,
		"SwapFree":  4096, // larger than total — must clamp
	}
	got := swapFromMeminfo(info)
	if got.UsedBytes != 0 {
		t.Fatalf("UsedBytes = %d, want 0 (clamped)", got.UsedBytes)
	}
	if got.TotalBytes != 1024*1024 {
		t.Fatalf("TotalBytes = %d, want %d", got.TotalBytes, 1024*1024)
	}
}

func TestSwapFromMeminfo_TypicalCase(t *testing.T) {
	info := map[string]uint64{
		"SwapTotal": 4096,
		"SwapFree":  1024,
	}
	got := swapFromMeminfo(info)
	wantUsed := int64((4096 - 1024) * 1024)
	if got.UsedBytes != wantUsed {
		t.Fatalf("UsedBytes = %d, want %d", got.UsedBytes, wantUsed)
	}
}

func TestCollectCPU_HonorsTimeout(t *testing.T) {
	start := time.Now()
	got := collectCPU(context.Background(), 1)
	elapsed := time.Since(start)

	if got.Error == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	// Assert only that a non-empty error was reported, not its exact wording —
	// the wrapped message ("top failed: ...") is not part of the contract
	// (CLAUDE.md: avoid brittle error-text assertions). The timing bound below
	// proves the deadline actually fired.
	if *got.Error == "" {
		t.Fatalf("expected non-empty timeout error, got empty string")
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("collectCPU took %v, want timeout before 200ms sample delay", elapsed)
	}
}
