package appservice

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecRun(t *testing.T) {
	t.Run("captures stdout of a successful command", func(t *testing.T) {
		out, err := execRun(t.Context(), "echo", "hello")
		if err != nil {
			t.Fatalf("execRun echo: %v", err)
		}
		if got := strings.TrimSpace(string(out)); got != "hello" {
			t.Errorf("output = %q, want %q", got, "hello")
		}
	})

	t.Run("returns error for non-zero exit", func(t *testing.T) {
		// `false` exits with status 1.
		_, err := execRun(t.Context(), "false")
		if err == nil {
			t.Fatal("expected error from `false` (exit 1), got nil")
		}
	})

	t.Run("nil context is tolerated", func(t *testing.T) {
		//nolint:staticcheck // explicitly exercising the nil-ctx fallback path
		out, err := execRun(nil, "echo", "ok")
		if err != nil {
			t.Fatalf("execRun with nil ctx: %v", err)
		}
		if got := strings.TrimSpace(string(out)); got != "ok" {
			t.Errorf("output = %q, want %q", got, "ok")
		}
	})

	t.Run("forces LC_ALL=C in the child environment", func(t *testing.T) {
		out, err := execRun(t.Context(), "sh", "-c", "echo $LC_ALL")
		if err != nil {
			t.Fatalf("execRun sh: %v", err)
		}
		if got := strings.TrimSpace(string(out)); got != "C" {
			t.Errorf("LC_ALL in child = %q, want %q", got, "C")
		}
	})

	t.Run("honours a caller deadline against a blocking command", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		start := time.Now()
		// `sleep 30` would block well past the deadline; the context cancels it.
		_, err := execRun(ctx, "sleep", "30")
		elapsed := time.Since(start)
		if err == nil {
			t.Fatal("expected deadline-cancelled command to error, got nil")
		}
		// Generous upper bound: the process must be killed near the deadline,
		// not after the full 30s sleep.
		if elapsed > 10*time.Second {
			t.Errorf("command not cancelled promptly: elapsed %v", elapsed)
		}
	})
}
