package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

//go:embed index.html
var indexHTML []byte

func main() {
	// Resolve binary directory (follows symlinks)
	exe, err := os.Executable()
	if err != nil {
		exe = "."
	}
	exe, _ = filepath.EvalSymlinks(exe)
	dir := filepath.Dir(exe)

	version := detectVersion(dir)
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
		}
	}

	// CLI flags
	bind := flag.String("bind", envBind, "Bind address (use 0.0.0.0 for LAN)")
	flag.StringVar(bind, "b", envBind, "Bind address (shorthand)")
	port := flag.Int("port", envPortInt, "Listen port")
	flag.IntVar(port, "p", envPortInt, "Listen port (shorthand)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.BoolVar(showVersion, "V", false, "Print version (shorthand)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("openclaw-dashboard %s\n", version)
		os.Exit(0)
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

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "[dashboard] fatal: %v\n", err)
			os.Exit(1)
		}
	}()

	<-stop
	serverCancel() // cancel background goroutines (metrics refresh, etc.)
	fmt.Println("\n[dashboard] shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[dashboard] shutdown error: %v\n", err)
	}
	fmt.Println("[dashboard] stopped")
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
