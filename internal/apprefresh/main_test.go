package apprefresh

import (
	"os"
	"testing"
)

// TestMain disables the platform log-fallback root so tests reading from
// TempDir-based openclaw paths do not accidentally pick up real
// ~/Library/Logs/openclaw/ entries on the developer's machine.
func TestMain(m *testing.M) {
	SetLogFallbackRoots(func() []string { return nil })
	os.Exit(m.Run())
}
