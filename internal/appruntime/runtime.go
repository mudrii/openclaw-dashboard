package appruntime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

func ResolveDashboardDir(dir string) string {
	resolved, err := ResolveDashboardDirWithError(dir)
	if err != nil || resolved == "" {
		return dir
	}
	return resolved
}

func ResolveDashboardDirWithError(dir string) (string, error) {
	if override := strings.TrimSpace(os.Getenv("OPENCLAW_DASHBOARD_DIR")); override != "" {
		return appconfig.ExpandHome(override), nil
	}
	if repoDir, ok := findDashboardDir(dir); ok {
		return repoDir, nil
	}
	if runtimeDir, ok, err := SeedHomebrewRuntimeDir(dir); err != nil {
		return "", err
	} else if ok {
		return runtimeDir, nil
	}
	return dir, nil
}

func ResolveRepoRoot(dir string) string {
	return ResolveDashboardDir(dir)
}

func findDashboardDir(dir string) (string, bool) {
	if _, err := os.Stat(filepath.Join(dir, "assets", "runtime", "refresh.sh")); err == nil {
		return dir, true
	}
	candidate := dir
	for i := 0; i < 3; i++ {
		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
		candidate = parent
		if _, err := os.Stat(filepath.Join(candidate, "assets", "runtime", "refresh.sh")); err == nil {
			return candidate, true
		}
	}
	return "", false
}

func SeedHomebrewRuntimeDir(binDir string) (string, bool, error) {
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
		if err := CopyIfMissing(f.src, f.dst, f.mode); err != nil {
			return "", false, fmt.Errorf("seed %s: %w", f.name, err)
		}
	}
	if err := CopyIfMissing(
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

func CopyIfMissing(src, dst string, mode os.FileMode) error {
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

func DetectVersion(dir string) string {
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
	return "dev"
}
