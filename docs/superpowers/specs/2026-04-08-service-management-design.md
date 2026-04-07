# Service Management Design

**Date:** 2026-04-08  
**Status:** Approved  
**Scope:** Add `start`, `stop`, `restart`, `install`, `uninstall`, `status` subcommands to `openclaw-dashboard`

---

## Problem

`openclaw-dashboard` has no built-in service management. Users must rely on `install.sh` / `uninstall.sh` shell scripts to register, start, and stop the service. There is no way to check service state from the binary. The goal is to make the binary self-sufficient for the full service lifecycle.

---

## CLI Surface

All six subcommands are available both directly and under the `service` namespace:

```
openclaw-dashboard install          # register service + start it
openclaw-dashboard uninstall        # stop + deregister (config/data preserved)
openclaw-dashboard start            # start a registered service
openclaw-dashboard stop             # stop a running service
openclaw-dashboard restart          # stop + start
openclaw-dashboard status           # rich status output

# service namespace aliases (identical behaviour):
openclaw-dashboard service install
openclaw-dashboard service uninstall
openclaw-dashboard service start
openclaw-dashboard service stop
openclaw-dashboard service restart
openclaw-dashboard service status
```

`install` forwards `--bind` and `--port` from the CLI into the generated plist / unit file so the service starts with the user's chosen network config. All other existing flags (`--version`, `--refresh`) are unaffected.

### Status output format

```
openclaw-dashboard v2026.3.23
Status:     running
PID:        48291
Uptime:     3h 12m
Port:       8080
Auto-start: enabled (LaunchAgent)

--- recent log ---
[dashboard] v2026.3.23
[dashboard] Serving on http://127.0.0.1:8080/
[dashboard] Refresh endpoint: /api/refresh (debounce: 30s)
```

---

## Architecture

### New package: `internal/appservice/`

```
internal/appservice/
  service.go          — Backend interface, ServiceStatus, InstallConfig, New() factory
  launchd.go          — macOS implementation  (build tag: darwin)
  launchd_test.go
  systemd.go          — Linux implementation  (build tag: linux)
  systemd_test.go
  unsupported.go      — stub returning ErrUnsupported for other platforms
```

### Backend interface

```go
type Backend interface {
    Install(cfg InstallConfig) error
    Uninstall() error
    Start() error
    Stop() error
    Restart() error
    Status() (ServiceStatus, error)
}

type InstallConfig struct {
    BinPath string // absolute path to the openclaw-dashboard binary
    WorkDir string // dashboard runtime directory (config.json lives here)
    LogPath string // stdout/stderr destination (WorkDir/server.log)
    Host    string // --bind value baked into the unit file
    Port    int    // --port value baked into the unit file
}

type ServiceStatus struct {
    Running   bool
    PID       int
    Uptime    time.Duration
    Port      int
    AutoStart bool
    Backend   string   // "LaunchAgent" | "systemd user service"
    LogLines  []string // last 20 lines from log file
}
```

`New() Backend` selects the correct implementation at runtime. Platform differences are fully contained inside the two backend files via Go build tags. `main.go` and the subcommand dispatcher are platform-agnostic.

---

## Platform Backends

### macOS — launchd (`launchd.go`, `//go:build darwin`)

| Operation   | Implementation |
|-------------|---------------|
| `Install`   | Write plist → `~/Library/LaunchAgents/com.openclaw.dashboard.plist`; `launchctl load` |
| `Uninstall` | `launchctl unload`; remove plist |
| `Start`     | `launchctl start com.openclaw.dashboard` |
| `Stop`      | `launchctl stop com.openclaw.dashboard` |
| `Restart`   | `Stop()` + `Start()` |
| `Status`    | `launchctl list com.openclaw.dashboard` → parse PID + exit status; read last 20 log lines; parse port from existing plist `ProgramArguments`; probe `http://localhost:<port>/` for liveness |

Plist configuration: `RunAtLoad=true`, `KeepAlive=true`, `WorkingDirectory={WorkDir}`, `StandardOutPath={LogPath}`, `StandardErrorPath={LogPath}`.

### Linux — systemd (`systemd.go`, `//go:build linux`)

| Operation   | Implementation |
|-------------|---------------|
| `Install`   | Write unit → `~/.config/systemd/user/openclaw-dashboard.service`; `daemon-reload`; `enable`; `start` |
| `Uninstall` | `stop`; `disable`; remove unit file; `daemon-reload` |
| `Start`     | `systemctl --user start openclaw-dashboard` |
| `Stop`      | `systemctl --user stop openclaw-dashboard` |
| `Restart`   | `systemctl --user restart openclaw-dashboard` |
| `Status`    | `systemctl --user show` → parse `MainPID`, `ActiveState`, `ActiveEnterTimestamp`; `journalctl --user -u openclaw-dashboard -n 20 --no-pager` for log lines |

Unit file configuration: `Type=simple`, `Restart=always`, `RestartSec=5`, `WantedBy=default.target`.

Both backends use `exec.CommandContext` with a 10 s timeout. All errors wrap command stderr output via `fmt.Errorf("...: %w", err)`.

---

## main.go Changes

`Main()` inspects `os.Args` before `flag.Parse()`. A `normaliseCmd` helper collapses `["service", "start"]` → `"start"`. `["service"]` with no subcommand prints a short usage message and exits 1. If a service subcommand is recognised the binary runs it and returns — the existing server startup path is completely untouched.

```go
// pseudocode — not final
cmd, rest := normaliseCmd(os.Args[1:])
switch cmd {
case "install", "uninstall", "start", "stop", "restart", "status":
    return runServiceCmd(cmd, dir, bind, port, rest)
}
// existing flag.Parse() + server startup unchanged
```

`runServiceCmd` parses `--bind` and `--port` from `rest` (needed only for `install`), constructs `InstallConfig`, calls `appservice.New()`, and delegates.

---

## Relationship to install.sh / uninstall.sh

`install.sh` and `uninstall.sh` are unchanged. They remain the curl-pipe installation path. The binary subcommands are an additive surface for users who already have the binary and want to manage the service without shell scripts.

---

## Testing

| Layer | Strategy |
|-------|----------|
| Subcommand routing | `fakeBackend` implementing `Backend` — verifies correct method called, correct `InstallConfig` fields |
| Plist / unit file generation | Table-driven tests with `t.TempDir()`; assert file content without invoking `launchctl`/`systemctl` |
| `exec.Command` calls | Injected `runCmd func(ctx, name, args) ([]byte, error)` field on each backend struct |
| Status output renderer | Table-driven tests over `ServiceStatus` values |
| Integration tests | Skipped unless `INTEGRATION=1` env var is set; avoids CI dependency on launchctl/systemctl |

No mocking frameworks. Fakes and injected functions only (go-rigor).

---

## Out of Scope

- System-wide service install (requires root; user-scoped only)
- Windows support
- Log streaming / `follow` mode
- Replacing `install.sh` / `uninstall.sh`
