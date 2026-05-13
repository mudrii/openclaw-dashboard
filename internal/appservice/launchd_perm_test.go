//go:build darwin

package appservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchd_Install_plistMode0600(t *testing.T) {
	dir := t.TempDir()
	lb := &launchdBackend{
		plistDir: dir,
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
	if err := lb.Install(cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	plistPath := filepath.Join(dir, "com.openclaw.dashboard.plist")
	info, err := os.Stat(plistPath)
	if err != nil {
		t.Fatalf("stat plist: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("plist perm = %o, want 0o600", got)
	}
}

func TestLaunchd_Install_rejectsRelativeOpenclawHome(t *testing.T) {
	dir := t.TempDir()
	lb := &launchdBackend{
		plistDir: dir,
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
	if err := lb.Install(cfg); err == nil {
		t.Fatal("expected Install to reject non-absolute OPENCLAW_HOME, got nil")
	}
}

func TestLaunchd_Install_filtersBadPathEntries(t *testing.T) {
	dir := t.TempDir()
	lb := &launchdBackend{
		plistDir: dir,
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
	if err := lb.Install(cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	plistPath := filepath.Join(dir, "com.openclaw.dashboard.plist")
	data, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	pathLine := extractPlistPath(t, string(data))
	for _, bad := range []string{"", ".", "relative/dir"} {
		for _, entry := range strings.Split(pathLine, ":") {
			if entry == bad {
				t.Errorf("PATH entry %q should have been filtered, got: %q", bad, pathLine)
			}
		}
	}
	if !strings.Contains(pathLine, "/opt/good") || !strings.Contains(pathLine, "/usr/bin") {
		t.Errorf("PATH missing expected entries: %q", pathLine)
	}
}

// extractPlistPath returns the value of <key>PATH</key><string>...</string>.
func extractPlistPath(t *testing.T, plist string) string {
	t.Helper()
	const marker = "<key>PATH</key>"
	i := strings.Index(plist, marker)
	if i < 0 {
		t.Fatalf("plist missing PATH key:\n%s", plist)
	}
	rest := plist[i+len(marker):]
	open := strings.Index(rest, "<string>")
	if open < 0 {
		t.Fatalf("plist missing PATH value:\n%s", plist)
	}
	rest = rest[open+len("<string>"):]
	end := strings.Index(rest, "</string>")
	if end < 0 {
		t.Fatalf("plist PATH value not closed:\n%s", plist)
	}
	return rest[:end]
}

func TestLaunchd_Install_idempotentReinstall(t *testing.T) {
	dir := t.TempDir()
	lb := &launchdBackend{
		plistDir: dir,
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
	if err := lb.Install(cfg); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	cfg.Port = 9090
	if err := lb.Install(cfg); err != nil {
		t.Fatalf("second Install (idempotent): %v", err)
	}
	plistPath := filepath.Join(dir, "com.openclaw.dashboard.plist")
	info, err := os.Stat(plistPath)
	if err != nil {
		t.Fatalf("stat plist: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("plist perm after reinstall = %o, want 0o600", got)
	}
}
