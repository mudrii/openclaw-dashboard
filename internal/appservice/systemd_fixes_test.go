//go:build linux

package appservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSystemd_Uninstall_BenignStopDisable covers the resilience path
// (systemd.go:172-185): stop/disable returning a benign "Unit not loaded"
// must not abort Uninstall — the unit file is still removed and Uninstall
// succeeds.
func TestSystemd_Uninstall_BenignStopDisable(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, systemdUnitName+".service")
	if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sb := &systemdBackend{
		ctx:     context.Background(),
		unitDir: dir,
		runCmd: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			for _, a := range args {
				if a == "stop" || a == "disable" {
					return []byte("Unit openclaw-dashboard.service not loaded."), errors.New("exit 1")
				}
			}
			return nil, nil // daemon-reload succeeds
		},
		probeFunc: func(string) bool { return false },
	}
	if err := sb.Uninstall(); err != nil {
		t.Fatalf("Uninstall err = %v, want nil for benign stop/disable", err)
	}
	if _, err := os.Stat(unitPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unit file should be removed; stat err = %v", err)
	}
}

// TestSystemd_Uninstall_NonBenignStillProceeds covers the warn-and-proceed
// path: a non-benign "permission denied" from stop/disable is logged but does
// not abort the removal.
func TestSystemd_Uninstall_NonBenignStillProceeds(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, systemdUnitName+".service")
	if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sb := &systemdBackend{
		ctx:     context.Background(),
		unitDir: dir,
		runCmd: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			for _, a := range args {
				if a == "stop" || a == "disable" {
					return []byte("permission denied"), errors.New("exit 1")
				}
			}
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	if err := sb.Uninstall(); err != nil {
		t.Fatalf("Uninstall err = %v, want nil (warn and proceed)", err)
	}
	if _, err := os.Stat(unitPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unit file should be removed despite non-benign failure; stat err = %v", err)
	}
}

// TestSystemd_Uninstall_NotInstalled covers the not-installed guard
// (systemd.go:169): an empty unitDir yields "not installed".
func TestSystemd_Uninstall_NotInstalled(t *testing.T) {
	sb := &systemdBackend{
		ctx:     context.Background(),
		unitDir: t.TempDir(),
		runCmd: func(context.Context, string, ...string) ([]byte, error) {
			t.Error("systemctl should not be invoked when unit file is absent")
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	err := sb.Uninstall()
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("Uninstall err = %v, want 'not installed'", err)
	}
}

// TestSystemd_Status_ZeroPortSkipsProbe covers the port==0 guard
// (systemd.go:265): an active unit with a valid PID but no --port must yield
// Running=false and must NOT consult the HTTP probe.
func TestSystemd_Status_ZeroPortSkipsProbe(t *testing.T) {
	dir := t.TempDir()
	// Unit file whose ExecStart carries no --port.
	unit := "[Service]\nExecStart=/usr/local/bin/openclaw-dashboard --bind 127.0.0.1\n"
	if err := os.WriteFile(filepath.Join(dir, systemdUnitName+".service"), []byte(unit), 0o600); err != nil {
		t.Fatal(err)
	}
	var probed bool
	sb := &systemdBackend{
		ctx:     context.Background(),
		unitDir: dir,
		runCmd: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			for _, a := range args {
				if a == "show" {
					return []byte("ActiveState=active\nMainPID=4321\nActiveEnterTimestamp=\n"), nil
				}
			}
			return nil, nil
		},
		probeFunc: func(string) bool {
			probed = true
			return true
		},
	}
	st, err := sb.Status()
	if err != nil {
		t.Fatalf("Status err = %v", err)
	}
	if st.Port != 0 {
		t.Fatalf("precondition: Port = %d, want 0", st.Port)
	}
	if st.Running {
		t.Errorf("Running = true, want false when port is 0")
	}
	if probed {
		t.Errorf("probeFunc consulted despite port==0 guard")
	}
}

// TestIsBenignSystemctlFailure tabulates the benign/non-benign classification.
func TestIsBenignSystemctlFailure(t *testing.T) {
	tests := []struct {
		out  string
		want bool
	}{
		{"Unit openclaw-dashboard.service not loaded.", true},
		{"Unit is not active", true},
		{"No such file or directory", true},
		{"permission denied", false},
		{"Failed to connect to bus", false},
	}
	for _, tc := range tests {
		if got := isBenignSystemctlFailure([]byte(tc.out)); got != tc.want {
			t.Errorf("isBenignSystemctlFailure(%q) = %v, want %v", tc.out, got, tc.want)
		}
	}
}
