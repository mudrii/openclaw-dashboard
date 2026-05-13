package appservice

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to path with the given permission. The write goes
// to a sibling temp file opened with O_EXCL (race-free creation), then a rename
// replaces the destination atomically. On any failure the temp file is removed.
//
// Use this instead of os.WriteFile / os.Create when the file may contain
// sensitive content (e.g. service unit files with embedded env vars). The
// 0o600 mode is enforced for the temp file at creation time, so the file is
// never observable on disk with broader permissions.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := uniqueTempPath(dir, filepath.Base(path))
	if err != nil {
		return fmt.Errorf("temp path: %w", err)
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	// Some filesystems honour umask on create; chmod to be explicit.
	if err := os.Chmod(tmp, perm); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	cleanup = false
	return nil
}

// uniqueTempPath returns a non-existing path inside dir using O_EXCL semantics
// from a probe loop. Avoids importing os.CreateTemp's defaults so the caller
// controls the permission bits explicitly.
func uniqueTempPath(dir, base string) (string, error) {
	for i := 0; i < 1000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf(".%s.tmp-%d-%d", base, os.Getpid(), i))
		_, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", errors.New("could not find unique temp path")
}
