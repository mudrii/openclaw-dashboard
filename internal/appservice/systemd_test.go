//go:build linux

package appservice

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestSystemd(t *testing.T) (*systemdBackend, string) {
	t.Helper()
	dir := t.TempDir()
	sb := &systemdBackend{
		unitDir: dir,
		runCmd: func(name string, args ...string) ([]byte, error) {
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	return sb, dir
}

func TestSystemd_Install_writesUnitFile(t *testing.T) {
	sb, dir := newTestSystemd(t)
	cfg := InstallConfig{
		BinPath: "/usr/local/bin/openclaw-dashboard",
		WorkDir: "/home/user/.openclaw/dashboard",
		LogPath: "/home/user/.openclaw/dashboard/server.log",
		Host:    "0.0.0.0",
		Port:    9090,
	}
	if err := sb.Install(cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	unitPath := filepath.Join(dir, "openclaw-dashboard.service")
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("unit file not written: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"[Unit]",
		"[Service]",
		"[Install]",
		"/usr/local/bin/openclaw-dashboard",
		"--bind", "0.0.0.0",
		"--port", "9090",
		"/home/user/.openclaw/dashboard",
		"Restart=always",
		"WantedBy=default.target",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("unit file missing %q\ncontent:\n%s", want, content)
		}
	}
}

func TestSystemd_Install_callsSystemctl(t *testing.T) {
	var calls []string
	dir := t.TempDir()
	sb := &systemdBackend{
		unitDir: dir,
		runCmd: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	cfg := InstallConfig{BinPath: "/bin/d", WorkDir: "/tmp", LogPath: "/tmp/s.log", Host: "127.0.0.1", Port: 8080}
	if err := sb.Install(cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, w := range []string{"daemon-reload", "enable", "start"} {
		found := false
		for _, c := range calls {
			if strings.Contains(c, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("systemctl %s not called, got: %v", w, calls)
		}
	}
}

func TestSystemd_Uninstall(t *testing.T) {
	var calls []string
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "openclaw-dashboard.service")
	_ = os.WriteFile(unitPath, []byte("[Unit]\n"), 0o644)
	sb := &systemdBackend{
		unitDir: dir,
		runCmd: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	if err := sb.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(unitPath); !errors.Is(err, os.ErrNotExist) {
		t.Error("unit file should be removed")
	}
	for _, w := range []string{"stop", "disable", "daemon-reload"} {
		found := false
		for _, c := range calls {
			if strings.Contains(c, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("systemctl %s not called, got: %v", w, calls)
		}
	}
}

func TestSystemd_Start(t *testing.T) {
	var calls []string
	sb := &systemdBackend{
		unitDir: t.TempDir(),
		runCmd: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
	}
	if err := sb.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	found := false
	for _, c := range calls {
		if strings.Contains(c, "--user") && strings.Contains(c, "start") {
			found = true
		}
	}
	if !found {
		t.Errorf("systemctl --user start not called, got: %v", calls)
	}
}

func TestSystemd_Stop(t *testing.T) {
	var calls []string
	sb := &systemdBackend{
		unitDir: t.TempDir(),
		runCmd: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
	}
	if err := sb.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	found := false
	for _, c := range calls {
		if strings.Contains(c, "--user") && strings.Contains(c, "stop") {
			found = true
		}
	}
	if !found {
		t.Errorf("systemctl --user stop not called, got: %v", calls)
	}
}

func TestSystemd_Restart(t *testing.T) {
	var calls []string
	sb := &systemdBackend{
		unitDir: t.TempDir(),
		runCmd: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
	}
	if err := sb.Restart(); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	found := false
	for _, c := range calls {
		if strings.Contains(c, "--user") && strings.Contains(c, "restart") {
			found = true
		}
	}
	if !found {
		t.Errorf("systemctl --user restart not called, got: %v", calls)
	}
}

func TestSystemd_Status_notInstalled(t *testing.T) {
	sb := &systemdBackend{
		unitDir:   t.TempDir(),
		probeFunc: func(string) bool { return false },
		runCmd: func(name string, args ...string) ([]byte, error) {
			return []byte(""), fmt.Errorf("exit status 4")
		},
	}
	st, err := sb.Status()
	if err != nil {
		t.Fatalf("Status should not error for uninstalled service: %v", err)
	}
	if st.Running {
		t.Error("should not be Running")
	}
}

func TestSystemd_Status_running(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "openclaw-dashboard.service")
	_ = os.WriteFile(unitPath, []byte("ExecStart=/bin/d --port 9090\n"), 0o644)

	sb := &systemdBackend{
		unitDir:   dir,
		probeFunc: func(string) bool { return true },
		runCmd: func(name string, args ...string) ([]byte, error) {
			joined := name + " " + strings.Join(args, " ")
			if strings.Contains(joined, "show") {
				return []byte("ActiveState=active\nMainPID=55555\nActiveEnterTimestamp=2026-04-08 10:00:00 UTC\n"), nil
			}
			if name == "journalctl" {
				return []byte("line1\nline2\n"), nil
			}
			return nil, nil
		},
	}
	st, err := sb.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Running {
		t.Error("expected Running=true")
	}
	if st.PID != 55555 {
		t.Errorf("PID = %d, want 55555", st.PID)
	}
	if st.Backend != "systemd user service" {
		t.Errorf("Backend = %q", st.Backend)
	}
	if st.Port != 9090 {
		t.Errorf("Port = %d, want 9090", st.Port)
	}
	if len(st.LogLines) == 0 {
		t.Error("expected log lines to be populated")
	}
	if !st.AutoStart {
		t.Error("expected AutoStart=true (unit file exists)")
	}
}

func TestSystemd_parseUnitPort(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantPort int
	}{
		{"with bind and port", "ExecStart=/bin/d --bind 0.0.0.0 --port 9090\n", 9090},
		{"port only", "ExecStart=/bin/d --port 8080\n", 8080},
		{"no port", "ExecStart=/bin/d\n", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseUnitPort(tc.content)
			if got != tc.wantPort {
				t.Errorf("parseUnitPort(%q) = %d, want %d", tc.content, got, tc.wantPort)
			}
		})
	}
}

func TestSystemd_parseSystemctlProps(t *testing.T) {
	tests := []struct {
		name  string
		input string
		key   string
		value string
	}{
		{"active state", "ActiveState=active\nMainPID=100\n", "ActiveState", "active"},
		{"main pid", "ActiveState=active\nMainPID=100\n", "MainPID", "100"},
		{"empty", "", "ActiveState", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			props := parseSystemctlProps(tc.input)
			if props[tc.key] != tc.value {
				t.Errorf("props[%q] = %q, want %q", tc.key, props[tc.key], tc.value)
			}
		})
	}
}
