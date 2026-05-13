// Package appruntime handles directory resolution, version detection, and Homebrew runtime seeding.
package appruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	for range 3 {
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
	if err := CopyFile(
		filepath.Join(shareDir, "VERSION"),
		filepath.Join(runtimeDir, "VERSION"),
		0o644,
	); err != nil {
		return "", false, fmt.Errorf("sync VERSION: %w", err)
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

// CopyIfMissing copies src to dst only if dst does not already exist. The
// existence check and creation are performed atomically via O_CREATE|O_EXCL,
// so concurrent first-run callers race safely: exactly one writer materializes
// the file and the rest observe it as already present.
func CopyIfMissing(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return err
	}
	// We own dst exclusively. Stream src into it; on any failure remove the
	// empty/partial file so the next call retries cleanly.
	if err := streamCopy(src, f); err != nil {
		_ = f.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(dst)
		return err
	}
	return f.Close()
}

// CopyFile writes src to dst atomically via temp-file + rename. Readers
// observe either the prior contents of dst or the new contents — never an
// intermediate truncated state.
func CopyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := openTempSibling(dst, mode)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := streamCopy(src, tmp); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// streamCopy copies src bytes into dst without buffering the whole file.
func streamCopy(src string, dst io.Writer) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err := io.Copy(dst, in); err != nil {
		return err
	}
	return nil
}

// openTempSibling creates a uniquely-named file alongside dst with the
// requested mode. Using a sibling guarantees the subsequent rename stays on
// the same filesystem and is therefore atomic.
func openTempSibling(dst string, mode os.FileMode) (*os.File, error) {
	dir := filepath.Dir(dst)
	base := filepath.Base(dst)
	var suffix [8]byte
	for range 8 {
		if _, err := rand.Read(suffix[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(dir, base+".tmp."+hex.EncodeToString(suffix[:]))
		f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("create temp sibling for %s: exhausted attempts", dst)
}

// ResolveOpenclawPath returns the OpenClaw root directory used by dashboard collectors.
// It honors OPENCLAW_HOME and falls back to ~/.openclaw.
func ResolveOpenclawPath() string {
	if override := strings.TrimSpace(os.Getenv("OPENCLAW_HOME")); override != "" {
		return appconfig.ExpandHome(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".openclaw"
	}
	return filepath.Join(home, ".openclaw")
}

func DetectVersion(ctx context.Context, dir string) string {
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
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
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
