package appservice

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ErrUnsupported is returned on platforms without a service backend.
var ErrUnsupported = errors.New("service management not supported on this platform")

// validateAbsPath returns nil iff p is a non-empty absolute filesystem path.
// Used to keep relative or empty values out of generated unit/plist files,
// where they would either fail at service-start with confusing errors or
// (worse) resolve against the daemon's cwd.
func validateAbsPath(p string) error {
	if p == "" {
		return errors.New("path is empty")
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("path %q is not absolute", p)
	}
	return nil
}

// joinAbsPaths returns a colon-joined PATH-style string built from the given
// entry groups. Empty, whitespace-only, and non-absolute entries are dropped;
// duplicates are kept once in first-seen order. Use to sanitize PATH derived
// from os.Getenv("PATH") before embedding into a service unit file.
func joinAbsPaths(groups ...[]string) string {
	seen := make(map[string]struct{})
	var paths []string
	for _, g := range groups {
		for _, entry := range g {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			if !filepath.IsAbs(entry) {
				continue
			}
			if _, ok := seen[entry]; ok {
				continue
			}
			seen[entry] = struct{}{}
			paths = append(paths, entry)
		}
	}
	return strings.Join(paths, ":")
}

// runCmdFunc is the signature for running an external command.
// Injected into backends so tests can intercept exec calls.
type runCmdFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

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

// Installer manages service registration with the OS init system.
type Installer interface {
	Install(cfg InstallConfig) error
	Uninstall() error
}

// Controller manages the running state of a registered service.
type Controller interface {
	Start() error
	Stop() error
	Restart() error
}

// Statuser reports the current state of a service.
type Statuser interface {
	Status() (ServiceStatus, error)
}

// Backend is the full service management interface, composed from focused sub-interfaces.
// Each platform provides one implementation via build tags.
type Backend interface {
	Installer
	Controller
	Statuser
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
	if d <= 0 {
		return "0s"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
