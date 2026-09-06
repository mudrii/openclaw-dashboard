package apprefresh

import (
	"fmt"
	"os"
	"testing"
)

// TestMain disables the platform log-fallback root so tests reading from
// TempDir-based openclaw paths do not accidentally pick up real
// ~/Library/Logs/openclaw/ entries on the developer's machine.
func TestMain(m *testing.M) {
	// Fixture collectors must not select a developer's live container or
	// state directory. Runtime-selection tests opt in explicitly with
	// t.Setenv or an explicit Target.
	for _, name := range []string{"OPENCLAW_CONTAINER", "OPENCLAW_STATE_DIR"} {
		if err := os.Unsetenv(name); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	SetLogFallbackRoots(func() []string { return nil })
	os.Exit(m.Run())
}
