package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// resolveRepoRoot walks upward from dir (max 3 levels) looking for the
// dashboard repo root, identified by refresh.sh. This allows binaries built
// into dist/ to still find repo-root assets like refresh.sh, data.json,
// config.json, and VERSION.
func resolveRepoRoot(dir string) string {
	// If refresh.sh exists in dir, this is the repo root
	if _, err := os.Stat(filepath.Join(dir, "refresh.sh")); err == nil {
		return dir
	}
	// Walk up to 3 parent directories looking for refresh.sh
	candidate := dir
	for i := 0; i < 3; i++ {
		parent := filepath.Dir(candidate)
		if parent == candidate {
			// reached filesystem root
			break
		}
		candidate = parent
		if _, err := os.Stat(filepath.Join(candidate, "refresh.sh")); err == nil {
			return candidate
		}
	}
	// No repo root found — return best-effort original dir
	return dir
}

func detectVersion(dir string) string {
	// 1. VERSION file — allow worktrees/experimental builds to override tagged releases.
	// Check both the executable directory and its parent so binaries built into ./dist
	// still pick up the repo-root VERSION file.
	for _, base := range []string{dir, filepath.Dir(dir)} {
		vf := filepath.Join(base, "VERSION")
		data, err := os.ReadFile(vf)
		if err == nil {
			v := strings.TrimSpace(string(data))
			if v != "" {
				return strings.TrimPrefix(v, "v")
			}
		}
	}
	// 2. git describe --tags --abbrev=0 — with 5s timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "describe", "--tags", "--abbrev=0")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err == nil {
		tag := strings.TrimSpace(string(out))
		if tag != "" {
			return strings.TrimPrefix(tag, "v")
		}
	}
	// 3. fallback
	return "dev"
}
