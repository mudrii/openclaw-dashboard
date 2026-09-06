package appsystem

import (
	"fmt"
	"os"
	"testing"
)

// TestMain clears runtime-selection variables so fixture-driven tests never
// reach a developer's live container or state directory. Tests that exercise
// runtime selection opt in explicitly with t.Setenv or an explicit Target.
func TestMain(m *testing.M) {
	for _, name := range []string{"OPENCLAW_CONTAINER", "OPENCLAW_STATE_DIR"} {
		if err := os.Unsetenv(name); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}
