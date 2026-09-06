package appserver

import (
	"fmt"
	"os"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

// TestMain disables the platform log-fallback root so tests reading from
// TempDir-based openclaw paths do not accidentally pick up real
// ~/Library/Logs/openclaw/ entries on the developer's machine.
func TestMain(m *testing.M) {
	// Fixture handlers must not select a developer's live container or state.
	// Runtime-selection tests opt in explicitly with t.Setenv or Config.
	for _, name := range []string{"OPENCLAW_CONTAINER", "OPENCLAW_STATE_DIR"} {
		if err := os.Unsetenv(name); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	apprefresh.SetLogFallbackRoots(func() []string { return nil })
	os.Exit(m.Run())
}
