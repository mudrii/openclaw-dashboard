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
	"log/slog"
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
	}
	for _, f := range required {
		if err := CopyIfMissing(f.src, f.dst, f.mode); err != nil {
			return "", false, fmt.Errorf("seed %s: %w", f.name, err)
		}
	}
	// VERSION is always overwritten so the runtime dir tracks the installed
	// release; user-editable assets above are only seeded when missing.
	versionDst := filepath.Join(runtimeDir, "VERSION")
	if err := CopyFile(filepath.Join(shareDir, "VERSION"), versionDst, 0o644); err != nil {
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
	if _, err := os.Stat(versionDst); err != nil {
		return "", false, errors.New("missing required asset VERSION")
	}

	return runtimeDir, true, nil
}

// CopyIfMissing copies src to dst only if dst does not already exist.
// The file is fully written to a temp sibling, fsynced, then published with
// os.Link, which is atomic on POSIX filesystems and fails with fs.ErrExist if
// another writer won the race. The parent directory is fsynced so the new
// name survives a crash. Concurrent callers race safely: exactly one writer
// materializes the file and the rest observe it as already present.
func CopyIfMissing(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
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
	if err := os.Link(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return err
	}
	syncErr := syncDir(filepath.Dir(dst))
	_ = os.Remove(tmpName)
	return syncErr
}

// CopyFile writes src to dst atomically via temp-file + rename. Readers
// observe either the prior contents of dst or the new contents — never an
// intermediate truncated state. The parent directory is fsynced so the new
// name survives a crash.
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
	return syncDir(filepath.Dir(dst))
}

// syncDir fsyncs the directory referenced by path so that any rename or link
// metadata persists across a crash. Filesystems that reject directory Sync
// (some tmpfs configurations, non-Unix) are treated as best-effort.
func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil && !errors.Is(err, fs.ErrInvalid) {
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
	defer func() { _ = in.Close() }()
	if _, err := io.Copy(dst, in); err != nil {
		return err
	}
	return nil
}

// openTempSibling creates a uniquely-named file alongside dst with the
// requested mode. Using a sibling guarantees the subsequent rename stays on
// the same filesystem and is therefore atomic. Mode bits are explicitly
// applied via Chmod after creation so a process umask (e.g., 0o077 under
// systemd) cannot strip group/other read bits the caller intended.
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
			if chmodErr := f.Chmod(mode); chmodErr != nil {
				_ = f.Close()
				_ = os.Remove(name)
				return nil, chmodErr
			}
			return f, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("create temp sibling for %s: exhausted attempts", dst)
}

// ResolveOpenclawPath returns the OpenClaw root directory used by dashboard collectors.
// It honors OPENCLAW_HOME and falls back to ~/.openclaw. When the home
// directory cannot be resolved it logs a warning and returns the legacy
// relative ".openclaw"; callers that can surface the failure should use
// ResolveOpenclawPathWithError instead.
func ResolveOpenclawPath() string {
	p, err := ResolveOpenclawPathWithError()
	if err != nil {
		slog.Warn("[dashboard] cannot resolve OpenClaw home; falling back to relative .openclaw", "error", err)
		return ".openclaw"
	}
	return p
}

// ResolveOpenclawPathWithError returns the OpenClaw root directory, honoring
// OPENCLAW_HOME and falling back to ~/.openclaw. It returns an error rather
// than a relative path when OPENCLAW_HOME is unset and the home directory
// cannot be resolved.
func ResolveOpenclawPathWithError() (string, error) {
	if override := strings.TrimSpace(os.Getenv("OPENCLAW_HOME")); override != "" {
		return appconfig.ExpandHome(override), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".openclaw"), nil
}

// StableExecutablePath maps a symlink-resolved Homebrew keg binary
// (<prefix>/Cellar/<formula>/<version>/bin/<name>) to the version-independent
// opt link (<prefix>/opt/<formula>/bin/<name>) so service definitions survive
// `brew upgrade`, which deletes the old keg. The input is returned unchanged
// when it is not a keg binary or the opt path does not exist.
func StableExecutablePath(resolved string) string {
	return stableExecutablePath(resolved, os.Stat)
}

func stableExecutablePath(resolved string, stat func(string) (fs.FileInfo, error)) string {
	if !filepath.IsAbs(resolved) {
		return resolved
	}
	binDir := filepath.Dir(resolved)
	kegDir := filepath.Dir(binDir)
	formulaDir := filepath.Dir(kegDir)
	cellarDir := filepath.Dir(formulaDir)
	if filepath.Base(binDir) != "bin" || filepath.Base(cellarDir) != "Cellar" {
		return resolved
	}
	opt := filepath.Join(filepath.Dir(cellarDir), "opt", filepath.Base(formulaDir), "bin", filepath.Base(resolved))
	if _, err := stat(opt); err != nil {
		return resolved
	}
	return opt
}

// DetectVersion returns the project version, preferring a VERSION file in dir
// or its parent and falling back to the latest git tag. ctx must be non-nil;
// its deadline and cancellation are propagated to the git probe so callers
// can bound the lookup.
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
	// Honor a tighter caller deadline if one is set; otherwise cap at 5s so a
	// missing git binary or hung describe can never block startup indefinitely.
	const gitTimeout = 5 * time.Second
	if d, ok := ctx.Deadline(); !ok || time.Until(d) > gitTimeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, gitTimeout)
		defer cancel()
	}
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
