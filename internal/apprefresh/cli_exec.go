package apprefresh

import (
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
	"os/exec"
)

// maxCLIOutputBytes caps the stdout buffered from an openclaw CLI shell-out so a
// pathologically large store (e.g. a long durable-task history) cannot spike
// memory each refresh. Real outputs are kilobytes; this is a safety ceiling.
const maxCLIOutputBytes = 8 << 20 // 8 MiB

// boundedOutput runs cmd and returns its stdout, reading at most maxBytes+1 so a
// runaway command can never buffer unbounded output into memory. If the output
// exceeds maxBytes the process is killed and an error is returned, letting
// callers degrade gracefully (empty result / fallback) instead of OOMing. It
// mirrors *exec.Cmd.Output's contract otherwise: a non-zero exit returns an
// error and stdout is the captured bytes.
func boundedOutput(cmd *exec.Cmd, maxBytes int) ([]byte, error) {
	return appopenclaw.Output(cmd, maxBytes)
}
