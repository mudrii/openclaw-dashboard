package apprefresh

import (
	"fmt"
	"io"
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
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(pipe, int64(maxBytes)+1))
	over := len(data) > maxBytes
	if over {
		_ = cmd.Process.Kill() // stop the producer so Wait does not block
	}
	waitErr := cmd.Wait()
	switch {
	case over:
		return nil, fmt.Errorf("openclaw CLI output exceeds %d bytes", maxBytes)
	case readErr != nil:
		return nil, readErr
	case waitErr != nil:
		return nil, waitErr
	}
	return data, nil
}
