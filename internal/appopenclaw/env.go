package appopenclaw

import (
	"log/slog"
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

// ExpandHome replaces a leading "~/" with the current user's home directory.
// Other paths, and every path when the home directory cannot be determined,
// are returned unchanged.
func ExpandHome(path string) string {
	rest, ok := strings.CutPrefix(path, "~/")
	if !ok {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("[dashboard] UserHomeDir failed, cannot expand ~", "error", err)
		return path
	}
	return filepath.Join(home, rest)
}
