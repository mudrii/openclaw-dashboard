//go:build darwin || linux

package appservice

import (
	"fmt"
	"os"
	"strings"
)

// openclawHomeEnv returns the OPENCLAW_HOME override to persist into the
// service definition, or "" when unset. A relative override is rejected: the
// service runs with a different working directory than the installing shell.
func openclawHomeEnv() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("OPENCLAW_HOME")); raw != "" {
		if err := validateAbsPath(raw); err != nil {
			return "", fmt.Errorf("OPENCLAW_HOME: %w", err)
		}
		return raw, nil
	}
	return "", nil
}

// servicePathEnv builds the PATH persisted into the service definition: the
// installing shell's absolute PATH entries followed by the platform defaults.
func servicePathEnv(defaults []string) string {
	return joinAbsPaths(strings.Split(os.Getenv("PATH"), ":"), defaults)
}
