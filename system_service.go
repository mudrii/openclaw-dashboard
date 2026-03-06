package main

import (
	"context"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SystemService collects host metrics and versions with TTL caching.
type SystemService struct {
	cfg       SystemConfig
	dashVer   string
	serverCtx context.Context // lifecycle context — cancelled on graceful shutdown

	metricsMu      sync.RWMutex
	metricsPayload []byte
	metricsAt      time.Time
	metricsRefresh bool

	verMu      sync.RWMutex
	verCached  SystemVersions
	verAt      time.Time
	verRefresh bool // true while a goroutine is collecting versions
}

func NewSystemService(cfg SystemConfig, dashVer string, serverCtx context.Context) *SystemService {
	return &SystemService{cfg: cfg, dashVer: dashVer, serverCtx: serverCtx}
}

// GetJSON returns (statusCode, jsonBody).
// Respects system.enabled config — returns 503 when disabled.
func (s *SystemService) GetJSON(ctx context.Context) (int, []byte) {
	if !s.cfg.Enabled {
		return http.StatusServiceUnavailable, []byte(`{"ok":false,"error":"system metrics disabled"}`)
	}

	ttl := time.Duration(s.cfg.MetricsTTLSeconds) * time.Second

	s.metricsMu.RLock()
	if s.metricsPayload != nil && time.Since(s.metricsAt) < ttl {
		b := s.metricsPayload
		s.metricsMu.RUnlock()
		return http.StatusOK, b
	}
	hasStale := s.metricsPayload != nil
	s.metricsMu.RUnlock()

	if hasStale {
		// Return stale immediately, refresh in background
		s.metricsMu.Lock()
		if !s.metricsRefresh {
			s.metricsRefresh = true
			go func() {
				data, hardFail := s.refresh(s.serverCtx)
				if data == nil || hardFail {
					log.Printf("[system] background refresh failed: data=%v hardFail=%v", data == nil, hardFail)
				}
				s.metricsMu.Lock()
				s.metricsRefresh = false
				s.metricsMu.Unlock()
			}()
		}
		b := s.metricsPayload
		s.metricsMu.Unlock()

		// Mark stale in response — byte-level replacement avoids unmarshal/remarshal overhead
		staleBytes := bytes.Replace(b, []byte(`"stale":false`), []byte(`"stale":true`), 1)
		return http.StatusOK, staleBytes
	}

	// No cache — collect synchronously
	data, hardFail := s.refresh(ctx)
	if data == nil {
		return http.StatusServiceUnavailable, []byte(`{"ok":false,"degraded":true,"error":"system metrics unavailable"}`)
	}
	if hardFail {
		return http.StatusServiceUnavailable, data
	}
	return http.StatusOK, data
}

// refresh collects fresh metrics and returns (jsonBytes, isHardFail).
// isHardFail=true when ALL core collectors failed (no useful data).
func (s *SystemService) refresh(ctx context.Context) ([]byte, bool) {
	// Run versions + disk + CPU/RAM/Swap all in parallel for minimum wall-clock time.
	var ver SystemVersions
	var disk SystemDisk
	var cpu SystemCPU
	var ram SystemRAM
	var swap SystemSwap
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); ver = s.getVersionsCached(ctx) }()
	go func() { defer wg.Done(); disk = collectDiskRoot(s.cfg.DiskPath) }()
	go func() {
		defer wg.Done()
		cpu, ram, swap = collectCPURAMSwapParallel(ctx)
	}()
	wg.Wait()

	// Hard fail = all four core collectors failed
	allFailed := cpu.Error != nil && ram.Error != nil && swap.Error != nil && disk.Error != nil

	resp := SystemResponse{
		OK:          !allFailed,
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
		PollSeconds: s.cfg.PollSeconds,
		Thresholds: SystemThresholds{
			CPU:  ThresholdPair{Warn: s.cfg.CPU.Warn, Critical: s.cfg.CPU.Critical},
			RAM:  ThresholdPair{Warn: s.cfg.RAM.Warn, Critical: s.cfg.RAM.Critical},
			Swap: ThresholdPair{Warn: s.cfg.Swap.Warn, Critical: s.cfg.Swap.Critical},
			Disk: ThresholdPair{Warn: s.cfg.Disk.Warn, Critical: s.cfg.Disk.Critical},
		},
		CPU:      cpu,
		RAM:      ram,
		Swap:     swap,
		Disk:     disk,
		Versions: ver,
	}

	if cpu.Error != nil {
		resp.Degraded = true
		resp.Errors = append(resp.Errors, "cpu: "+*cpu.Error)
	}
	if ram.Error != nil {
		resp.Degraded = true
		resp.Errors = append(resp.Errors, "ram: "+*ram.Error)
	}
	if swap.Error != nil {
		resp.Degraded = true
		resp.Errors = append(resp.Errors, "swap: "+*swap.Error)
	}
	if disk.Error != nil {
		resp.Degraded = true
		resp.Errors = append(resp.Errors, "disk: "+*disk.Error)
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return nil, true
	}
	s.metricsMu.Lock()
	s.metricsPayload = b
	s.metricsAt = time.Now()
	s.metricsMu.Unlock()
	return b, allFailed
}

func (s *SystemService) getVersionsCached(ctx context.Context) SystemVersions {
	ttl := time.Duration(s.cfg.VersionsTTLSeconds) * time.Second
	s.verMu.RLock()
	if s.verAt != (time.Time{}) && time.Since(s.verAt) < ttl {
		v := s.verCached
		s.verMu.RUnlock()
		return v
	}
	s.verMu.RUnlock()

	// Double-checked lock with refresh flag to prevent thundering herd
	s.verMu.Lock()
	// Re-check after acquiring write lock (another goroutine may have refreshed)
	if s.verAt != (time.Time{}) && time.Since(s.verAt) < ttl {
		v := s.verCached
		s.verMu.Unlock()
		return v
	}
	// If another goroutine is already refreshing, return stale
	if s.verRefresh {
		v := s.verCached
		s.verMu.Unlock()
		return v
	}
	s.verRefresh = true
	s.verMu.Unlock()

	v := collectVersions(ctx, s.dashVer, s.cfg.GatewayTimeoutMs, s.cfg.GatewayPort)
	s.verMu.Lock()
	s.verCached = v
	s.verAt = time.Now()
	s.verRefresh = false
	s.verMu.Unlock()
	return v
}

// collectDiskRoot uses syscall.Statfs — works on both darwin and linux.
func collectDiskRoot(path string) SystemDisk {
	d := SystemDisk{Path: path}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		e := fmt.Sprintf("statfs %s: %v", path, err)
		d.Error = &e
		return d
	}
	d.TotalBytes = int64(stat.Blocks) * int64(stat.Bsize)
	free := int64(stat.Bavail) * int64(stat.Bsize)
	d.UsedBytes = d.TotalBytes - free
	if d.TotalBytes > 0 {
		d.Percent = math.Round(float64(d.UsedBytes)/float64(d.TotalBytes)*1000) / 10
	}
	return d
}

// collectVersions probes openclaw + gateway CLIs.
func collectVersions(ctx context.Context, dashVer string, timeoutMs int, gatewayPort int) SystemVersions {
	v := SystemVersions{Dashboard: dashVer}

	// OpenClaw version
	// Use full path — asdf shims may not be in the server's PATH
	oclawBin := resolveOpenclawBin()
	out, err := runWithTimeout(ctx, timeoutMs, oclawBin, "--version")
	if err != nil {
		v.Openclaw = "unknown"
	} else {
		v.Openclaw = strings.TrimPrefix(strings.TrimSpace(out), "openclaw ")
	}

	// Gateway status — use --json flag for reliable parsing
	gw := SystemGateway{Status: "unknown"}
	gwOut, err := runWithTimeout(ctx, timeoutMs, oclawBin, "gateway", "status", "--json")
	if err != nil {
		// Fallback: check if gateway process is reachable via HTTP
		gw = detectGatewayFallback(ctx, gatewayPort, timeoutMs)
	} else {
		gw = parseGatewayStatusJSON(ctx, gwOut)
	}
	v.Gateway = gw

	// Latest version from npm registry (best-effort, non-blocking)
	v.Latest = fetchLatestNpmVersion(ctx, timeoutMs)

	return v
}

// parseGatewayStatusJSON parses `openclaw gateway status --json` output.
// The JSON has shape: {"service":{"loaded":true,"runtime":{...}},...}
func parseGatewayStatusJSON(ctx context.Context, output string) SystemGateway {
	var result struct {
		Service struct {
			Loaded  bool `json:"loaded"`
			Runtime struct {
				Status string `json:"status"`
				PID    int    `json:"pid"`
			} `json:"runtime"`
		} `json:"service"`
		Version string `json:"version"`
	}
	// Find first JSON object in output (may have leading non-JSON lines)
	start := strings.Index(output, "{")
	if start >= 0 {
		if err := json.Unmarshal([]byte(output[start:]), &result); err == nil {
			// Prefer runtime.Status == "running" over just Loaded
			status := "offline"
			if result.Service.Runtime.Status == "running" || result.Service.Loaded {
				status = "online"
			}
			gw := SystemGateway{Version: result.Version, Status: status, PID: result.Service.Runtime.PID}
			// Get uptime + memory from /proc or ps if we have a PID
			if gw.PID > 0 {
				gw.Uptime, gw.Memory = getProcessInfo(ctx, gw.PID)
			}
			return gw
		}
	}
	// Fallback: text parsing
	lower := strings.ToLower(output)
	if strings.Contains(lower, "loaded") || strings.Contains(lower, "running") {
		return SystemGateway{Status: "online"}
	}
	return SystemGateway{Status: "offline"}
}

// formatBytes formats bytes into a human-readable string (KB/MB/GB).
func formatBytes(b int64) string {
	if b < 0 {
		return "0B"
	}
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1fGB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.0fKB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// getProcessInfo returns uptime and memory usage for a PID using ps.
// Uses a 3-second context timeout to avoid hanging on unresponsive ps.
func getProcessInfo(ctx context.Context, pid int) (uptime string, memory string) {
	tctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// Get elapsed time and RSS via ps
	out, err := exec.CommandContext(tctx, "ps", "-o", "etime=,rss=", "-p", fmt.Sprintf("%d", pid)).Output()
	if err != nil {
		return "", ""
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) >= 1 {
		uptime = strings.TrimSpace(fields[0])
	}
	if len(fields) >= 2 {
		if rssKB, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			memory = formatBytes(rssKB * 1024)
		}
	}
	return
}

// detectGatewayFallback checks if the gateway HTTP port is responding.
// timeoutMs controls how long to wait; defaults to 1500ms if <= 0.
func detectGatewayFallback(ctx context.Context, gatewayPort int, timeoutMs int) SystemGateway {
	if gatewayPort <= 0 {
		gatewayPort = 18789
	}
	if timeoutMs <= 0 {
		timeoutMs = 1500
	}
	tctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(tctx, http.MethodHead, fmt.Sprintf("http://127.0.0.1:%d/", gatewayPort), nil)
	if err != nil {
		e := "probe failed"
		return SystemGateway{Status: "offline", Error: &e}
	}
	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		return SystemGateway{Status: "online"}
	}
	e := "unreachable"
	return SystemGateway{Status: "offline", Error: &e}
}

// runWithTimeout runs an external command with a context deadline.
// On failure, stderr is appended to the error message for better diagnostics.
func runWithTimeout(ctx context.Context, timeoutMs int, name string, args ...string) (string, error) {
	tctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(tctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return strings.TrimSpace(string(out)), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return strings.TrimSpace(string(out)), err
	}
	return strings.TrimSpace(string(out)), nil
}

// resolveOpenclawBin finds the openclaw binary, checking PATH then known asdf locations.
// asdf shims may not be on the server's PATH when launched as a background process.
func resolveOpenclawBin() string {
	if p, err := exec.LookPath("openclaw"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".asdf", "shims", "openclaw"),
	}
	// Also probe asdf nodejs installs — sort newest-first so the highest version is tried first
	if nodeDir := filepath.Join(home, ".asdf", "installs", "nodejs"); nodeDir != "" {
		if entries, err := os.ReadDir(nodeDir); err == nil {
			// Sort by name descending (lexicographic reverse = newest version first)
			sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
			for _, e := range entries {
				if e.IsDir() {
					candidates = append(candidates, filepath.Join(nodeDir, e.Name(), "bin", "openclaw"))
				}
			}
		}
	}
	candidates = append(candidates,
		"/usr/local/bin/openclaw",
		"/opt/homebrew/bin/openclaw",
	)
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return c
		}
	}
	return "openclaw" // last resort — may fail but gives a clear error
}

// fetchLatestNpmVersion queries the npm registry for the latest openclaw version.
// Best-effort: returns "" on any error.
func fetchLatestNpmVersion(ctx context.Context, timeoutMs int) string {
	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://registry.npmjs.org/openclaw/latest", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&pkg); err != nil {
		return ""
	}
	return pkg.Version
}


