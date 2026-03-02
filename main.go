package main

import (
	_ "embed"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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

	srv := NewServer(dir, version, cfg, gatewayToken, indexHTML)

	addr := fmt.Sprintf("%s:%d", *bind, *port)
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: srv,
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

	if err := httpSrv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "[dashboard] fatal: %v\n", err)
		os.Exit(1)
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
