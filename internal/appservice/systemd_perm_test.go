//go:build linux

package appservice

import (
	"context"
	"os"
	"path/filepath"
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
