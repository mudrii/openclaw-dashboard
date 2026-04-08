//go:build linux

package appservice

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
ExecStart={{systemdQuote .BinPath}} --bind {{systemdQuote .Host}} --port {{.Port}}
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
	unitDir   string
	runCmd    runCmdFunc
	probeFunc func(string) bool
}

// New returns a systemd user-service Backend for Linux.
func New() (Backend, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	return &systemdBackend{
		unitDir:   filepath.Join(home, ".config", "systemd", "user"),
		runCmd:    execRun,
		probeFunc: probeHTTP,
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
	defer func() { _ = f.Close() }()
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
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("service not installed (unit file not found: %s)", p)
	}
	_, _ = sb.ctl("stop", systemdUnitName)
	_, _ = sb.ctl("disable", systemdUnitName)
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("remove unit file: %w", err)
	}
	if _, err := sb.ctl("daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload after uninstall: %w", err)
	}
	return nil
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
		if t, err := time.Parse("2006-01-02 15:04:05 MST", ts); err == nil {
			st.Uptime = time.Since(t)
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
		logOut, err := sb.runCmd("journalctl", "--user", "-u", systemdUnitName, "-n", "20", "--no-pager")
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
