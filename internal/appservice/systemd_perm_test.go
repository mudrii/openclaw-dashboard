//go:build linux

package appservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemd_Install_unitMode0600(t *testing.T) {
	dir := t.TempDir()
	sb := &systemdBackend{
		unitDir: dir,
		runCmd: func(context.Context, string, ...string) ([]byte, error) {
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	cfg := InstallConfig{
		BinPath: "/bin/openclaw-dashboard",
		WorkDir: "/tmp",
		LogPath: "/tmp/server.log",
		Host:    "127.0.0.1",
		Port:    8080,
	}
	if err := sb.Install(cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	unitPath := filepath.Join(dir, "openclaw-dashboard.service")
	info, err := os.Stat(unitPath)
	if err != nil {
		t.Fatalf("stat unit: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("unit perm = %o, want 0o600", got)
	}
}

func TestSystemd_Install_rejectsRelativeOpenclawHome(t *testing.T) {
	dir := t.TempDir()
	sb := &systemdBackend{
		unitDir: dir,
		runCmd: func(context.Context, string, ...string) ([]byte, error) {
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	t.Setenv("OPENCLAW_HOME", "relative/path")
	cfg := InstallConfig{
		BinPath: "/bin/openclaw-dashboard",
		WorkDir: "/tmp",
		LogPath: "/tmp/server.log",
		Host:    "127.0.0.1",
		Port:    8080,
	}
	if err := sb.Install(cfg); err == nil {
		t.Fatal("expected Install to reject non-absolute OPENCLAW_HOME, got nil")
	}
}

func TestSystemd_Install_filtersBadPathEntries(t *testing.T) {
	dir := t.TempDir()
	sb := &systemdBackend{
		unitDir: dir,
		runCmd: func(context.Context, string, ...string) ([]byte, error) {
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	t.Setenv("PATH", "/opt/good::.:relative/dir:/usr/bin")
	t.Setenv("OPENCLAW_HOME", "/srv/openclaw")
	cfg := InstallConfig{
		BinPath: "/bin/openclaw-dashboard",
		WorkDir: "/tmp",
		LogPath: "/tmp/server.log",
		Host:    "127.0.0.1",
		Port:    8080,
	}
	if err := sb.Install(cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	unitPath := filepath.Join(dir, "openclaw-dashboard.service")
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	pathVal := extractUnitPath(t, string(data))
	for _, bad := range []string{"", ".", "relative/dir"} {
		for _, entry := range strings.Split(pathVal, ":") {
			if entry == bad {
				t.Errorf("PATH entry %q should have been filtered, got: %q", bad, pathVal)
			}
		}
	}
	if !strings.Contains(pathVal, "/opt/good") || !strings.Contains(pathVal, "/usr/bin") {
		t.Errorf("PATH missing expected entries: %q", pathVal)
	}
}

// extractUnitPath returns the value embedded in `Environment="PATH=..."`.
func extractUnitPath(t *testing.T, content string) string {
	t.Helper()
	for _, line := range strings.Split(content, "\n") {
		const prefix = `Environment="PATH=`
		i := strings.Index(line, prefix)
		if i < 0 {
			continue
		}
		rest := line[i+len(prefix):]
		end := strings.LastIndex(rest, `"`)
		if end < 0 {
			t.Fatalf("malformed PATH env line: %q", line)
		}
		return rest[:end]
	}
	t.Fatalf("PATH environment not present in unit:\n%s", content)
	return ""
}

func TestSystemd_Install_idempotentReinstall(t *testing.T) {
	dir := t.TempDir()
	sb := &systemdBackend{
		unitDir: dir,
		runCmd: func(context.Context, string, ...string) ([]byte, error) {
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	cfg := InstallConfig{
		BinPath: "/bin/openclaw-dashboard",
		WorkDir: "/tmp",
		LogPath: "/tmp/server.log",
		Host:    "127.0.0.1",
		Port:    8080,
	}
	if err := sb.Install(cfg); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	cfg.Port = 9090
	if err := sb.Install(cfg); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	unitPath := filepath.Join(dir, "openclaw-dashboard.service")
	info, err := os.Stat(unitPath)
	if err != nil {
		t.Fatalf("stat unit: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("unit perm after reinstall = %o, want 0o600", got)
	}
}
