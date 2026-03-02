package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func detectVersion(dir string) string {
	// 1. git describe --tags --abbrev=0
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err == nil {
		tag := strings.TrimSpace(string(out))
		if tag != "" {
			return strings.TrimPrefix(tag, "v")
		}
	}
	// 2. VERSION file
	vf := filepath.Join(dir, "VERSION")
	data, err := os.ReadFile(vf)
	if err == nil {
		v := strings.TrimSpace(string(data))
		if v != "" {
			return v
		}
	}
	// 3. fallback
	return "dev"
}
