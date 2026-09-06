package appopenclaw

import (
	"os"
	"path/filepath"
	"strings"
)

// CLIEnv removes an older dashboard's OPENCLAW_HOME=data-directory setting.
// OpenClaw interprets HOME as the parent of .openclaw, not the state directory.
func CLIEnv(name string) []string {
	if filepath.Base(name) != "openclaw" {
		return nil
	}
	raw := strings.TrimSpace(os.Getenv("OPENCLAW_HOME"))
	if raw == "" || filepath.Base(filepath.Clean(raw)) != ".openclaw" {
		return nil
	}
	// #nosec G703 -- read-only stat of a fixed filename under the process owner's configured state directory.
	info, err := os.Stat(filepath.Join(raw, "openclaw.json"))
	if err != nil || info.IsDir() {
		return nil
	}
	return withoutEnv(os.Environ(), "OPENCLAW_HOME")
}
