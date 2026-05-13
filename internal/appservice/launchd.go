//go:build darwin

package appservice

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const launchdLabel = "com.openclaw.dashboard"

var plistTmpl = template.Must(template.New("plist").Funcs(template.FuncMap{
	"xmlText": xmlText,
}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{{xmlText .Label}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{xmlText .BinPath}}</string>
    <string>--bind</string>
    <string>{{xmlText .Host}}</string>
    <string>--port</string>
    <string>{{xmlText .Port}}</string>
  </array>
  <key>WorkingDirectory</key>
  <string>{{xmlText .WorkDir}}</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>{{xmlText .HomeDir}}</string>
    <key>PATH</key>
    <string>{{xmlText .PathEnv}}</string>
    <key>OPENCLAW_HOME</key>
    <string>{{xmlText .OpenclawHome}}</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>{{xmlText .LogPath}}</string>
  <key>StandardErrorPath</key>
  <string>{{xmlText .LogPath}}</string>
</dict>
</plist>
`))

type plistData struct {
	Label        string
	BinPath      string
	Host         string
	Port         int
	WorkDir      string
	LogPath      string
	HomeDir      string
	PathEnv      string
	OpenclawHome string
}

type launchdBackend struct {
	ctx       context.Context
	plistDir  string
	runCmd    runCmdFunc
	probeFunc func(string) bool
}

// New returns a launchd Backend for macOS.
func New() (Backend, error) {
	return NewWithContext(context.Background())
}

// NewWithContext returns a launchd Backend for macOS bound to a caller context.
func NewWithContext(ctx context.Context) (Backend, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &launchdBackend{
		ctx:       ctx,
		plistDir:  filepath.Join(home, "Library", "LaunchAgents"),
		runCmd:    execRun,
		probeFunc: probeHTTP,
	}, nil
}

func (lb *launchdBackend) plistPath() string {
	return filepath.Join(lb.plistDir, launchdLabel+".plist")
}

func (lb *launchdBackend) Install(cfg InstallConfig) error {
	if err := os.MkdirAll(lb.plistDir, 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	openclawHome, err := launchdOpenclawHome()
	if err != nil {
		return fmt.Errorf("resolve OPENCLAW_HOME: %w", err)
	}
	data := plistData{
		Label:        launchdLabel,
		BinPath:      cfg.BinPath,
		Host:         cfg.Host,
		Port:         cfg.Port,
		WorkDir:      cfg.WorkDir,
		LogPath:      cfg.LogPath,
		HomeDir:      userHomeDir(),
		PathEnv:      launchdPathEnv(),
		OpenclawHome: openclawHome,
	}
	var buf bytes.Buffer
	if err := plistTmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render plist: %w", err)
	}
	if err := writeFileAtomic(lb.plistPath(), buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	// unload first in case a stale registration exists
	_, _ = lb.runCmd(lb.ctx, "launchctl", "unload", lb.plistPath())
	if out, err := lb.runCmd(lb.ctx, "launchctl", "load", lb.plistPath()); err != nil {
		return fmt.Errorf("launchctl load: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func userHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return os.Getenv("HOME")
}

func launchdPathEnv() string {
	return joinAbsPaths(
		strings.Split(os.Getenv("PATH"), ":"),
		[]string{
			"/opt/homebrew/bin",
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
			"/usr/sbin",
			"/sbin",
		},
	)
}

func launchdOpenclawHome() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("OPENCLAW_HOME")); raw != "" {
		if err := validateAbsPath(raw); err != nil {
			return "", fmt.Errorf("OPENCLAW_HOME: %w", err)
		}
		return raw, nil
	}
	home := userHomeDir()
	if home == "" {
		return "", errors.New("OPENCLAW_HOME unset and home directory unknown")
	}
	if err := validateAbsPath(home); err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".openclaw"), nil
}

func (lb *launchdBackend) Uninstall() error {
	p := lb.plistPath()
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("service not installed (plist not found: %s)", p)
	}
	out, err := lb.runCmd(lb.ctx, "launchctl", "unload", p)
	if err != nil {
		return fmt.Errorf("launchctl unload: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}

func (lb *launchdBackend) Start() error {
	out, err := lb.runCmd(lb.ctx, "launchctl", "start", launchdLabel)
	if err != nil {
		return fmt.Errorf("launchctl start: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (lb *launchdBackend) Stop() error {
	out, err := lb.runCmd(lb.ctx, "launchctl", "stop", launchdLabel)
	if err != nil {
		return fmt.Errorf("launchctl stop: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (lb *launchdBackend) Restart() error {
	// ignore stop error — service may not be running
	_, _ = lb.runCmd(lb.ctx, "launchctl", "stop", launchdLabel)
	return lb.Start()
}

func (lb *launchdBackend) Status() (ServiceStatus, error) {
	st := ServiceStatus{Backend: "LaunchAgent"}

	// AutoStart = plist file exists
	p := lb.plistPath()
	plistContent, err := os.ReadFile(p)
	var logPath string
	if err == nil {
		st.AutoStart = true
		st.Port, logPath = parsePlist(string(plistContent))
	}

	// Running = launchctl list succeeds and contains PID
	out, err := lb.runCmd(lb.ctx, "launchctl", "list", launchdLabel)
	if err != nil {
		// not running or not registered — not an error for Status
		return st, nil
	}
	pid := parseLaunchctlPID(string(out))
	if pid > 0 && st.Port > 0 {
		st.PID = pid
		st.Uptime = resolveUptime(lb.ctx, lb.runCmd, pid)
		if lb.probeFunc(fmt.Sprintf("http://127.0.0.1:%d/", st.Port)) {
			st.Running = true
		}
	}

	// Last 20 log lines
	if st.AutoStart && logPath != "" {
		st.LogLines = tailFile(logPath, 20)
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

// parsePlist parses a launchd plist and extracts the --port value from any
// <array> of <string>s and the StandardOutPath value. On malformed input or
// missing fields it returns zero values; errors are not propagated.
//
// Behavior:
//   - port: the <string> immediately following a <string>--port</string>
//     entry inside an <array>.
//   - logPath: the next <string> appearing after the most recent
//     <key>StandardOutPath</key>.
func parsePlist(content string) (port int, logPath string) {
	dec := xml.NewDecoder(strings.NewReader(content))
	dec.Strict = false

	var (
		curKey   *strings.Builder
		curStr   *strings.Builder
		lastKey  string
		inArray  int
		arrStrs  []string // <string> values seen inside the current <array>
		stdOut   string
		wantNext bool // next <string> should be captured as StandardOutPath
	)

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "array":
				inArray++
				arrStrs = nil
			case "key":
				curKey = &strings.Builder{}
			case "string":
				curStr = &strings.Builder{}
			}
		case xml.CharData:
			if curKey != nil {
				curKey.Write(t)
			}
			if curStr != nil {
				curStr.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "key":
				if curKey != nil {
					lastKey = strings.TrimSpace(curKey.String())
					curKey = nil
				}
			case "string":
				if curStr != nil {
					val := curStr.String()
					if inArray > 0 {
						arrStrs = append(arrStrs, val)
					} else if wantNext {
						stdOut = val
						wantNext = false
					}
					curStr = nil
				}
			case "array":
				if inArray > 0 {
					inArray--
				}
				if inArray == 0 && port == 0 {
					for i, a := range arrStrs {
						if a == "--port" && i+1 < len(arrStrs) {
							port, _ = strconv.Atoi(strings.TrimSpace(arrStrs[i+1]))
							break
						}
					}
				}
			}
			// Arm StandardOutPath capture once we exit the <key> element.
			if t.Name.Local == "key" && lastKey == "StandardOutPath" && stdOut == "" {
				wantNext = true
			}
		}
	}
	return port, strings.TrimSpace(stdOut)
}

// parsePlistPort reads the --port value from the ProgramArguments in a plist.
func parsePlistPort(content string) int {
	port, _ := parsePlist(content)
	return port
}

// parsePlistLogPath reads StandardOutPath from a plist.
func parsePlistLogPath(content string) string {
	_, logPath := parsePlist(content)
	return logPath
}

// resolveUptime fetches the process start time via ps and computes elapsed duration.
func resolveUptime(ctx context.Context, run runCmdFunc, pid int) time.Duration {
	out, err := run(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "lstart=")
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
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.Size() <= 0 {
		return nil
	}

	const maxTailBytes int64 = 256 * 1024
	start := max(info.Size()-maxTailBytes, 0)
	buf := make([]byte, info.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return nil
	}

	lines := slices.Collect(strings.SplitSeq(strings.TrimRight(string(buf), "\n"), "\n"))
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func xmlText(v any) string {
	var raw string
	switch value := v.(type) {
	case string:
		raw = value
	case int:
		raw = strconv.Itoa(value)
	default:
		raw = fmt.Sprint(value)
	}
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(raw)); err != nil {
		return raw
	}
	return b.String()
}
