package dashboard

import (
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appservice"
)

// captureStdout redirects os.Stdout for the duration of fn and returns whatever
// was written. serviceStatus renders via fmt.Print to the real stdout, so this
// is the only way to assert the user-visible status output.
func captureStdout(t *testing.T, fn func()) (out string) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	var buf strings.Builder
	var wg sync.WaitGroup

	os.Stdout = w
	defer func() {
		os.Stdout = orig
		_ = w.Close()
		wg.Wait()
		_ = r.Close()
		out = buf.String()
	}()

	wg.Add(1)
	go func() { defer wg.Done(); _, _ = io.Copy(&buf, r) }()

	fn()
	return out
}

// captureStderr mirrors captureStdout for os.Stderr. The service dispatch writes
// usage and error diagnostics to stderr via fmt.Fprintf(os.Stderr, ...).
func captureStderr(t *testing.T, fn func()) (out string) {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	var buf strings.Builder
	var wg sync.WaitGroup

	os.Stderr = w
	defer func() {
		os.Stderr = orig
		_ = w.Close()
		wg.Wait()
		_ = r.Close()
		out = buf.String()
	}()

	wg.Add(1)
	go func() { defer wg.Done(); _, _ = io.Copy(&buf, r) }()

	fn()
	return out
}

// fakeBackend records which methods were called and with what args.
type fakeBackend struct {
	installedWith *appservice.InstallConfig
	uninstalled   bool
	started       bool
	stopped       bool
	restarted     bool
	statusCalled  bool
	statusResult  appservice.ServiceStatus

	errInstall   error
	errUninstall error
	errStart     error
	errStop      error
	errRestart   error
	errStatus    error
}

func (f *fakeBackend) Install(cfg appservice.InstallConfig) error {
	f.installedWith = &cfg
	return f.errInstall
}
func (f *fakeBackend) Uninstall() error { f.uninstalled = true; return f.errUninstall }
func (f *fakeBackend) Start() error     { f.started = true; return f.errStart }
func (f *fakeBackend) Stop() error      { f.stopped = true; return f.errStop }
func (f *fakeBackend) Restart() error   { f.restarted = true; return f.errRestart }
func (f *fakeBackend) Status() (appservice.ServiceStatus, error) {
	f.statusCalled = true
	return f.statusResult, f.errStatus
}

// TestServiceCmdRecognisedSubcommandsDispatch verifies, through observable
// behavior, that every subcommand Main accepts (serviceCmdSet) is one that
// runServiceCmd can actually execute. Rather than comparing two unexported maps
// for equality (which only proves the data structures match, not that dispatch
// works), each recognised subcommand is driven through normaliseCmd → runServiceCmd
// and must reach its backend method (exit 0), while an unrecognised subcommand
// must be rejected with a non-zero exit and a usage message. This catches drift
// between the pre-flag recognition set and the action table at the behavior level.
func TestServiceCmdRecognisedSubcommandsDispatch(t *testing.T) {
	for sub := range serviceCmdSet {
		t.Run(sub, func(t *testing.T) {
			// normaliseCmd is the front door Main uses; confirm it preserves the
			// subcommand for both the bare and "service <sub>" forms.
			if got, _ := normaliseCmd([]string{sub}); got != sub {
				t.Fatalf("normaliseCmd([%q]) = %q, want %q", sub, got, sub)
			}
			if got, _ := normaliseCmd([]string{"service", sub}); got != sub {
				t.Fatalf("normaliseCmd([service %q]) = %q, want %q", sub, got, sub)
			}

			fb := &fakeBackend{}
			var code int
			out := captureStdout(t, func() {
				code = runServiceCmd(sub, baseOpts(fb, nil))
			})
			if code != 0 {
				t.Fatalf("runServiceCmd(%q) exit = %d, want 0; out=%q", sub, code, out)
			}
			// Each recognised subcommand must have driven a backend call.
			reached := fb.installedWith != nil || fb.uninstalled || fb.started ||
				fb.stopped || fb.restarted || fb.statusCalled
			if !reached {
				t.Fatalf("runServiceCmd(%q) returned 0 but no backend method was reached", sub)
			}
		})
	}

	t.Run("unrecognised subcommand rejected", func(t *testing.T) {
		fb := &fakeBackend{}
		code := runServiceCmd("bogus", baseOpts(fb, nil))
		if code == 0 {
			t.Fatal("runServiceCmd(\"bogus\") exit = 0, want non-zero")
		}
	})
}

func TestNormaliseCmd(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCmd  string
		wantRest []string
	}{
		{"direct start", []string{"start"}, "start", nil},
		{"direct stop", []string{"stop"}, "stop", nil},
		{"service start", []string{"service", "start"}, "start", nil},
		{"service install with flags", []string{"service", "install", "--port", "9090"}, "install", []string{"--port", "9090"}},
		{"bare service", []string{"service"}, "", nil},
		{"empty args", []string{}, "", nil},
		{"direct status", []string{"status"}, "status", nil},
		{"service status", []string{"service", "status"}, "status", nil},
		{"flag arg intercepted", []string{"--version"}, "", nil},
		{"unknown cmd passes through", []string{"bogus"}, "bogus", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, rest := normaliseCmd(tc.args)
			if cmd != tc.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tc.wantCmd)
			}
			if !slices.Equal(rest, tc.wantRest) {
				t.Errorf("rest = %v, want %v", rest, tc.wantRest)
			}
		})
	}
}

func baseOpts(fb *fakeBackend, args []string) serviceCmdOpts {
	return serviceCmdOpts{
		dir:         "/tmp/dir",
		binPath:     "/tmp/bin",
		version:     "v1.0",
		backend:     fb,
		args:        args,
		defaultBind: "127.0.0.1",
		defaultPort: 8080,
	}
}

func TestRunServiceCmd(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		args     []string
		setupFb  func(*fakeBackend)
		wantCode int
		checkFb  func(*testing.T, *fakeBackend)
	}{
		{
			name:     "install forwards bind, port and paths",
			cmd:      "install",
			args:     []string{"--bind", "localhost", "--port", "9090"},
			wantCode: 0,
			checkFb: func(t *testing.T, fb *fakeBackend) {
				t.Helper()
				if fb.installedWith == nil {
					t.Fatal("Install not called")
				}
				if fb.installedWith.Host != "localhost" {
					t.Errorf("Host = %q, want localhost", fb.installedWith.Host)
				}
				if fb.installedWith.Port != 9090 {
					t.Errorf("port = %d, want 9090", fb.installedWith.Port)
				}
				if fb.installedWith.WorkDir != "/tmp/dir" {
					t.Errorf("WorkDir = %q, want /tmp/dir", fb.installedWith.WorkDir)
				}
				if fb.installedWith.BinPath != "/tmp/bin" {
					t.Errorf("BinPath = %q, want /tmp/bin", fb.installedWith.BinPath)
				}
			},
		},
		{
			name:     "install defaults bind to defaultBind from opts",
			cmd:      "install",
			wantCode: 0,
			checkFb: func(t *testing.T, fb *fakeBackend) {
				t.Helper()
				if fb.installedWith == nil {
					t.Fatal("Install not called")
				}
				// baseOpts sets defaultBind=127.0.0.1, defaultPort=8080.
				if fb.installedWith.Host != "127.0.0.1" {
					t.Errorf("Host = %q, want 127.0.0.1 (defaultBind)", fb.installedWith.Host)
				}
				if fb.installedWith.Port != 8080 {
					t.Errorf("Port = %d, want 8080 (defaultPort)", fb.installedWith.Port)
				}
			},
		},
		{
			name:     "install rejects non-loopback bind without env override",
			cmd:      "install",
			args:     []string{"--bind", "0.0.0.0"},
			wantCode: 1,
			checkFb: func(t *testing.T, fb *fakeBackend) {
				t.Helper()
				if fb.installedWith != nil {
					t.Fatal("Install must not be called when loopback validation fails")
				}
			},
		},
		{
			name:     "start calls Start",
			cmd:      "start",
			wantCode: 0,
			checkFb: func(t *testing.T, fb *fakeBackend) {
				t.Helper()
				if !fb.started {
					t.Error("Start not called")
				}
			},
		},
		{
			name:     "stop calls Stop",
			cmd:      "stop",
			wantCode: 0,
			checkFb: func(t *testing.T, fb *fakeBackend) {
				t.Helper()
				if !fb.stopped {
					t.Error("Stop not called")
				}
			},
		},
		{
			name:     "restart calls Restart",
			cmd:      "restart",
			wantCode: 0,
			checkFb: func(t *testing.T, fb *fakeBackend) {
				t.Helper()
				if !fb.restarted {
					t.Error("Restart not called")
				}
			},
		},
		{
			name:     "uninstall calls Uninstall",
			cmd:      "uninstall",
			wantCode: 0,
			checkFb: func(t *testing.T, fb *fakeBackend) {
				t.Helper()
				if !fb.uninstalled {
					t.Error("Uninstall not called")
				}
			},
		},
		{
			name:     "status calls Status",
			cmd:      "status",
			wantCode: 0,
			checkFb: func(t *testing.T, fb *fakeBackend) {
				t.Helper()
				if !fb.statusCalled {
					t.Error("Status not called")
				}
			},
		},
		{
			name:     "error propagates as exit 1",
			cmd:      "start",
			setupFb:  func(fb *fakeBackend) { fb.errStart = errors.New("launchctl failed") },
			wantCode: 1,
		},
		{
			name:     "invalid flag returns exit 1 without executing command",
			cmd:      "status",
			args:     []string{"--port", "nope"},
			wantCode: 1,
			checkFb: func(t *testing.T, fb *fakeBackend) {
				t.Helper()
				if fb.statusCalled {
					t.Fatal("Status should not be called when flag parsing fails")
				}
			},
		},
		{
			name:     "unexpected positional args return exit 1 without executing command",
			cmd:      "status",
			args:     []string{"extra"},
			wantCode: 1,
			checkFb: func(t *testing.T, fb *fakeBackend) {
				t.Helper()
				if fb.statusCalled {
					t.Fatal("Status should not be called when unexpected args are present")
				}
			},
		},
		{
			name:     "unknown cmd returns exit 1",
			cmd:      "bogus",
			wantCode: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fb := &fakeBackend{}
			if tc.setupFb != nil {
				tc.setupFb(fb)
			}
			var code int
			// Capture stdout so success-path renderings (e.g. status) do not
			// pollute test output; the rendered content itself is asserted in
			// TestServiceStatusRendersResult.
			_ = captureStdout(t, func() {
				code = runServiceCmd(tc.cmd, baseOpts(fb, tc.args))
			})
			if code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d", code, tc.wantCode)
			}
			if tc.checkFb != nil {
				tc.checkFb(t, fb)
			}
		})
	}
}

// TestServiceStatusRendersResult verifies that the status subcommand does not
// merely call Status() but renders the returned ServiceStatus to stdout. The
// previous test set statusResult and only checked the statusCalled flag, so a
// regression that dropped the rendering (or rendered the wrong fields) would
// have gone unnoticed.
func TestServiceStatusRendersResult(t *testing.T) {
	fb := &fakeBackend{
		statusResult: appservice.ServiceStatus{Running: true, PID: 999, Port: 8080},
	}
	var code int
	out := captureStdout(t, func() {
		code = runServiceCmd("status", baseOpts(fb, nil))
	})
	if code != 0 {
		t.Fatalf("status exit = %d, want 0; out=%q", code, out)
	}
	if !fb.statusCalled {
		t.Fatal("Status not called")
	}
	// FormatStatus output reflects the fields of the returned status. Assert the
	// observable facts (running state, PID, port, version) rather than exact
	// formatting so the test tracks behavior, not whitespace.
	for _, want := range []string{"running", "999", "8080", "v1.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q; got:\n%s", want, out)
		}
	}
}

// TestRunServiceCmdUnknownPrintsUsage is a facade-level smoke test for the
// service-dispatch path Main relies on: an unrecognised subcommand must exit
// non-zero AND emit a usage line to stderr so the operator learns the valid
// subcommands. The backend must never be touched.
func TestRunServiceCmdUnknownPrintsUsage(t *testing.T) {
	fb := &fakeBackend{}
	var code int
	stderr := captureStderr(t, func() {
		code = runServiceCmd("bogus", baseOpts(fb, nil))
	})
	if code == 0 {
		t.Fatalf("unknown service command exit = %d, want non-zero", code)
	}
	if !strings.Contains(stderr, "unknown service command") {
		t.Errorf("stderr missing 'unknown service command'; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr missing usage line; got:\n%s", stderr)
	}
	if fb.installedWith != nil || fb.started || fb.stopped || fb.restarted ||
		fb.uninstalled || fb.statusCalled {
		t.Error("backend must not be invoked for an unknown service command")
	}
}

// TestRunServiceCmdInstallNonLoopbackRejected is a facade-level smoke test for
// the loopback-only gate baked into the install path: installing with a
// non-loopback --bind and no OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK override must
// exit non-zero and never reach the backend's Install.
func TestRunServiceCmdInstallNonLoopbackRejected(t *testing.T) {
	// Ensure the override is absent so the gate is active.
	t.Setenv("OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK", "")
	fb := &fakeBackend{}
	var code int
	stderr := captureStderr(t, func() {
		code = runServiceCmd("install", baseOpts(fb, []string{"--bind", "0.0.0.0"}))
	})
	if code == 0 {
		t.Fatalf("install --bind 0.0.0.0 exit = %d, want non-zero", code)
	}
	if fb.installedWith != nil {
		t.Fatal("Install must not be called when the loopback gate rejects the bind")
	}
	if !strings.Contains(stderr, "loopback") {
		t.Errorf("stderr should explain the loopback policy; got:\n%s", stderr)
	}
}

// TestRunServiceCmdInstallNonLoopbackAllowedWithEnv confirms the documented
// escape hatch: with OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK=1 a non-loopback
// bind is forwarded to Install rather than rejected.
func TestRunServiceCmdInstallNonLoopbackAllowedWithEnv(t *testing.T) {
	t.Setenv("OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK", "1")
	fb := &fakeBackend{}
	var code int
	_ = captureStdout(t, func() {
		code = runServiceCmd("install", baseOpts(fb, []string{"--bind", "0.0.0.0"}))
	})
	if code != 0 {
		t.Fatalf("install with override exit = %d, want 0", code)
	}
	if fb.installedWith == nil {
		t.Fatal("Install not called despite the env override")
	}
	if fb.installedWith.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want 0.0.0.0", fb.installedWith.Host)
	}
}
