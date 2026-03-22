package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// resolveDashboardDir returns the writable dashboard runtime directory.
func resolveDashboardDir(dir string) string {
	resolved, err := resolveDashboardDirWithError(dir)
	if err != nil || resolved == "" {
		return dir
	}
	return resolved
}

// resolveDashboardDirWithError returns the writable dashboard runtime directory.
//
// Resolution order:
// 1. OPENCLAW_DASHBOARD_DIR override.
// 2. Source/release directory containing refresh.sh (or a parent up to 3 levels).
// 3. Homebrew install: seed ~/.openclaw/dashboard from ../share/openclaw-dashboard.
// 4. Fallback to the executable directory.
func resolveDashboardDirWithError(dir string) (string, error) {
	if override := strings.TrimSpace(os.Getenv("OPENCLAW_DASHBOARD_DIR")); override != "" {
		return expandHome(override), nil
	}
	if repoDir, ok := findDashboardDir(dir); ok {
		return repoDir, nil
	}
	if runtimeDir, ok, err := seedHomebrewRuntimeDir(dir); err != nil {
		return "", err
	} else if ok {
		return runtimeDir, nil
	}
	return dir, nil
}

// resolveRepoRoot is kept as a compatibility wrapper for existing tests/callers.
func resolveRepoRoot(dir string) string {
	return resolveDashboardDir(dir)
}

func findDashboardDir(dir string) (string, bool) {
	// If refresh.sh exists in dir, this is the repo root
	if _, err := os.Stat(filepath.Join(dir, "refresh.sh")); err == nil {
		return dir, true
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
			return candidate, true
		}
	}
	return "", false
}

func seedHomebrewRuntimeDir(binDir string) (string, bool, error) {
	shareDir := filepath.Join(filepath.Dir(binDir), "share", "openclaw-dashboard")
	if _, err := os.Stat(filepath.Join(shareDir, "themes.json")); err != nil {
		return "", false, nil
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false, fmt.Errorf("resolve user home: %w", err)
	}
	runtimeDir := filepath.Join(home, ".openclaw", "dashboard")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return "", false, fmt.Errorf("create runtime dir %s: %w", runtimeDir, err)
	}

	required := []struct {
		src  string
		dst  string
		mode os.FileMode
		name string
	}{
		{filepath.Join(shareDir, "refresh.sh"), filepath.Join(runtimeDir, "refresh.sh"), 0o755, "refresh.sh"},
		{filepath.Join(shareDir, "themes.json"), filepath.Join(runtimeDir, "themes.json"), 0o644, "themes.json"},
		{filepath.Join(shareDir, "config.json"), filepath.Join(runtimeDir, "config.json"), 0o644, "config.json"},
		{filepath.Join(shareDir, "VERSION"), filepath.Join(runtimeDir, "VERSION"), 0o644, "VERSION"},
	}
	for _, f := range required {
		if err := copyIfMissing(f.src, f.dst, f.mode); err != nil {
			return "", false, fmt.Errorf("seed %s: %w", f.name, err)
		}
	}
	if err := copyIfMissing(
		filepath.Join(shareDir, "examples", "config.minimal.json"),
		filepath.Join(runtimeDir, "examples", "config.minimal.json"),
		0o644,
	); err != nil && !os.IsNotExist(err) {
		return "", false, fmt.Errorf("seed examples/config.minimal.json: %w", err)
	}
	for _, f := range required {
		if _, err := os.Stat(f.dst); err != nil {
			return "", false, fmt.Errorf("missing required asset %s", f.name)
		}
	}

	return runtimeDir, true, nil
}

func copyIfMissing(src, dst string, mode os.FileMode) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
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
