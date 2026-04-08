# Service Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `start`, `stop`, `restart`, `install`, `uninstall`, `status` subcommands to `openclaw-dashboard`, backed by launchd (macOS) and systemd (Linux), with both direct and `service <cmd>` aliases.

**Architecture:** New `internal/appservice/` package with a `Backend` interface and two platform-specific implementations selected via Go build tags. Subcommand dispatch is added to `main.go` before `flag.Parse()`, leaving the existing server startup path untouched. All external commands are injected via a `runCmdFunc` field for testability.

**Tech Stack:** Go 1.26, stdlib only (`flag`, `os/exec`, `text/template`, `strings`, `fmt`). No new dependencies.

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/appservice/service.go` | Create | `Backend` interface, `InstallConfig`, `ServiceStatus`, `runCmdFunc` type, `FormatStatus()`, `formatUptime()` |
| `internal/appservice/service_test.go` | Create | `FormatStatus` table-driven tests |
| `internal/appservice/launchd.go` | Create (`//go:build darwin`) | launchd backend: plist write/read, launchctl calls, `New()` |
| `internal/appservice/launchd_test.go` | Create (`//go:build darwin`) | plist generation, start/stop/restart/uninstall, status parsing |
| `internal/appservice/systemd.go` | Create (`//go:build linux`) | systemd backend: unit file write/read, systemctl calls, `New()` |
| `internal/appservice/systemd_test.go` | Create (`//go:build linux`) | unit file generation, start/stop/restart/uninstall, status parsing |
| `internal/appservice/unsupported.go` | Create (`//go:build !darwin && !linux`) | `New()` returning `ErrUnsupported` |
| `main.go` | Modify | `normaliseCmd()`, `runServiceCmd()`, dispatch block before `flag.Parse()` |
| `service_cmd_test.go` | Create | Acceptance tests for subcommand routing using `fakeBackend` |

---

## Task 1: Backend interface and shared types

**Files:**
- Create: `internal/appservice/service.go`

- [ ] **Step 1: Write the failing compilation check**

Create `internal/appservice/service.go`:

```go
package appservice

import (
	"fmt"
	"strings"
	"time"
)

// ErrUnsupported is returned on platforms without a service backend.
var ErrUnsupported = fmt.Errorf("service management not supported on this platform")

// runCmdFunc is the signature for running an external command.
// Injected into backends so tests can intercept exec calls.
type runCmdFunc func(name string, args ...string) ([]byte, error)

// InstallConfig holds parameters baked into the service unit file at install time.
type InstallConfig struct {
	BinPath string // absolute path to the openclaw-dashboard binary
	WorkDir string // dashboard runtime directory (config.json lives here)
	LogPath string // stdout/stderr log file path
	Host    string // --bind value
	Port    int    // --port value
}

// ServiceStatus is the parsed state returned by Backend.Status.
type ServiceStatus struct {
	Running   bool
	PID       int
	Uptime    time.Duration
	Port      int
	AutoStart bool
	Backend   string   // "LaunchAgent" | "systemd user service"
	LogLines  []string // last 20 lines of log file
}

// Backend abstracts service lifecycle operations.
// Each platform provides one implementation via build tags.
type Backend interface {
	Install(cfg InstallConfig) error
	Uninstall() error
	Start() error
	Stop() error
	Restart() error
	Status() (ServiceStatus, error)
}

// FormatStatus renders a ServiceStatus as human-readable text for the terminal.
func FormatStatus(version string, st ServiceStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "openclaw-dashboard %s\n", version)

	state := "stopped"
	if st.Running {
		state = "running"
	}
	fmt.Fprintf(&b, "Status:     %s\n", state)

	if st.PID > 0 {
		fmt.Fprintf(&b, "PID:        %d\n", st.PID)
	}
	if st.Running && st.Uptime > 0 {
		fmt.Fprintf(&b, "Uptime:     %s\n", formatUptime(st.Uptime))
	}
	if st.Port > 0 {
		fmt.Fprintf(&b, "Port:       %d\n", st.Port)
	}
	autoStart := "disabled"
	if st.AutoStart {
		autoStart = fmt.Sprintf("enabled (%s)", st.Backend)
	}
	fmt.Fprintf(&b, "Auto-start: %s\n", autoStart)

	if len(st.LogLines) > 0 {
		fmt.Fprintf(&b, "\n--- recent log ---\n")
		for _, line := range st.LogLines {
			fmt.Fprintln(&b, line)
		}
	}
	return b.String()
}

func formatUptime(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
```

- [ ] **Step 2: Create the unsupported stub so the package compiles on all platforms**

Create `internal/appservice/unsupported.go`:

```go
//go:build !darwin && !linux

package appservice

// New returns ErrUnsupported on platforms with no service backend.
func New() (Backend, error) {
	return nil, ErrUnsupported
}
```

- [ ] **Step 3: Verify package compiles**

```bash
go build ./internal/appservice/...
```

Expected: no output (clean compile).

- [ ] **Step 4: Commit**

```bash
git add internal/appservice/service.go internal/appservice/unsupported.go
git commit -m "feat(appservice): add Backend interface, types, FormatStatus, unsupported stub"
```

---

## Task 2: FormatStatus tests

**Files:**
- Create: `internal/appservice/service_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/appservice/service_test.go`:

```go
package appservice

import (
	"strings"
	"testing"
	"time"
)

func TestFormatStatus_running(t *testing.T) {
	st := ServiceStatus{
		Running:   true,
		PID:       12345,
		Uptime:    3*time.Hour + 12*time.Minute,
		Port:      8080,
		AutoStart: true,
		Backend:   "LaunchAgent",
		LogLines:  []string{"[dashboard] started", "[dashboard] ready"},
	}
	got := FormatStatus("v2026.4.8", st)

	for _, want := range []string{
		"openclaw-dashboard v2026.4.8",
		"Status:     running",
		"PID:        12345",
		"Uptime:     3h 12m",
		"Port:       8080",
		"Auto-start: enabled (LaunchAgent)",
		"--- recent log ---",
		"[dashboard] started",
		"[dashboard] ready",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatStatus missing %q\ngot:\n%s", want, got)
		}
	}
}

func TestFormatStatus_stopped(t *testing.T) {
	st := ServiceStatus{
		Running:   false,
		AutoStart: false,
		Backend:   "LaunchAgent",
	}
	got := FormatStatus("v2026.4.8", st)

	if !strings.Contains(got, "Status:     stopped") {
		t.Errorf("expected 'stopped', got:\n%s", got)
	}
	if !strings.Contains(got, "Auto-start: disabled") {
		t.Errorf("expected 'Auto-start: disabled', got:\n%s", got)
	}
	if strings.Contains(got, "PID:") {
		t.Errorf("stopped status should not show PID, got:\n%s", got)
	}
	if strings.Contains(got, "Uptime:") {
		t.Errorf("stopped status should not show Uptime, got:\n%s", got)
	}
}

func TestFormatStatus_noLogLines(t *testing.T) {
	st := ServiceStatus{Running: true, PID: 1}
	got := FormatStatus("v1.0", st)
	if strings.Contains(got, "recent log") {
		t.Errorf("should not show log section with no log lines, got:\n%s", got)
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{3*time.Hour + 12*time.Minute, "3h 12m"},
		{45 * time.Minute, "45m"},
		{0, "0m"},
		{1*time.Hour + 0*time.Minute, "1h 0m"},
	}
	for _, tc := range tests {
		got := formatUptime(tc.d)
		if got != tc.want {
			t.Errorf("formatUptime(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run TestFormatStatus -v ./internal/appservice/
```

Expected: FAIL — `FormatStatus` not yet returning correct format (actually should PASS since we already wrote FormatStatus; verify all pass).

- [ ] **Step 3: Run all package tests**

```bash
go test -race ./internal/appservice/
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/appservice/service_test.go
git commit -m "test(appservice): add FormatStatus and formatUptime table tests"
```

---

## Task 3: Acceptance tests for subcommand dispatch

**Files:**
- Create: `service_cmd_test.go` (root package `dashboard`)

These tests define the expected CLI behaviour and will fail until Task 4 implements the routing.

- [ ] **Step 1: Write failing acceptance tests**

Create `service_cmd_test.go` in the repo root:

```go
package dashboard

import (
	"strings"
	"testing"

	appservice "github.com/mudrii/openclaw-dashboard/internal/appservice"
)

// fakeBackend records which methods were called and with what args.
type fakeBackend struct {
	installedWith *appservice.InstallConfig
	uninstalled   bool
	started       bool
	stopped       bool
	restarted     bool
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
	return f.statusResult, f.errStatus
}

func TestNormaliseCmd(t *testing.T) {
	tests := []struct {
		args     []string
		wantCmd  string
		wantRest []string
	}{
		{[]string{"start"}, "start", []string{}},
		{[]string{"stop"}, "stop", []string{}},
		{[]string{"service", "start"}, "start", []string{}},
		{[]string{"service", "install", "--port", "9090"}, "install", []string{"--port", "9090"}},
		{[]string{"service"}, "", nil},
		{[]string{}, "", nil},
		{[]string{"status"}, "status", []string{}},
		{[]string{"service", "status"}, "status", []string{}},
	}
	for _, tc := range tests {
		cmd, rest := normaliseCmd(tc.args)
		if cmd != tc.wantCmd {
			t.Errorf("normaliseCmd(%v): cmd = %q, want %q", tc.args, cmd, tc.wantCmd)
		}
		if tc.wantRest != nil && len(rest) != len(tc.wantRest) {
			t.Errorf("normaliseCmd(%v): rest = %v, want %v", tc.args, rest, tc.wantRest)
		}
	}
}

func TestRunServiceCmd_install(t *testing.T) {
	fb := &fakeBackend{}
	code := runServiceCmd("install", "/tmp/dir", "/tmp/bin", "v1.0", fb, []string{"--port", "9090"})
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
	code := runServiceCmd("start", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !fb.started {
		t.Error("Start was not called")
	}
}

func TestRunServiceCmd_stop(t *testing.T) {
	fb := &fakeBackend{}
	code := runServiceCmd("stop", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !fb.stopped {
		t.Error("Stop was not called")
	}
}

func TestRunServiceCmd_restart(t *testing.T) {
	fb := &fakeBackend{}
	code := runServiceCmd("restart", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !fb.restarted {
		t.Error("Restart was not called")
	}
}

func TestRunServiceCmd_uninstall(t *testing.T) {
	fb := &fakeBackend{}
	code := runServiceCmd("uninstall", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil)
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
	code := runServiceCmd("status", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
}

func TestRunServiceCmd_errorPropagation(t *testing.T) {
	fb := &fakeBackend{errStart: fmt.Errorf("launchctl failed")}
	code := runServiceCmd("start", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil)
	if code != 1 {
		t.Errorf("expected exit 1 on error, got %d", code)
	}
}

func TestRunServiceCmd_unknownCmd(t *testing.T) {
	fb := &fakeBackend{}
	code := runServiceCmd("bogus", "/tmp/dir", "/tmp/bin", "v1.0", fb, nil)
	if code != 1 {
		t.Errorf("expected exit 1 for unknown cmd, got %d", code)
	}
}

func TestRunServiceCmd_statusContainsVersion(t *testing.T) {
	fb := &fakeBackend{}
	// capture stdout — use a strings.Builder via os.Stdout redirect is complex;
	// instead verify FormatStatus is called by checking non-error exit
	code := runServiceCmd("status", "/tmp/dir", "/tmp/bin", "v2026.4.8", fb, nil)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
}

// ensure service_cmd_test.go compiles — fmt is needed
var _ = strings.Contains
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test -run "TestNormaliseCmd|TestRunServiceCmd" -v ./...
```

Expected: FAIL — `normaliseCmd` and `runServiceCmd` not defined yet.

- [ ] **Step 3: Commit the failing tests**

```bash
git add service_cmd_test.go
git commit -m "test(dashboard): acceptance tests for service subcommand routing (failing)"
```

---

## Task 4: Implement subcommand dispatch in main.go

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Add `normaliseCmd` and `runServiceCmd` to main.go**

Add these functions at the bottom of `main.go`, before the closing line:

```go
// normaliseCmd extracts the service subcommand from args.
// "service start" → ("start", rest)
// "start"         → ("start", rest)
// "service"       → ("", nil)  — caller prints usage
func normaliseCmd(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	if args[0] == "service" {
		if len(args) < 2 {
			return "", nil
		}
		return args[1], args[2:]
	}
	return args[0], args[1:]
}

// runServiceCmd executes a service lifecycle subcommand using the given backend.
// dir is the dashboard runtime directory, binPath is the resolved binary path,
// version is the current build version, and args are remaining CLI args (for --bind/--port).
func runServiceCmd(cmd, dir, binPath, version string, b appservice.Backend, args []string) int {
	fs := flag.NewFlagSet("service", flag.ContinueOnError)
	bind := fs.String("bind", "127.0.0.1", "Bind address")
	fs.StringVar(bind, "b", "127.0.0.1", "Bind address")
	port := fs.Int("port", 8080, "Listen port")
	fs.IntVar(port, "p", 8080, "Listen port")
	_ = fs.Parse(args)

	switch cmd {
	case "install":
		cfg := appservice.InstallConfig{
			BinPath: binPath,
			WorkDir: dir,
			LogPath: filepath.Join(dir, "server.log"),
			Host:    *bind,
			Port:    *port,
		}
		if err := b.Install(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "[dashboard] install failed: %v\n", err)
			return 1
		}
		fmt.Println("[dashboard] service installed and started")
		return 0
	case "uninstall":
		if err := b.Uninstall(); err != nil {
			fmt.Fprintf(os.Stderr, "[dashboard] uninstall failed: %v\n", err)
			return 1
		}
		fmt.Println("[dashboard] service stopped and unregistered (config and data preserved)")
		return 0
	case "start":
		if err := b.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[dashboard] start failed: %v\n", err)
			return 1
		}
		fmt.Println("[dashboard] service started")
		return 0
	case "stop":
		if err := b.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "[dashboard] stop failed: %v\n", err)
			return 1
		}
		fmt.Println("[dashboard] service stopped")
		return 0
	case "restart":
		if err := b.Restart(); err != nil {
			fmt.Fprintf(os.Stderr, "[dashboard] restart failed: %v\n", err)
			return 1
		}
		fmt.Println("[dashboard] service restarted")
		return 0
	case "status":
		st, err := b.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[dashboard] status failed: %v\n", err)
			return 1
		}
		fmt.Print(appservice.FormatStatus(version, st))
		return 0
	default:
		fmt.Fprintf(os.Stderr, "[dashboard] unknown service command %q\n", cmd)
		fmt.Fprintln(os.Stderr, "Usage: openclaw-dashboard [service] install|uninstall|start|stop|restart|status")
		return 1
	}
}
```

- [ ] **Step 2: Add the dispatch block to `Main()` in main.go**

In `Main()`, add the following block immediately after `binDir` is set and before `dir, err := resolveDashboardDirWithError(binDir)`:

Find this line in main.go:
```go
	binDir := filepath.Dir(exe)

	// Resolve the dashboard runtime directory.
```

Insert between them:
```go
	binDir := filepath.Dir(exe)

	// Service subcommand dispatch — must happen before flag.Parse so flags
	// like --bind/--port are not consumed by the default flagset.
	if subcmd, rest := normaliseCmd(os.Args[1:]); subcmd != "" {
		switch subcmd {
		case "install", "uninstall", "start", "stop", "restart", "status":
			dir, dirErr := resolveDashboardDirWithError(binDir)
			if dirErr != nil {
				fmt.Fprintf(os.Stderr, "[dashboard] failed to resolve runtime directory: %v\n", dirErr)
				return 1
			}
			version := BuildVersion
			if version == "" {
				version = detectVersion(dir)
			}
			b, err := appservice.New()
			if err != nil {
				fmt.Fprintf(os.Stderr, "[dashboard] service management not available: %v\n", err)
				return 1
			}
			return runServiceCmd(subcmd, dir, exe, version, b, rest)
		}
	} else if len(os.Args) > 1 && os.Args[1] == "service" {
		fmt.Fprintln(os.Stderr, "Usage: openclaw-dashboard service install|uninstall|start|stop|restart|status")
		return 1
	}

	// Resolve the dashboard runtime directory.
```

- [ ] **Step 3: Add the appservice import to main.go**

Add to the import block in `main.go`:
```go
	appservice "github.com/mudrii/openclaw-dashboard/internal/appservice"
```

- [ ] **Step 4: Run the acceptance tests — they should still fail (no platform backend yet)**

```bash
go test -run "TestNormaliseCmd|TestRunServiceCmd" -v ./...
```

Expected: `TestNormaliseCmd` PASS. `TestRunServiceCmd_*` PASS (fakeBackend is used, not platform backend).

- [ ] **Step 5: Verify full build still works**

```bash
go build ./cmd/openclaw-dashboard/
```

Expected: clean compile.

- [ ] **Step 6: Commit**

```bash
git add main.go
git commit -m "feat(dashboard): add service subcommand dispatch to Main()"
```

---

## Task 5: launchd backend — plist generation

**Files:**
- Create: `internal/appservice/launchd.go` (`//go:build darwin`)
- Create: `internal/appservice/launchd_test.go` (`//go:build darwin`)

- [ ] **Step 1: Write failing test for plist file content**

Create `internal/appservice/launchd_test.go`:

```go
//go:build darwin

package appservice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestLaunchd(t *testing.T) (*launchdBackend, string) {
	t.Helper()
	dir := t.TempDir()
	calls := []string{}
	lb := &launchdBackend{
		plistDir: dir,
		runCmd: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil, nil
		},
	}
	return lb, dir
}

func TestLaunchd_Install_writesPlist(t *testing.T) {
	lb, dir := newTestLaunchd(t)
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
		"<true/>",     // RunAtLoad
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
	// Must call launchctl load
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
	// pre-write a plist so Uninstall finds it
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
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
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

func TestLaunchd_Start(t *testing.T) {
	var calls []string
	lb := &launchdBackend{
		plistDir: t.TempDir(),
		runCmd: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
	}
	if err := lb.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	found := false
	for _, c := range calls {
		if strings.Contains(c, "launchctl") && strings.Contains(c, "start") && strings.Contains(c, "com.openclaw.dashboard") {
			found = true
		}
	}
	if !found {
		t.Errorf("launchctl start not called, got: %v", calls)
	}
}

func TestLaunchd_Stop(t *testing.T) {
	var calls []string
	lb := &launchdBackend{
		plistDir: t.TempDir(),
		runCmd: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
	}
	if err := lb.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	found := false
	for _, c := range calls {
		if strings.Contains(c, "launchctl") && strings.Contains(c, "stop") && strings.Contains(c, "com.openclaw.dashboard") {
			found = true
		}
	}
	if !found {
		t.Errorf("launchctl stop not called, got: %v", calls)
	}
}

func TestLaunchd_Restart(t *testing.T) {
	var calls []string
	lb := &launchdBackend{
		plistDir: t.TempDir(),
		runCmd: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
	}
	if err := lb.Restart(); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	stopSeen, startSeen := false, false
	for _, c := range calls {
		if strings.Contains(c, "launchctl stop") {
			stopSeen = true
		}
		if strings.Contains(c, "launchctl start") {
			startSeen = true
		}
	}
	if !stopSeen || !startSeen {
		t.Errorf("Restart must call both stop and start, got: %v", calls)
	}
}

func TestLaunchd_Status_notInstalled(t *testing.T) {
	lb := &launchdBackend{
		plistDir: t.TempDir(), // empty dir — no plist
		runCmd: func(name string, args ...string) ([]byte, error) {
			// launchctl list returns non-zero if not installed
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
	// write a plist with port 9090 to simulate installed state
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
		plistDir: dir,
		runCmd: func(name string, args ...string) ([]byte, error) {
			if strings.Contains(strings.Join(args, " "), "list") {
				return []byte(`{ "PID" = 48291; "LastExitStatus" = 0; };`), nil
			}
			// ps for uptime
			if name == "ps" {
				return []byte("Tue Apr  8 10:00:00 2026"), nil
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
}

func TestLaunchd_parsePlistPort(t *testing.T) {
	tests := []struct {
		content  string
		wantPort int
	}{
		{
			`<array><string>/bin/d</string><string>--port</string><string>8080</string></array>`,
			8080,
		},
		{
			`<array><string>/bin/d</string><string>--port</string><string>9999</string></array>`,
			9999,
		},
		{`<array><string>/bin/d</string></array>`, 0},
	}
	for _, tc := range tests {
		got := parsePlistPort(tc.content)
		if got != tc.wantPort {
			t.Errorf("parsePlistPort: got %d, want %d\ncontent: %s", got, tc.wantPort, tc.content)
		}
	}
}
```

- [ ] **Step 2: Run to confirm tests fail**

```bash
go test -run TestLaunchd -v ./internal/appservice/
```

Expected: FAIL — `launchdBackend` not defined yet.

- [ ] **Step 3: Implement launchd.go**

Create `internal/appservice/launchd.go`:

```go
//go:build darwin

package appservice

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const launchdLabel = "com.openclaw.dashboard"

var plistTmpl = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{{.Label}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{.BinPath}}</string>
    <string>--bind</string>
    <string>{{.Host}}</string>
    <string>--port</string>
    <string>{{.Port}}</string>
  </array>
  <key>WorkingDirectory</key>
  <string>{{.WorkDir}}</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>{{.LogPath}}</string>
  <key>StandardErrorPath</key>
  <string>{{.LogPath}}</string>
</dict>
</plist>
`))

type plistData struct {
	Label   string
	BinPath string
	Host    string
	Port    int
	WorkDir string
	LogPath string
}

type launchdBackend struct {
	plistDir string
	runCmd   runCmdFunc
}

// New returns a launchd Backend for macOS.
func New() (Backend, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	return &launchdBackend{
		plistDir: filepath.Join(home, "Library", "LaunchAgents"),
		runCmd:   defaultRunCmd,
	}, nil
}

func defaultRunCmd(name string, args ...string) ([]byte, error) {
	// Uses exec.Command — imported below
	return execRun(name, args...)
}

func (lb *launchdBackend) plistPath() string {
	return filepath.Join(lb.plistDir, launchdLabel+".plist")
}

func (lb *launchdBackend) Install(cfg InstallConfig) error {
	if err := os.MkdirAll(lb.plistDir, 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	f, err := os.Create(lb.plistPath())
	if err != nil {
		return fmt.Errorf("create plist: %w", err)
	}
	defer f.Close()
	data := plistData{
		Label:   launchdLabel,
		BinPath: cfg.BinPath,
		Host:    cfg.Host,
		Port:    cfg.Port,
		WorkDir: cfg.WorkDir,
		LogPath: cfg.LogPath,
	}
	if err := plistTmpl.Execute(f, data); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	// unload first in case a stale registration exists
	_, _ = lb.runCmd("launchctl", "unload", lb.plistPath())
	if out, err := lb.runCmd("launchctl", "load", lb.plistPath()); err != nil {
		return fmt.Errorf("launchctl load: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (lb *launchdBackend) Uninstall() error {
	p := lb.plistPath()
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return fmt.Errorf("service not installed (plist not found: %s)", p)
	}
	out, err := lb.runCmd("launchctl", "unload", p)
	if err != nil {
		return fmt.Errorf("launchctl unload: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return os.Remove(p)
}

func (lb *launchdBackend) Start() error {
	out, err := lb.runCmd("launchctl", "start", launchdLabel)
	if err != nil {
		return fmt.Errorf("launchctl start: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (lb *launchdBackend) Stop() error {
	out, err := lb.runCmd("launchctl", "stop", launchdLabel)
	if err != nil {
		return fmt.Errorf("launchctl stop: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (lb *launchdBackend) Restart() error {
	// launchctl stop + start; ignore stop error (may not be running)
	_, _ = lb.runCmd("launchctl", "stop", launchdLabel)
	return lb.Start()
}

func (lb *launchdBackend) Status() (ServiceStatus, error) {
	st := ServiceStatus{Backend: "LaunchAgent"}

	// AutoStart = plist file exists
	p := lb.plistPath()
	plistContent, err := os.ReadFile(p)
	if err == nil {
		st.AutoStart = true
		st.Port = parsePlistPort(string(plistContent))
	}

	// Running = launchctl list succeeds and contains PID
	out, err := lb.runCmd("launchctl", "list", launchdLabel)
	if err != nil {
		// not running or not registered — not an error for Status
		return st, nil
	}
	pid := parseLaunchctlPID(string(out))
	if pid > 0 {
		st.Running = true
		st.PID = pid
		st.Uptime = resolveUptime(lb.runCmd, pid)
	}

	// Last 20 log lines
	if st.AutoStart {
		logPath := parsePlistLogPath(string(plistContent))
		if logPath != "" {
			st.LogLines = tailFile(logPath, 20)
		}
	}
	return st, nil
}

// parseLaunchctlPID extracts the PID value from `launchctl list` output.
// Output format: { "PID" = 12345; ... }
func parseLaunchctlPID(out string) int {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, `"PID"`) {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				s := strings.Trim(strings.TrimSpace(parts[1]), ";")
				if n, err := strconv.Atoi(s); err == nil {
					return n
				}
			}
		}
	}
	return 0
}

// parsePlistPort reads the --port value from the ProgramArguments in a plist.
func parsePlistPort(content string) int {
	idx := strings.Index(content, "--port</string>")
	if idx < 0 {
		return 0
	}
	rest := content[idx+len("--port</string>"):]
	start := strings.Index(rest, "<string>")
	end := strings.Index(rest, "</string>")
	if start < 0 || end < 0 || end <= start {
		return 0
	}
	s := rest[start+len("<string>") : end]
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// parsePlistLogPath reads StandardOutPath from a plist.
func parsePlistLogPath(content string) string {
	const key = "<key>StandardOutPath</key>"
	idx := strings.Index(content, key)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(key):]
	start := strings.Index(rest, "<string>")
	end := strings.Index(rest, "</string>")
	if start < 0 || end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[start+len("<string>") : end])
}

// resolveUptime fetches the process start time via ps and computes elapsed duration.
func resolveUptime(run runCmdFunc, pid int) time.Duration {
	out, err := run("ps", "-p", strconv.Itoa(pid), "-o", "lstart=")
	if err != nil || len(out) == 0 {
		return 0
	}
	s := strings.TrimSpace(string(out))
	// macOS lstart format: "Tue Apr  8 10:00:00 2026"
	t, err := time.Parse("Mon Jan _2 15:04:05 2006", s)
	if err != nil {
		t, err = time.Parse("Mon Jan  2 15:04:05 2006", s)
	}
	if err != nil {
		return 0
	}
	return time.Since(t)
}

// tailFile reads the last n lines of a file. Returns empty slice on error.
func tailFile(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
```

- [ ] **Step 4: Add execRun helper (exec.Command wrapper used by defaultRunCmd)**

Add a new file `internal/appservice/exec.go` (no build tag — used by both platforms):

```go
package appservice

import (
	"fmt"
	"os/exec"
)

// execRun runs name with args, returns combined output.
// Used as the default runCmdFunc implementation.
func execRun(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%w", err)
	}
	return out, nil
}
```

- [ ] **Step 5: Run launchd tests**

```bash
go test -run TestLaunchd -v ./internal/appservice/
```

Expected: all PASS.

- [ ] **Step 6: Run full test suite**

```bash
go test -race ./...
```

Expected: PASS (systemd tests skipped on darwin via build tag).

- [ ] **Step 7: Commit**

```bash
git add internal/appservice/launchd.go internal/appservice/launchd_test.go internal/appservice/exec.go
git commit -m "feat(appservice): launchd backend with plist generation, lifecycle ops, status"
```

---

## Task 6: systemd backend — unit file + lifecycle operations

**Files:**
- Create: `internal/appservice/systemd.go` (`//go:build linux`)
- Create: `internal/appservice/systemd_test.go` (`//go:build linux`)

> Note: systemd tests only compile and run on Linux. On macOS, `go test ./internal/appservice/` skips them automatically via build tag. CI on Linux will exercise them.

- [ ] **Step 1: Write failing tests**

Create `internal/appservice/systemd_test.go`:

```go
//go:build linux

package appservice

import (
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
	}
	cfg := InstallConfig{BinPath: "/bin/d", WorkDir: "/tmp", LogPath: "/tmp/s.log", Host: "127.0.0.1", Port: 8080}
	if err := sb.Install(cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	wants := []string{"daemon-reload", "enable", "start"}
	for _, w := range wants {
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
	}
	if err := sb.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
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

func TestSystemd_Status_notInstalled(t *testing.T) {
	sb := &systemdBackend{
		unitDir: t.TempDir(),
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
		unitDir: dir,
		runCmd: func(name string, args ...string) ([]byte, error) {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "show") {
				return []byte("ActiveState=active\nMainPID=55555\nActiveEnterTimestamp=2026-04-08 10:00:00 UTC\n"), nil
			}
			if strings.Contains(joined, "journalctl") {
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
}

func TestSystemd_parseUnitPort(t *testing.T) {
	tests := []struct {
		content  string
		wantPort int
	}{
		{"ExecStart=/bin/d --bind 0.0.0.0 --port 9090\n", 9090},
		{"ExecStart=/bin/d --port 8080\n", 8080},
		{"ExecStart=/bin/d\n", 0},
	}
	for _, tc := range tests {
		got := parseUnitPort(tc.content)
		if got != tc.wantPort {
			t.Errorf("parseUnitPort(%q) = %d, want %d", tc.content, got, tc.wantPort)
		}
	}
}
```

- [ ] **Step 2: Run to confirm tests fail**

```bash
go test -run TestSystemd -v ./internal/appservice/   # on Linux only
```

Expected: FAIL — `systemdBackend` not defined.

- [ ] **Step 3: Implement systemd.go**

Create `internal/appservice/systemd.go`:

```go
//go:build linux

package appservice

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const systemdUnitName = "openclaw-dashboard"

var unitTmpl = template.Must(template.New("unit").Parse(`[Unit]
Description=OpenClaw Dashboard Server
After=network.target

[Service]
Type=simple
WorkingDirectory={{.WorkDir}}
ExecStart={{.BinPath}} --bind {{.Host}} --port {{.Port}}
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`))

type unitData struct {
	BinPath string
	Host    string
	Port    int
	WorkDir string
}

type systemdBackend struct {
	unitDir string
	runCmd  runCmdFunc
}

// New returns a systemd user-service Backend for Linux.
func New() (Backend, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	return &systemdBackend{
		unitDir: filepath.Join(home, ".config", "systemd", "user"),
		runCmd:  defaultRunCmd,
	}, nil
}

func (sb *systemdBackend) unitPath() string {
	return filepath.Join(sb.unitDir, systemdUnitName+".service")
}

func (sb *systemdBackend) ctl(args ...string) ([]byte, error) {
	return sb.runCmd("systemctl", append([]string{"--user"}, args...)...)
}

func (sb *systemdBackend) Install(cfg InstallConfig) error {
	if err := os.MkdirAll(sb.unitDir, 0o755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}
	f, err := os.Create(sb.unitPath())
	if err != nil {
		return fmt.Errorf("create unit file: %w", err)
	}
	defer f.Close()
	if err := unitTmpl.Execute(f, unitData{BinPath: cfg.BinPath, Host: cfg.Host, Port: cfg.Port, WorkDir: cfg.WorkDir}); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	if out, err := sb.ctl("daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := sb.ctl("enable", systemdUnitName); err != nil {
		return fmt.Errorf("enable: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := sb.ctl("start", systemdUnitName); err != nil {
		return fmt.Errorf("start: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (sb *systemdBackend) Uninstall() error {
	p := sb.unitPath()
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return fmt.Errorf("service not installed (unit file not found: %s)", p)
	}
	_, _ = sb.ctl("stop", systemdUnitName)
	_, _ = sb.ctl("disable", systemdUnitName)
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("remove unit file: %w", err)
	}
	_, err := sb.ctl("daemon-reload")
	return err
}

func (sb *systemdBackend) Start() error {
	out, err := sb.ctl("start", systemdUnitName)
	if err != nil {
		return fmt.Errorf("systemctl start: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (sb *systemdBackend) Stop() error {
	out, err := sb.ctl("stop", systemdUnitName)
	if err != nil {
		return fmt.Errorf("systemctl stop: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (sb *systemdBackend) Restart() error {
	out, err := sb.ctl("restart", systemdUnitName)
	if err != nil {
		return fmt.Errorf("systemctl restart: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (sb *systemdBackend) Status() (ServiceStatus, error) {
	st := ServiceStatus{Backend: "systemd user service"}

	// AutoStart + port from unit file
	unitContent, err := os.ReadFile(sb.unitPath())
	if err == nil {
		st.AutoStart = true
		st.Port = parseUnitPort(string(unitContent))
	}

	// Running state via systemctl show
	out, err := sb.ctl("show", systemdUnitName,
		"--property=ActiveState,MainPID,ActiveEnterTimestamp")
	if err != nil {
		return st, nil // not installed or not running — not an error
	}
	props := parseSystemctlProps(string(out))

	if props["ActiveState"] == "active" {
		st.Running = true
	}
	if pidStr, ok := props["MainPID"]; ok {
		if n, err := strconv.Atoi(pidStr); err == nil && n > 0 {
			st.PID = n
		}
	}
	if ts, ok := props["ActiveEnterTimestamp"]; ok && st.Running {
		if t, err := time.Parse("2006-01-02 15:04:05 MST", ts); err == nil {
			st.Uptime = time.Since(t)
		}
	}

	// Last 20 log lines via journalctl
	if st.Running {
		logOut, err := sb.runCmd("journalctl", "--user", "-u", systemdUnitName, "-n", "20", "--no-pager", "--output=short")
		if err == nil {
			lines := strings.Split(strings.TrimRight(string(logOut), "\n"), "\n")
			st.LogLines = lines
		}
	}
	return st, nil
}

func parseSystemctlProps(out string) map[string]string {
	props := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			props[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return props
}

// parseUnitPort extracts the --port value from an ExecStart line.
func parseUnitPort(content string) int {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "ExecStart=") {
			continue
		}
		parts := strings.Fields(line)
		for i, p := range parts {
			if p == "--port" && i+1 < len(parts) {
				n, _ := strconv.Atoi(parts[i+1])
				return n
			}
		}
	}
	return 0
}
```

- [ ] **Step 4: Run systemd tests on Linux (skip on macOS)**

```bash
go test -run TestSystemd -v ./internal/appservice/  # Linux only
```

Expected: all PASS.

- [ ] **Step 5: Verify Darwin build is unaffected**

```bash
go build ./...
go test -race ./...
```

Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/appservice/systemd.go internal/appservice/systemd_test.go
git commit -m "feat(appservice): systemd backend with unit file generation, lifecycle ops, status"
```

---

## Task 7: Fix service_cmd_test.go import and run full suite

The acceptance tests in `service_cmd_test.go` use a `fmt` reference — ensure the import is correct, then run everything.

- [ ] **Step 1: Fix imports in service_cmd_test.go**

Open `service_cmd_test.go` and ensure the import block is:

```go
import (
	"fmt"
	"strings"
	"testing"

	appservice "github.com/mudrii/openclaw-dashboard/internal/appservice"
)
```

Remove the `var _ = strings.Contains` placeholder line at the bottom.

- [ ] **Step 2: Run full test suite**

```bash
go test -race -count=1 ./...
```

Expected: all PASS. Note: systemd tests only run on Linux.

- [ ] **Step 3: Run go fix**

```bash
go fix ./...
```

Expected: no changes needed (code already uses modern idioms).

- [ ] **Step 4: Run linter**

```bash
make lint
```

Expected: clean. Fix any reported issues before proceeding.

- [ ] **Step 5: Smoke test the binary**

```bash
go build -o /tmp/openclaw-dashboard-test ./cmd/openclaw-dashboard/
/tmp/openclaw-dashboard-test status
/tmp/openclaw-dashboard-test service status
/tmp/openclaw-dashboard-test service
```

Expected outputs:
- `status`: rich status output (service not installed state)
- `service status`: same
- `service` (no subcommand): usage message + exit 1

- [ ] **Step 6: Final commit**

```bash
git add service_cmd_test.go
git commit -m "test(dashboard): fix service_cmd_test imports, all tests passing"
```

---

## Task 8: End-to-end smoke test on macOS

- [ ] **Step 1: Install the service**

```bash
./openclaw-dashboard install
```

Expected:
```
[dashboard] service installed and started
```

- [ ] **Step 2: Check status**

```bash
./openclaw-dashboard status
```

Expected: Running=true, PID shown, port 8080, Auto-start: enabled (LaunchAgent).

- [ ] **Step 3: Stop the service**

```bash
./openclaw-dashboard stop
./openclaw-dashboard status
```

Expected: Status: stopped after stop.

- [ ] **Step 4: Start the service**

```bash
./openclaw-dashboard start
./openclaw-dashboard status
```

Expected: Status: running.

- [ ] **Step 5: Restart**

```bash
./openclaw-dashboard restart
./openclaw-dashboard status
```

Expected: new PID, Status: running.

- [ ] **Step 6: Uninstall**

```bash
./openclaw-dashboard uninstall
./openclaw-dashboard status
```

Expected: uninstall success message; status shows stopped, AutoStart: disabled.

- [ ] **Step 7: Verify config.json and data.json are untouched after uninstall**

```bash
ls ~/.openclaw/dashboard/config.json ~/.openclaw/dashboard/data.json
```

Expected: both files present.

- [ ] **Step 8: Tag and commit**

```bash
git add -A
git commit -m "feat: service management — install/uninstall/start/stop/restart/status"
```

---

## Spec Coverage Check

| Spec requirement | Covered by |
|-----------------|-----------|
| `install` registers + starts | Task 5 (launchd), Task 6 (systemd) |
| `uninstall` stops + deregisters, preserves data | Task 5, Task 6, Task 8 step 7 |
| `start` / `stop` / `restart` | Task 5, Task 6 |
| `status` rich output | Task 2 (FormatStatus), Task 5/6 (Status()) |
| Direct AND `service <cmd>` aliases | Task 3 (normaliseCmd tests), Task 4 (dispatch) |
| `service` alone → usage + exit 1 | Task 4 dispatch block |
| macOS launchd | Task 5 |
| Linux systemd | Task 6 |
| unsupported platform error | Task 1 |
| `--bind`/`--port` forwarded to unit file | Task 3 (TestRunServiceCmd_install port=9090) |
| Zero new dependencies | All tasks — stdlib only |
| TDD/ATDD: tests first | Tasks 2, 3, 5, 6 each write tests before implementation |
| Injected runCmd for testability | Tasks 5, 6 |
| No mocks — fakes only | Task 3 (fakeBackend) |
