package dashboard

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appruntime"
	"github.com/mudrii/openclaw-dashboard/internal/appservice"
)

//go:embed web/index.html
var indexHTML []byte

// BuildVersion is set at link time.
var BuildVersion string

// Main runs the dashboard CLI and returns a process exit code.
func Main() int {
	cmdCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	// Resolve binary directory (follows symlinks)
	exe, err := os.Executable()
	if err != nil {
		exe = "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err != nil {
		fmt.Fprintf(os.Stderr, "[dashboard] WARNING: EvalSymlinks failed: %v\n", err)
	} else {
		exe = resolved
	}
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
				version = detectVersion(cmdCtx, dir)
			}
			version = strings.TrimPrefix(version, "v")
			cfg := loadConfig(dir)

			// env var overrides
			envBind := os.Getenv("DASHBOARD_BIND")
			if envBind == "" {
				envBind = cfg.Server.Host
			}
			envPort := cfg.Server.Port
			if p := os.Getenv("DASHBOARD_PORT"); p != "" {
				if n, err := strconv.Atoi(p); err == nil {
					envPort = n
				} else {
					slog.Warn("[dashboard] invalid DASHBOARD_PORT, using default", "value", p, "default", envPort)
				}
			}

			b, err := appservice.NewWithContext(cmdCtx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[dashboard] service management not available: %v\n", err)
				return 1
			}
			return runServiceCmd(subcmd, serviceCmdOpts{
				dir:         dir,
				binPath:     exe,
				version:     version,
				backend:     b,
				args:        rest,
				defaultBind: envBind,
				defaultPort: envPort,
			})
		default:
			fmt.Fprintf(os.Stderr, "[dashboard] unknown command %q\n", subcmd)
			fmt.Fprintln(os.Stderr, "Usage: openclaw-dashboard [service] install|uninstall|start|stop|restart|status")
			return 1
		}
	} else if len(os.Args) > 1 && os.Args[1] == "service" {
		fmt.Fprintln(os.Stderr, "Usage: openclaw-dashboard service install|uninstall|start|stop|restart|status")
		return 1
	}

	// Resolve the dashboard runtime directory. Source checkouts use the repo root,
	// release archives use the extracted folder, and Homebrew installs hydrate a
	// writable runtime directory under ~/.openclaw/dashboard.
	dir, err := resolveDashboardDirWithError(binDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[dashboard] failed to resolve runtime directory: %v\n", err)
		return 1
	}

	version := BuildVersion
	if version == "" {
		version = detectVersion(cmdCtx, dir)
	}
	version = strings.TrimPrefix(version, "v")
	cfg := loadConfig(dir)

	// Env var defaults
	envBind := os.Getenv("DASHBOARD_BIND")
	if envBind == "" {
		envBind = cfg.Server.Host
	}
	envPort := os.Getenv("DASHBOARD_PORT")
	envPortInt := cfg.Server.Port

	if envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			envPortInt = p
		} else {
			slog.Warn("[dashboard] invalid DASHBOARD_PORT, using default", "value", envPort, "default", envPortInt)
		}
	}

	// CLI flags
	bind := flag.String("bind", envBind, "Bind address (use 0.0.0.0 for LAN)")
	flag.StringVar(bind, "b", envBind, "Bind address (shorthand)")
	port := flag.Int("port", envPortInt, "Listen port")
	flag.IntVar(port, "p", envPortInt, "Listen port (shorthand)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.BoolVar(showVersion, "V", false, "Print version (shorthand)")
	doRefresh := flag.Bool("refresh", false, "Generate data.json and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("openclaw-dashboard %s\n", version)
		return 0
	}

	if *doRefresh {
		openclawPath := appruntime.ResolveOpenclawPath()
		if _, err := os.Stat(openclawPath); errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "OpenClaw not found at %s\n", openclawPath)
			return 1
		}
		fmt.Printf("Dashboard dir: %s\n", dir)
		fmt.Printf("OpenClaw path: %s\n", openclawPath)
		if err := refreshCollectorFunc(cmdCtx, dir, openclawPath, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "refresh failed: %v\n", err)
			return 1
		}
		fmt.Printf("✅ data.json refreshed at %s\n", time.Now().Format("2006-01-02 15:04:05"))
		return 0
	}

	// Load gateway token from .env
	env := readDotenv(cfg.AI.DotenvPath)
	gatewayToken := env["OPENCLAW_GATEWAY_TOKEN"]
	if cfg.AI.Enabled && gatewayToken == "" {
		slog.Warn("[dashboard] ai.enabled=true but OPENCLAW_GATEWAY_TOKEN not found in dotenv")
	}

	// Server lifecycle context — follows the top-level CLI lifecycle.
	serverCtx, serverCancel := context.WithCancel(cmdCtx)
	defer serverCancel()

	srv := NewServer(dir, version, cfg, gatewayToken, indexHTML, serverCtx)

	// Pre-warm data.json in background so first browser hit is instant
	srv.PreWarm()

	addr := fmt.Sprintf("%s:%d", *bind, *port)
	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      srv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 90 * time.Second, // chat streaming can be slow
		IdleTimeout:  120 * time.Second,
	}

	fmt.Printf("[dashboard] v%s\n", version)
	fmt.Printf("[dashboard] Serving on http://%s/\n", addr)
	fmt.Printf("[dashboard] Refresh endpoint: /api/refresh (debounce: %ds)\n", cfg.Refresh.IntervalSeconds)
	if cfg.AI.Enabled {
		fmt.Printf("[dashboard] AI chat: /api/chat (gateway: localhost:%d, model: %s)\n",
			cfg.AI.GatewayPort, cfg.AI.Model)
	}
	if *bind == "0.0.0.0" {
		if ip := localIP(); ip != "" {
			fmt.Printf("[dashboard] LAN access: http://%s:%d/\n", ip, *port)
		}
	}

	serverErr := make(chan error, 1)

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	select {
	case <-cmdCtx.Done():
	case err := <-serverErr:
		serverCancel()
		fmt.Fprintf(os.Stderr, "[dashboard] fatal: %v\n", err)
		return 1
	}

	serverCancel() // cancel background goroutines (metrics refresh, etc.)
	fmt.Println("\n[dashboard] shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[dashboard] shutdown error: %v\n", err)
	}
	fmt.Println("[dashboard] stopped")
	return 0
}

// normaliseCmd extracts the service subcommand from args.
// Returns ("", nil) for empty input or when args[0] starts with "-" (a flag).
// "service start" → ("start", rest)
// "start"         → ("start", rest)
// "service"       → ("", nil)  — caller prints usage
func normaliseCmd(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	if strings.HasPrefix(args[0], "-") {
		return "", nil
	}
	if args[0] == "service" {
		if len(args) < 2 {
			return "", nil
		}
		cmd := args[1]
		rest := args[2:]
		if len(rest) == 0 {
			rest = nil
		}
		return cmd, rest
	}
	cmd := args[0]
	rest := args[1:]
	if len(rest) == 0 {
		rest = nil
	}
	return cmd, rest
}

// serviceCmdOpts holds parameters for runServiceCmd.
type serviceCmdOpts struct {
	dir         string
	binPath     string
	version     string
	backend     appservice.Backend
	args        []string
	defaultBind string
	defaultPort int
}

// runServiceCmd executes a service lifecycle subcommand using the given opts.
func runServiceCmd(cmd string, opts serviceCmdOpts) int {
	fs := flag.NewFlagSet("service", flag.ContinueOnError)
	bind := fs.String("bind", opts.defaultBind, "Bind address")
	fs.StringVar(bind, "b", opts.defaultBind, "Bind address")
	port := fs.Int("port", opts.defaultPort, "Listen port")
	fs.IntVar(port, "p", opts.defaultPort, "Listen port")
	if err := fs.Parse(opts.args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "[dashboard] unexpected arguments for %s: %s\n", cmd, strings.Join(fs.Args(), " "))
		return 1
	}

	switch cmd {
	case "install":
		cfg := appservice.InstallConfig{
			BinPath: opts.binPath,
			WorkDir: opts.dir,
			LogPath: filepath.Join(opts.dir, "server.log"),
			Host:    *bind,
			Port:    *port,
		}
		if err := opts.backend.Install(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "[dashboard] install failed: %v\n", err)
			return 1
		}
		fmt.Println("[dashboard] service installed and started")
		return 0
	case "uninstall":
		if err := opts.backend.Uninstall(); err != nil {
			fmt.Fprintf(os.Stderr, "[dashboard] uninstall failed: %v\n", err)
			return 1
		}
		fmt.Println("[dashboard] service stopped and unregistered (config and data preserved)")
		return 0
	case "start":
		if err := opts.backend.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[dashboard] start failed: %v\n", err)
			return 1
		}
		fmt.Println("[dashboard] service started")
		return 0
	case "stop":
		if err := opts.backend.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "[dashboard] stop failed: %v\n", err)
			return 1
		}
		fmt.Println("[dashboard] service stopped")
		return 0
	case "restart":
		if err := opts.backend.Restart(); err != nil {
			fmt.Fprintf(os.Stderr, "[dashboard] restart failed: %v\n", err)
			return 1
		}
		fmt.Println("[dashboard] service restarted")
		return 0
	case "status":
		st, err := opts.backend.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[dashboard] status failed: %v\n", err)
			return 1
		}
		fmt.Print(appservice.FormatStatus(opts.version, st))
		return 0
	default:
		fmt.Fprintf(os.Stderr, "[dashboard] unknown service command %q\n", cmd)
		fmt.Fprintln(os.Stderr, "Usage: openclaw-dashboard [service] install|uninstall|start|stop|restart|status")
		return 1
	}
}

func localIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return ""
}
