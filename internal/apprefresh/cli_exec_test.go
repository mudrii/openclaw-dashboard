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

// TestBoundedOutput_ExceedsCap kills the producer and errors instead of
// buffering unbounded output (the durable-task-store growth guard).
func TestBoundedOutput_ExceedsCap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// `yes` streams forever; the cap must stop it.
	_, err := boundedOutput(exec.CommandContext(ctx, "yes"), 64)
	if err == nil {
		t.Fatal("expected error when output exceeds cap, got nil")
	}
}

// TestBoundedOutput_NonZeroExit surfaces a command failure as an error, matching
// *exec.Cmd.Output's contract (callers fall back / return empty).
func TestBoundedOutput_NonZeroExit(t *testing.T) {
	if _, err := boundedOutput(exec.Command("false"), 1024); err == nil {
		t.Error("expected error on non-zero exit, got nil")
	}
}
