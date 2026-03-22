package dashboard

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

//go:embed web/index.html
var indexHTML []byte

// BuildVersion is set at link time.
var BuildVersion string

// Main runs the dashboard CLI and returns a process exit code.
func Main() int {
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
		version = detectVersion(dir)
	}
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
			log.Printf("[dashboard] WARNING: invalid DASHBOARD_PORT %q, using default %d", envPort, envPortInt)
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
		openclawPath := os.Getenv("OPENCLAW_HOME")
		if openclawPath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				fmt.Fprintf(os.Stderr, "[dashboard] WARNING: UserHomeDir failed: %v\n", err)
			}
			openclawPath = filepath.Join(home, ".openclaw")
		}
		if _, err := os.Stat(openclawPath); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "OpenClaw not found at %s\n", openclawPath)
			return 1
		}
		fmt.Printf("Dashboard dir: %s\n", dir)
		fmt.Printf("OpenClaw path: %s\n", openclawPath)
		if err := refreshCollectorFunc(dir, openclawPath, cfg); err != nil {
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
		fmt.Println("[dashboard] WARNING: ai.enabled=true but OPENCLAW_GATEWAY_TOKEN not found in dotenv")
	}

	// Server lifecycle context — cancelled on SIGINT/SIGTERM for clean goroutine shutdown
	serverCtx, serverCancel := context.WithCancel(context.Background())
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

	// Graceful shutdown on SIGINT/SIGTERM
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)
	serverErr := make(chan error, 1)

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	select {
	case <-stop:
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
