package appsystem

import (
	"os"
	"path/filepath"
	"strings"
)

// OpenclawCLIEnv returns a sanitized environment for openclaw CLI subprocesses.
// Older dashboard services wrote OPENCLAW_HOME=$HOME/.openclaw, which is the
// dashboard data dir interpretation. Current openclaw CLI releases interpret
// that value as a base home and then look under .openclaw/.openclaw. When the
// stale default is detected, omit OPENCLAW_HOME for the child process so the CLI
// falls back to HOME while the dashboard process can still use the variable for
// file-based collectors.
func OpenclawCLIEnv(name string) []string {
	if filepath.Base(name) != "openclaw" {
		return nil
	}
	raw := strings.TrimSpace(os.Getenv("OPENCLAW_HOME"))
	if raw == "" || !looksLikeOpenclawDataDir(raw) {
		return nil
	}
	env := os.Environ()
	out := env[:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, "OPENCLAW_HOME=") {
			out = append(out, kv)
		}
	}
	return out
}

func looksLikeOpenclawDataDir(path string) bool {
	clean := filepath.Clean(path)
	if filepath.Base(clean) != ".openclaw" {
		return false
	}
	info, err := os.Stat(filepath.Join(clean, "openclaw.json"))
	return err == nil && !info.IsDir()
}
