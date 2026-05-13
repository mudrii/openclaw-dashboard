package dashboard

import (
	"errors"
	"slices"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appservice"
)

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

// TestServiceCmdSetMatchesActions ensures the pre-flag subcommand recognition
// set stays in lockstep with the actual action dispatch table. If they drift,
// Main may accept a subcommand that runServiceCmd cannot execute (or vice versa).
func TestServiceCmdSetMatchesActions(t *testing.T) {
	want := map[string]struct{}{
		"install":   {},
		"uninstall": {},
		"start":     {},
		"stop":      {},
		"restart":   {},
		"status":    {},
	}
	if len(serviceCmdSet) != len(want) {
		t.Fatalf("serviceCmdSet size = %d, want %d", len(serviceCmdSet), len(want))
	}
	for k := range want {
		if _, ok := serviceCmdSet[k]; !ok {
			t.Errorf("serviceCmdSet missing key %q", k)
		}
	}
	actions := serviceActions()
	if len(actions) != len(want) {
		t.Fatalf("serviceActions size = %d, want %d", len(actions), len(want))
	}
	for k := range want {
		if _, ok := actions[k]; !ok {
			t.Errorf("serviceActions missing key %q", k)
		}
	}
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
			name:     "install forwards port and paths",
			cmd:      "install",
			args:     []string{"--port", "9090"},
			wantCode: 0,
			checkFb: func(t *testing.T, fb *fakeBackend) {
				t.Helper()
				if fb.installedWith == nil {
					t.Fatal("Install not called")
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
			name: "status calls Status",
			cmd:  "status",
			setupFb: func(fb *fakeBackend) {
				fb.statusResult = appservice.ServiceStatus{Running: true, PID: 999, Port: 8080}
			},
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
			code := runServiceCmd(tc.cmd, baseOpts(fb, tc.args))
			if code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d", code, tc.wantCode)
			}
			if tc.checkFb != nil {
				tc.checkFb(t, fb)
			}
		})
	}
}
