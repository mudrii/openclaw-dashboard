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

func TestNormaliseCmd(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCmd  string
		wantRest []string
	}{
		{"direct start", []string{"start"}, "start", []string{}},
		{"direct stop", []string{"stop"}, "stop", []string{}},
		{"service start", []string{"service", "start"}, "start", []string{}},
		{"service install with flags", []string{"service", "install", "--port", "9090"}, "install", []string{"--port", "9090"}},
		{"bare service", []string{"service"}, "", nil},
		{"empty args", []string{}, "", nil},
		{"direct status", []string{"status"}, "status", []string{}},
		{"service status", []string{"service", "status"}, "status", []string{}},
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

func TestRunServiceCmd_install(t *testing.T) {
	fb := &fakeBackend{}
	code := runServiceCmd("install", "/tmp/dir", "/tmp/bin", "v1.0", fb, []string{"--port", "9090"}, "127.0.0.1", 8080)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if fb.installedWith == nil {
		t.Fatal("Install was not called")
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
}

func TestRunServiceCmd_start(t *testing.T) {
	fb := &fakeBackend{}
	code := runServiceCmd("start", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil, "127.0.0.1", 8080)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !fb.started {
		t.Error("Start was not called")
	}
}

func TestRunServiceCmd_stop(t *testing.T) {
	fb := &fakeBackend{}
	code := runServiceCmd("stop", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil, "127.0.0.1", 8080)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !fb.stopped {
		t.Error("Stop was not called")
	}
}

func TestRunServiceCmd_restart(t *testing.T) {
	fb := &fakeBackend{}
	code := runServiceCmd("restart", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil, "127.0.0.1", 8080)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !fb.restarted {
		t.Error("Restart was not called")
	}
}

func TestRunServiceCmd_uninstall(t *testing.T) {
	fb := &fakeBackend{}
	code := runServiceCmd("uninstall", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil, "127.0.0.1", 8080)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !fb.uninstalled {
		t.Error("Uninstall was not called")
	}
}

func TestRunServiceCmd_status(t *testing.T) {
	fb := &fakeBackend{
		statusResult: appservice.ServiceStatus{
			Running: true,
			PID:     999,
			Port:    8080,
		},
	}
	code := runServiceCmd("status", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil, "127.0.0.1", 8080)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !fb.statusCalled {
		t.Error("Status was not called")
	}
}

func TestRunServiceCmd_errorPropagation(t *testing.T) {
	fb := &fakeBackend{errStart: errors.New("launchctl failed")}
	code := runServiceCmd("start", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil, "127.0.0.1", 8080)
	if code != 1 {
		t.Errorf("expected exit 1 on error, got %d", code)
	}
}

func TestRunServiceCmd_unknownCmd(t *testing.T) {
	fb := &fakeBackend{}
	code := runServiceCmd("bogus", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil, "127.0.0.1", 8080)
	if code != 1 {
		t.Errorf("expected exit 1 for unknown cmd, got %d", code)
	}
}
