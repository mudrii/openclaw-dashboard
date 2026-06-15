//go:build darwin

package appservice

import (
	"context"
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
		runCmd: func(context.Context, string, ...string) ([]byte, error) {
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

func TestLaunchd_Install_PersistsNonLoopbackOverride(t *testing.T) {
	lb, dir := newTestLaunchd(t)
	t.Setenv("HOME", "/home/user")
	t.Setenv("OPENCLAW_HOME", "/srv/openclaw")
	cfg := InstallConfig{
		BinPath:          "/usr/local/bin/openclaw-dashboard",
		WorkDir:          "/home/user/.openclaw/dashboard",
		LogPath:          "/home/user/.openclaw/dashboard/server.log",
		Host:             "0.0.0.0",
		Port:             9090,
		AllowNonLoopback: true,
	}
	if err := lb.Install(cfg); err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "com.openclaw.dashboard.plist"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"<key>OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK</key>",
		"<string>1</string>",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("plist missing %q\n%s", want, content)
		}
	}
}

func TestLaunchd_Install_callsLaunchctl(t *testing.T) {
	var calls []string
	dir := t.TempDir()
	lb := &launchdBackend{
		plistDir: dir,
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
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
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
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

// stubLaunchd builds a backend whose runCmd dispatches by launchctl verb,
// letting tests model realistic command outcomes (success vs failure) without
// asserting exact argument strings or call order. verbResults maps a verb
// ("start", "stop", "load", ...) to the (output, error) the stub should return.
func stubLaunchd(t *testing.T, verbResults map[string]stubResult) *launchdBackend {
	t.Helper()
	return &launchdBackend{
		plistDir: t.TempDir(),
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
			verb := ""
			if name == "launchctl" && len(args) > 0 {
				verb = args[0]
			}
			if r, ok := verbResults[verb]; ok {
				return r.out, r.err
			}
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
}

type stubResult struct {
	out []byte
	err error
}

func TestLaunchd_Start(t *testing.T) {
	t.Run("succeeds when launchctl start exits 0", func(t *testing.T) {
		lb := stubLaunchd(t, nil)
		if err := lb.Start(); err != nil {
			t.Errorf("Start: %v", err)
		}
	})

	t.Run("surfaces launchctl failure output", func(t *testing.T) {
		lb := stubLaunchd(t, map[string]stubResult{
			"start": {out: []byte("Could not find service"), err: errors.New("exit status 113")},
		})
		err := lb.Start()
		if err == nil {
			t.Fatal("expected Start to error on launchctl failure")
		}
		if !strings.Contains(err.Error(), "Could not find service") {
			t.Errorf("error should include launchctl output, got: %v", err)
		}
	})
}

func TestLaunchd_Stop(t *testing.T) {
	t.Run("succeeds when launchctl stop exits 0", func(t *testing.T) {
		lb := stubLaunchd(t, nil)
		if err := lb.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})

	t.Run("surfaces launchctl failure output", func(t *testing.T) {
		lb := stubLaunchd(t, map[string]stubResult{
			"stop": {out: []byte("no such process"), err: errors.New("exit status 3")},
		})
		err := lb.Stop()
		if err == nil {
			t.Fatal("expected Stop to error on launchctl failure")
		}
		if !strings.Contains(err.Error(), "no such process") {
			t.Errorf("error should include launchctl output, got: %v", err)
		}
	})
}

func TestLaunchd_Restart(t *testing.T) {
	t.Run("succeeds even when the prior stop fails (service not running)", func(t *testing.T) {
		// Restart ignores the stop error and depends only on start succeeding.
		lb := stubLaunchd(t, map[string]stubResult{
			"stop": {err: errors.New("exit status 3")}, // not running
		})
		if err := lb.Restart(); err != nil {
			t.Errorf("Restart should ignore stop failure, got: %v", err)
		}
	})

	t.Run("fails when the underlying start fails", func(t *testing.T) {
		lb := stubLaunchd(t, map[string]stubResult{
			"start": {out: []byte("load failed"), err: errors.New("exit status 1")},
		})
		err := lb.Restart()
		if err == nil {
			t.Fatal("expected Restart to error when start fails")
		}
		if !strings.Contains(err.Error(), "load failed") {
			t.Errorf("error should include start output, got: %v", err)
		}
	})
}

func TestLaunchd_New(t *testing.T) {
	t.Run("New returns a configured launchd backend", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		b, err := New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		lb, ok := b.(*launchdBackend)
		if !ok {
			t.Fatalf("New returned %T, want *launchdBackend", b)
		}
		if lb.runCmd == nil {
			t.Error("runCmd should be wired")
		}
		if lb.probeFunc == nil {
			t.Error("probeFunc should be wired")
		}
		if !strings.HasSuffix(lb.plistDir, filepath.Join("Library", "LaunchAgents")) {
			t.Errorf("plistDir = %q, want a .../Library/LaunchAgents path", lb.plistDir)
		}
	})

	t.Run("NewWithContext binds the provided context", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		type ctxKey struct{}
		ctx := context.WithValue(context.Background(), ctxKey{}, "v")
		b, err := NewWithContext(ctx)
		if err != nil {
			t.Fatalf("NewWithContext: %v", err)
		}
		lb := b.(*launchdBackend)
		if lb.ctx.Value(ctxKey{}) != "v" {
			t.Error("NewWithContext did not retain the caller context")
		}
	})

	t.Run("NewWithContext tolerates a nil context", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		//lint:ignore SA1012 exercising the nil-ctx fallback
		//nolint:staticcheck // exercising the nil-ctx fallback
		b, err := NewWithContext(nil)
		if err != nil {
			t.Fatalf("NewWithContext(nil): %v", err)
		}
		if b.(*launchdBackend).ctx == nil {
			t.Error("ctx should default to non-nil")
		}
	})
}

func TestLaunchd_Status_notInstalled(t *testing.T) {
	lb := &launchdBackend{
		plistDir:  t.TempDir(),
		probeFunc: func(string) bool { return false },
		runCmd: func(context.Context, string, ...string) ([]byte, error) {
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
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
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
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
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

func TestParsePlistPort_Reformatted(t *testing.T) {
	// Reformatted: tags split across lines, extra whitespace inside <string>,
	// attribute order varied. The old strings.Cut implementation (which
	// relied on the literal "--port</string>" substring) would miss these.
	content := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
	<dict>
		<key>ProgramArguments</key>
		<array>
			<string >/bin/openclaw-dashboard</string>
			<string>--bind</string>
			<string>127.0.0.1</string>
			<string
			>--port</string
			>
			<string>  7777  </string>
		</array>
	</dict>
</plist>`
	if got := parsePlistPort(content); got != 7777 {
		t.Errorf("parsePlistPort reformatted: got %d, want 7777", got)
	}
}

func TestParsePlistLogPath_Reformatted(t *testing.T) {
	// Reformatted with split tags + whitespace; old string-cut would break
	// on the linebreak between "</key>" and "<string>".
	content := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
	<dict>
		<key>ProgramArguments</key>
		<array>
			<string>/bin/d</string>
			<string>--port</string>
			<string>8080</string>
		</array>
		<key
		>StandardOutPath</key
		>
		<string>
			/var/log/openclaw/dashboard.log
		</string>
	</dict>
</plist>`
	got := parsePlistLogPath(content)
	if got != "/var/log/openclaw/dashboard.log" {
		t.Errorf("parsePlistLogPath reformatted: got %q, want %q", got, "/var/log/openclaw/dashboard.log")
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
