//go:build linux

package appservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const systemdUnitName = "openclaw-dashboard"

var unitTmpl = template.Must(template.New("unit").Funcs(template.FuncMap{
	"systemdQuote": strconv.Quote,
}).Parse(`[Unit]
Description=OpenClaw Dashboard Server
After=network.target

[Service]
Type=simple
WorkingDirectory={{systemdQuote .WorkDir}}
Environment={{systemdQuote (printf "OPENCLAW_HOME=%s" .OpenclawHome)}}
Environment={{systemdQuote (printf "PATH=%s" .PathEnv)}}
ExecStart={{systemdQuote .BinPath}} --bind {{systemdQuote .Host}} --port {{.Port}}
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`))

type unitData struct {
	BinPath      string
	Host         string
	Port         int
	WorkDir      string
	OpenclawHome string
	PathEnv      string
}

var unitPortRe = regexp.MustCompile(`(?:^|\s)--port\s+"?([0-9]+)"?`)

type systemdBackend struct {
	ctx       context.Context
	unitDir   string
	runCmd    runCmdFunc
	probeFunc func(string) bool
}

// New returns a systemd user-service Backend for Linux.
func New() (Backend, error) {
	return NewWithContext(context.Background())
}

// NewWithContext returns a systemd user-service Backend for Linux bound to a caller context.
func NewWithContext(ctx context.Context) (Backend, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &systemdBackend{
		ctx:       ctx,
		unitDir:   filepath.Join(home, ".config", "systemd", "user"),
		runCmd:    execRun,
		probeFunc: probeHTTP,
	}, nil
}

func (sb *systemdBackend) unitPath() string {
	return filepath.Join(sb.unitDir, systemdUnitName+".service")
}

func (sb *systemdBackend) ctl(args ...string) ([]byte, error) {
	return sb.runCmd(sb.ctx, "systemctl", append([]string{"--user"}, args...)...)
}

func (sb *systemdBackend) Install(cfg InstallConfig) error {
	// systemd captures stdout/stderr into the journal directly, so cfg.LogPath
	// is intentionally ignored here. launchd validates LogPath because the
	// plist redirects stdout/err to that path; do not mirror this check in the
	// systemd backend without first updating unitTmpl to consume LogPath.
	if err := validateAbsPath(cfg.BinPath); err != nil {
		return fmt.Errorf("BinPath: %w", err)
	}
	if err := validateAbsPath(cfg.WorkDir); err != nil {
		return fmt.Errorf("WorkDir: %w", err)
	}
	if err := os.MkdirAll(sb.unitDir, 0o755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}
	openclawHome, err := systemdOpenclawHome()
	if err != nil {
		return fmt.Errorf("resolve OPENCLAW_HOME: %w", err)
	}
	data := unitData{
		BinPath:      cfg.BinPath,
		Host:         cfg.Host,
		Port:         cfg.Port,
		WorkDir:      cfg.WorkDir,
		OpenclawHome: openclawHome,
		PathEnv:      systemdPathEnv(),
	}
	var buf bytes.Buffer
	if err := unitTmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render unit file: %w", err)
	}
	if err := writeFileAtomic(sb.unitPath(), buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	if out, err := sb.ctl("daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := sb.ctl("enable", systemdUnitName); err != nil {
		return fmt.Errorf("enable: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// Use restart so a reinstall with changed --bind/--port/Env actually picks
	// up the new unit content. systemctl restart starts the unit if it is not
	// currently running, so this also works for first installs.
	if out, err := sb.ctl("restart", systemdUnitName); err != nil {
		return fmt.Errorf("restart: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func systemdOpenclawHome() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("OPENCLAW_HOME")); raw != "" {
		if err := validateAbsPath(raw); err != nil {
			return "", fmt.Errorf("OPENCLAW_HOME: %w", err)
		}
		return raw, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("OPENCLAW_HOME unset and home directory unknown")
	}
	if err := validateAbsPath(home); err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".openclaw"), nil
}

func systemdPathEnv() string {
	return joinAbsPaths(
		strings.Split(os.Getenv("PATH"), ":"),
		[]string{
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
			"/usr/sbin",
			"/sbin",
		},
	)
}

func (sb *systemdBackend) Uninstall() error {
	p := sb.unitPath()
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("service not installed (unit file not found: %s)", p)
	}
	if out, err := sb.ctl("stop", systemdUnitName); err != nil && !isBenignSystemctlFailure(out) {
		slog.Warn("systemd stop during uninstall failed", "output", strings.TrimSpace(string(out)), "error", err)
	}
	if out, err := sb.ctl("disable", systemdUnitName); err != nil && !isBenignSystemctlFailure(out) {
		slog.Warn("systemd disable during uninstall failed", "output", strings.TrimSpace(string(out)), "error", err)
	}
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("remove unit file: %w", err)
	}
	if _, err := sb.ctl("daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload after uninstall: %w", err)
	}
	return nil
}

// isBenignSystemctlFailure returns true when systemctl's stderr indicates the
// unit was already in the desired state (e.g., stop on a stopped unit, disable
// on a disabled unit). These are routine outcomes during Uninstall and should
// not surface as warnings; only genuine failures (permission denied, bus
// error, etc.) should reach the operator's logs.
func isBenignSystemctlFailure(out []byte) bool {
	s := strings.ToLower(string(out))
	for _, marker := range []string{
		"not loaded",
		"not active",
		"no such file or directory",
		"failed to disable unit: file does not exist",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
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
		return st, nil // not installed or not running
	}
	props := parseSystemctlProps(string(out))

	if n, err := strconv.Atoi(props["MainPID"]); err == nil && n > 0 {
		st.PID = n
	}
	if ts, ok := props["ActiveEnterTimestamp"]; ok && ts != "" {
		// systemd's default ActiveEnterTimestamp carries a leading weekday
		// (e.g. "Tue 2026-04-08 10:00:00 UTC"). Try the weekday layout first,
		// then fall back to the bare form for non-default --timestamp settings.
		for _, layout := range []string{"Mon 2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 MST"} {
			if t, err := time.Parse(layout, ts); err == nil {
				st.Uptime = time.Since(t)
				break
			}
		}
	}

	// Running requires active state + HTTP probe
	if props["ActiveState"] == "active" && st.Port > 0 {
		if sb.probeFunc(fmt.Sprintf("http://127.0.0.1:%d/", st.Port)) {
			st.Running = true
		}
	}

	// Last 20 log lines via journalctl (only if process is active)
	if props["ActiveState"] == "active" {
		logOut, err := sb.runCmd(sb.ctx, "journalctl", "--user", "-u", systemdUnitName, "-n", "20", "--no-pager")
		if err == nil {
			lines := strings.Split(strings.TrimRight(string(logOut), "\n"), "\n")
			if len(lines) > 0 && lines[0] != "" {
				st.LogLines = lines
			}
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
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		if match := unitPortRe.FindStringSubmatch(line); len(match) == 2 {
			n, _ := strconv.Atoi(match[1])
			return n
		}
	}
	return 0
}
