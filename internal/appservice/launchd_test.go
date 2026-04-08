//go:build darwin

package appservice

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestLaunchd(t *testing.T) (*launchdBackend, string) {
	t.Helper()
	dir := t.TempDir()
	lb := &launchdBackend{
		plistDir: dir,
		runCmd: func(name string, args ...string) ([]byte, error) {
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	return lb, dir
}

func TestLaunchd_Install_writesPlist(t *testing.T) {
	lb, dir := newTestLaunchd(t)
	t.Setenv("HOME", "/home/user")
	t.Setenv("PATH", "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin")
	t.Setenv("OPENCLAW_HOME", "/srv/openclaw")
	cfg := InstallConfig{
		BinPath: "/usr/local/bin/openclaw-dashboard",
		WorkDir: "/home/user/.openclaw/dashboard",
		LogPath: "/home/user/.openclaw/dashboard/server.log",
		Host:    "127.0.0.1",
		Port:    9090,
	}
	if err := lb.Install(cfg); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	plistPath := filepath.Join(dir, "com.openclaw.dashboard.plist")
	data, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("plist not written: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		"com.openclaw.dashboard",
		"/usr/local/bin/openclaw-dashboard",
		"--bind",
		"127.0.0.1",
		"--port",
		"9090",
		"/home/user/.openclaw/dashboard",
		"/home/user/.openclaw/dashboard/server.log",
		"<key>EnvironmentVariables</key>",
		"<key>HOME</key>",
		"<string>/home/user</string>",
		"<key>PATH</key>",
		"<string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>",
		"<key>OPENCLAW_HOME</key>",
		"<string>/srv/openclaw</string>",
		"<true/>", // RunAtLoad
	} {
		if !strings.Contains(content, want) {
			t.Errorf("plist missing %q\ncontent:\n%s", want, content)
		}
	}
}

func TestLaunchd_Install_callsLaunchctl(t *testing.T) {
	var calls []string
	dir := t.TempDir()
	lb := &launchdBackend{
		plistDir: dir,
		runCmd: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
	}
	cfg := InstallConfig{BinPath: "/bin/test", WorkDir: "/tmp", LogPath: "/tmp/s.log", Host: "127.0.0.1", Port: 8080}
	if err := lb.Install(cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	found := false
	for _, c := range calls {
		if strings.Contains(c, "launchctl") && strings.Contains(c, "load") {
			found = true
		}
	}
	if !found {
		t.Errorf("launchctl load not called, got: %v", calls)
	}
}

func TestLaunchd_Uninstall(t *testing.T) {
	var calls []string
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.openclaw.dashboard.plist")
	_ = os.WriteFile(plistPath, []byte("<plist/>"), 0o644)

	lb := &launchdBackend{
		plistDir: dir,
		runCmd: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
	}
	if err := lb.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(plistPath); !errors.Is(err, os.ErrNotExist) {
		t.Error("plist file should be removed after Uninstall")
	}
	found := false
	for _, c := range calls {
		if strings.Contains(c, "launchctl") && strings.Contains(c, "unload") {
			found = true
		}
	}
	if !found {
		t.Errorf("launchctl unload not called, got: %v", calls)
	}
}

func TestLaunchd_Lifecycle(t *testing.T) {
	tests := []struct {
		name        string
		op          func(*launchdBackend) error
		wantInCalls []string
	}{
		{
			"Start",
			(*launchdBackend).Start,
			[]string{"launchctl start com.openclaw.dashboard"},
		},
		{
			"Stop",
			(*launchdBackend).Stop,
			[]string{"launchctl stop com.openclaw.dashboard"},
		},
		{
			"Restart",
			(*launchdBackend).Restart,
			[]string{"launchctl stop com.openclaw.dashboard", "launchctl start com.openclaw.dashboard"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			lb := &launchdBackend{
				plistDir: t.TempDir(),
				runCmd: func(name string, args ...string) ([]byte, error) {
					calls = append(calls, strings.Join(append([]string{name}, args...), " "))
					return nil, nil
				},
			}
			if err := tc.op(lb); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			for _, want := range tc.wantInCalls {
				found := false
				for _, c := range calls {
					if strings.Contains(c, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: %q not in calls: %v", tc.name, want, calls)
				}
			}
		})
	}
}

func TestLaunchd_Status_notInstalled(t *testing.T) {
	lb := &launchdBackend{
		plistDir:  t.TempDir(),
		probeFunc: func(string) bool { return false },
		runCmd: func(name string, args ...string) ([]byte, error) {
			return []byte("Could not find service"), fmt.Errorf("exit status 113")
		},
	}
	st, err := lb.Status()
	if err != nil {
		t.Fatalf("Status should not error for uninstalled service: %v", err)
	}
	if st.Running {
		t.Error("uninstalled service should not be Running")
	}
	if st.AutoStart {
		t.Error("uninstalled service should not have AutoStart")
	}
}

func TestLaunchd_Status_runningService(t *testing.T) {
	dir := t.TempDir()
	plistContent := `<?xml version="1.0"?>
<plist version="1.0"><dict>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/openclaw-dashboard</string>
    <string>--port</string>
    <string>9090</string>
  </array>
</dict></plist>`
	_ = os.WriteFile(filepath.Join(dir, "com.openclaw.dashboard.plist"), []byte(plistContent), 0o644)

	lb := &launchdBackend{
		plistDir:  dir,
		probeFunc: func(string) bool { return true },
		runCmd: func(name string, args ...string) ([]byte, error) {
			if strings.Contains(strings.Join(args, " "), "list") {
				return []byte(`{ "PID" = 48291; "LastExitStatus" = 0; };`), nil
			}
			if name == "ps" {
				return []byte("Tue Apr  7 00:00:00 2026"), nil
			}
			return nil, nil
		},
	}
	st, err := lb.Status()
	if err != nil {
		t.Fatalf("Status error: %v", err)
	}
	if !st.Running {
		t.Error("expected Running=true")
	}
	if st.PID != 48291 {
		t.Errorf("PID = %d, want 48291", st.PID)
	}
	if st.Port != 9090 {
		t.Errorf("Port = %d, want 9090", st.Port)
	}
	if !st.AutoStart {
		t.Error("expected AutoStart=true (plist exists)")
	}
	if st.Backend != "LaunchAgent" {
		t.Errorf("Backend = %q, want LaunchAgent", st.Backend)
	}
	if st.Uptime <= 0 {
		t.Error("expected Uptime > 0 for running service with valid ps output")
	}
}

func TestLaunchd_Status_pidButNoHTTP(t *testing.T) {
	dir := t.TempDir()
	plistContent := `<?xml version="1.0"?>
<plist version="1.0"><dict>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/d</string>
    <string>--port</string>
    <string>19999</string>
  </array>
</dict></plist>`
	_ = os.WriteFile(filepath.Join(dir, "com.openclaw.dashboard.plist"), []byte(plistContent), 0o644)

	lb := &launchdBackend{
		plistDir:  dir,
		probeFunc: func(string) bool { return false },
		runCmd: func(name string, args ...string) ([]byte, error) {
			if strings.Contains(strings.Join(args, " "), "list") {
				return []byte(`{ "PID" = 99999; "LastExitStatus" = 0; };`), nil
			}
			return nil, nil
		},
	}
	st, err := lb.Status()
	if err != nil {
		t.Fatalf("Status error: %v", err)
	}
	// PID exists but HTTP probe fails (stub returns false) → Running=false
	if st.Running {
		t.Error("Running should be false when HTTP probe fails")
	}
	if st.PID != 99999 {
		t.Errorf("PID = %d, want 99999", st.PID)
	}
}

func TestLaunchd_parsePlistPort(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantPort int
	}{
		{
			"port 8080",
			`<array><string>/bin/d</string><string>--port</string><string>8080</string></array>`,
			8080,
		},
		{
			"port 9999",
			`<array><string>/bin/d</string><string>--port</string><string>9999</string></array>`,
			9999,
		},
		{"no port", `<array><string>/bin/d</string></array>`, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePlistPort(tc.content)
			if got != tc.wantPort {
				t.Errorf("parsePlistPort: got %d, want %d\ncontent: %s", got, tc.wantPort, tc.content)
			}
		})
	}
}

func TestLaunchd_parseLaunchctlPID(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		wantPID int
	}{
		{"running", `{ "PID" = 48291; "LastExitStatus" = 0; };`, 48291},
		{"stopped", `{ "LastExitStatus" = 0; };`, 0},
		{"empty", "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLaunchctlPID(tc.out)
			if got != tc.wantPID {
				t.Errorf("parseLaunchctlPID(%q) = %d, want %d", tc.out, got, tc.wantPID)
			}
		})
	}
}
