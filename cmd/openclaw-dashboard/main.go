package main

import (
	"os"

	dashboard "github.com/mudrii/openclaw-dashboard"
)

// run invokes the dashboard CLI and returns the process exit code. Kept as a
// thin wrapper so tests can exercise the cmd-package entry path in-process
// (main() itself is not directly testable because it calls os.Exit).
func run() int {
	return dashboard.Main()
}

func main() {
	os.Exit(run())
}
