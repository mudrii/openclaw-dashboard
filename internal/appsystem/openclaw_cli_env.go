package appsystem

import "github.com/mudrii/openclaw-dashboard/internal/appopenclaw"

// OpenclawCLIEnv returns a sanitized environment for openclaw CLI subprocesses.
// Older dashboard services wrote OPENCLAW_HOME=$HOME/.openclaw, which is the
// dashboard data dir interpretation. Current openclaw CLI releases interpret
// that value as a base home and then look under .openclaw/.openclaw. When the
// stale default is detected, omit OPENCLAW_HOME for the child process so the CLI
// falls back to HOME while the dashboard process can still use the variable for
// file-based collectors.
func OpenclawCLIEnv(name string) []string {
	return appopenclaw.CLIEnv(name)
}
