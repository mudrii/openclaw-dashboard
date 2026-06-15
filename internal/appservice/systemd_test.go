//go:build linux

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

func newTestSystemd(t *testing.T) (*systemdBackend, string) {
	t.Helper()
	dir := t.TempDir()
	sb := &systemdBackend{
		unitDir: dir,
		runCmd: func(context.Context, string, ...string) ([]byte, error) {
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	return sb, dir
}

func TestSystemd_Install_writesUnitFile(t *testing.T) {
	sb, dir := newTestSystemd(t)
	t.Setenv("HOME", "/home/user")
	t.Setenv("PATH", "/usr/local/bin:/usr/bin:/bin")
	t.Setenv("OPENCLAW_HOME", "/srv/openclaw")
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
		`Environment="OPENCLAW_HOME=/srv/openclaw"`,
		`Environment="PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("unit file missing %q\ncontent:\n%s", want, content)
		}
	}
}

func TestSystemd_Install_PersistsNonLoopbackOverride(t *testing.T) {
	sb, dir := newTestSystemd(t)
	t.Setenv("OPENCLAW_HOME", "/srv/openclaw")
	cfg := InstallConfig{
		BinPath:          "/usr/local/bin/openclaw-dashboard",
		WorkDir:          "/home/user/.openclaw/dashboard",
		Host:             "0.0.0.0",
		Port:             9090,
		AllowNonLoopback: true,
	}
	if err := sb.Install(cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "openclaw-dashboard.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `Environment="OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK=1"`) {
		t.Fatalf("unit missing non-loopback override env:\n%s", data)
	}
}

func TestSystemd_Install_quotesPathsWithSpaces(t *testing.T) {
	sb, dir := newTestSystemd(t)
	cfg := InstallConfig{
		BinPath: "/home/test user/bin/openclaw-dashboard",
		WorkDir: "/home/test user/.openclaw/dashboard",
		Host:    "127.0.0.1",
		Port:    8080,
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
	if !strings.Contains(content, `WorkingDirectory="/home/test user/.openclaw/dashboard"`) {
		t.Fatalf("expected quoted working directory, got:\n%s", content)
	}
	// Assert the quoted binary path and the port token appear on the ExecStart
	// line, rather than pinning the entire line verbatim. The quoting of the
	// space-bearing path is the behavior under test; arg ordering and spacing
	// are not part of this contract.
	execLine := execStartLine(t, content)
	if !strings.Contains(execLine, `"/home/test user/bin/openclaw-dashboard"`) {
		t.Errorf("ExecStart should quote the space-bearing binary path, got: %q", execLine)
	}
	if !strings.Contains(execLine, "--port 8080") {
		t.Errorf("ExecStart should carry the --port token, got: %q", execLine)
	}
}

// execStartLine returns the ExecStart= line from a rendered unit file.
func execStartLine(t *testing.T, unit string) string {
	t.Helper()
	for _, line := range strings.Split(unit, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "ExecStart=") {
			return line
		}
	}
	t.Fatalf("unit file has no ExecStart line:\n%s", unit)
	return ""
}

func TestSystemd_Install_callsSystemctl(t *testing.T) {
	var calls []string
	dir := t.TempDir()
	sb := &systemdBackend{
		unitDir: dir,
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	cfg := InstallConfig{BinPath: "/bin/d", WorkDir: "/tmp", LogPath: "/tmp/s.log", Host: "127.0.0.1", Port: 8080}
	if err := sb.Install(cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, w := range []string{"daemon-reload", "enable", "restart"} {
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
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
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

// stubSystemd builds a backend whose runCmd dispatches by systemctl verb,
// letting tests model realistic command outcomes without asserting argument
// strings or call order. verbResults maps a systemctl verb to its result.
func stubSystemd(t *testing.T, verbResults map[string]stubResult) *systemdBackend {
	t.Helper()
	return &systemdBackend{
		unitDir: t.TempDir(),
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
			// ctl prepends "--user"; the verb is the first non-flag arg.
			verb := ""
			if name == "systemctl" {
				for _, a := range args {
					if a != "--user" {
						verb = a
						break
					}
				}
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

func TestSystemd_Start(t *testing.T) {
	t.Run("succeeds when systemctl start exits 0", func(t *testing.T) {
		if err := stubSystemd(t, nil).Start(); err != nil {
			t.Errorf("Start: %v", err)
		}
	})

	t.Run("surfaces systemctl failure output", func(t *testing.T) {
		sb := stubSystemd(t, map[string]stubResult{
			"start": {out: []byte("Unit not found"), err: errors.New("exit status 5")},
		})
		err := sb.Start()
		if err == nil {
			t.Fatal("expected Start to error on systemctl failure")
		}
		if !strings.Contains(err.Error(), "Unit not found") {
			t.Errorf("error should include systemctl output, got: %v", err)
		}
	})
}

func TestSystemd_Stop(t *testing.T) {
	t.Run("succeeds when systemctl stop exits 0", func(t *testing.T) {
		if err := stubSystemd(t, nil).Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})

	t.Run("surfaces systemctl failure output", func(t *testing.T) {
		sb := stubSystemd(t, map[string]stubResult{
			"stop": {out: []byte("Job failed"), err: errors.New("exit status 1")},
		})
		err := sb.Stop()
		if err == nil {
			t.Fatal("expected Stop to error on systemctl failure")
		}
		if !strings.Contains(err.Error(), "Job failed") {
			t.Errorf("error should include systemctl output, got: %v", err)
		}
	})
}

func TestSystemd_Restart(t *testing.T) {
	t.Run("succeeds when systemctl restart exits 0", func(t *testing.T) {
		if err := stubSystemd(t, nil).Restart(); err != nil {
			t.Errorf("Restart: %v", err)
		}
	})

	t.Run("surfaces systemctl failure output", func(t *testing.T) {
		sb := stubSystemd(t, map[string]stubResult{
			"restart": {out: []byte("start-limit-hit"), err: errors.New("exit status 1")},
		})
		err := sb.Restart()
		if err == nil {
			t.Fatal("expected Restart to error on systemctl failure")
		}
		if !strings.Contains(err.Error(), "start-limit-hit") {
			t.Errorf("error should include systemctl output, got: %v", err)
		}
	})
}

func TestSystemd_New(t *testing.T) {
	t.Run("New returns a configured systemd backend", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		b, err := New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		sb, ok := b.(*systemdBackend)
		if !ok {
			t.Fatalf("New returned %T, want *systemdBackend", b)
		}
		if sb.runCmd == nil {
			t.Error("runCmd should be wired")
		}
		if sb.probeFunc == nil {
			t.Error("probeFunc should be wired")
		}
		if !strings.HasSuffix(sb.unitDir, filepath.Join(".config", "systemd", "user")) {
			t.Errorf("unitDir = %q, want a .../.config/systemd/user path", sb.unitDir)
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
		if b.(*systemdBackend).ctx.Value(ctxKey{}) != "v" {
			t.Error("NewWithContext did not retain the caller context")
		}
	})

	t.Run("NewWithContext tolerates a nil context", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		//nolint:staticcheck // exercising the nil-ctx fallback
		b, err := NewWithContext(nil)
		if err != nil {
			t.Fatalf("NewWithContext(nil): %v", err)
		}
		if b.(*systemdBackend).ctx == nil {
			t.Error("ctx should default to non-nil")
		}
	})
}

func TestSystemd_Status_notInstalled(t *testing.T) {
	sb := &systemdBackend{
		unitDir:   t.TempDir(),
		probeFunc: func(string) bool { return false },
		runCmd: func(context.Context, string, ...string) ([]byte, error) {
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
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
			joined := name + " " + strings.Join(args, " ")
			if strings.Contains(joined, "show") {
				// systemd's default timestamp format carries a leading weekday.
				return []byte("ActiveState=active\nMainPID=55555\nActiveEnterTimestamp=Wed 2026-04-08 10:00:00 UTC\n"), nil
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
	if st.Uptime <= 0 {
		t.Error("expected Uptime > 0 parsed from weekday-prefixed ActiveEnterTimestamp")
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
