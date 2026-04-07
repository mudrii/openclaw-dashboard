//go:build darwin

package appservice

import (
	"fmt"
	"net/http"
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
		runCmd:   execRun,
	}, nil
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
	// ignore stop error — service may not be running
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
	if pid > 0 && st.Port > 0 {
		st.PID = pid
		st.Uptime = resolveUptime(lb.runCmd, pid)
		if probeHTTP(fmt.Sprintf("http://127.0.0.1:%d/", st.Port)) {
			st.Running = true
		}
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
// Handles both single-line: { "PID" = 12345; ... }
// and multi-line formats where "PID" appears on its own line.
func parseLaunchctlPID(out string) int {
	// Tokenise on semicolons and newlines to handle both formats.
	for _, segment := range strings.FieldsFunc(out, func(r rune) bool {
		return r == ';' || r == '\n'
	}) {
		segment = strings.TrimSpace(segment)
		// Strip leading brace if present: `{ "PID" = 12345`
		segment = strings.TrimPrefix(segment, "{")
		segment = strings.TrimSpace(segment)
		if strings.HasPrefix(segment, `"PID"`) {
			parts := strings.SplitN(segment, "=", 2)
			if len(parts) == 2 {
				s := strings.TrimSpace(parts[1])
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
	if start < 0 || end < 0 || end <= start+len("<string>") {
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
	if start < 0 || end < 0 || end <= start+len("<string>") {
		return ""
	}
	return strings.TrimSpace(rest[start+len("<string>") : end])
}

// probeHTTP returns true if the URL responds within 2 seconds.
func probeHTTP(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// resolveUptime fetches the process start time via ps and computes elapsed duration.
func resolveUptime(run runCmdFunc, pid int) time.Duration {
	out, err := run("ps", "-p", strconv.Itoa(pid), "-o", "lstart=")
	if err != nil || len(out) == 0 {
		return 0
	}
	s := strings.TrimSpace(string(out))
	// macOS lstart format varies: "Tue Apr  8 10:00:00 2026"
	for _, layout := range []string{
		"Mon Jan _2 15:04:05 2006",
		"Mon Jan  2 15:04:05 2006",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Since(t)
		}
	}
	return 0
}

// tailFile reads the last n lines of a file. Returns nil on error.
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
