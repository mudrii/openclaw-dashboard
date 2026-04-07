package appservice

import (
	"fmt"
	"os/exec"
)

// execRun runs name with args, returns combined output.
func execRun(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%w", err)
	}
	return out, nil
}
