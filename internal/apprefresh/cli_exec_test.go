package apprefresh

import (
	"context"
	"os/exec"
	"testing"
)

// TestBoundedOutput_SmallOutput returns stdout intact when it fits under the cap.
func TestBoundedOutput_SmallOutput(t *testing.T) {
	out, err := boundedOutput(exec.Command("printf", "%s", "hello"), 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "hello" {
		t.Errorf("out = %q, want hello", out)
	}
}

// TestBoundedOutput_ExceedsCap kills the producer and errors (returning nil
// data) instead of buffering unbounded output — the durable-task-store growth
// guard. Asserting nil data pins the "degrade, don't return partial" contract.
func TestBoundedOutput_ExceedsCap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// `yes` streams forever; the cap must stop it.
	out, err := boundedOutput(exec.CommandContext(ctx, "yes"), 64)
	if err == nil {
		t.Fatal("expected error when output exceeds cap, got nil")
	}
	if out != nil {
		t.Errorf("out = %q, want nil on over-cap (no partial data)", out)
	}
}

// TestBoundedOutput_AtCapBoundary pins the off-by-one: output of exactly maxBytes
// must succeed (the guard is len>max, not len>=max).
func TestBoundedOutput_AtCapBoundary(t *testing.T) {
	out, err := boundedOutput(exec.Command("printf", "%s", "0123456789"), 10) // exactly 10 bytes
	if err != nil {
		t.Fatalf("unexpected error at exactly-cap output: %v", err)
	}
	if string(out) != "0123456789" {
		t.Errorf("out = %q, want full 10-byte output", out)
	}
	if _, err := boundedOutput(exec.Command("printf", "%s", "0123456789X"), 10); err == nil {
		t.Error("11 bytes over a 10-byte cap should error")
	}
}

// TestBoundedOutput_NonZeroExit surfaces a command failure as an error, matching
// *exec.Cmd.Output's contract (callers fall back / return empty).
func TestBoundedOutput_NonZeroExit(t *testing.T) {
	if _, err := boundedOutput(exec.Command("false"), 1024); err == nil {
		t.Error("expected error on non-zero exit, got nil")
	}
}
