package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SystemService collects host metrics and versions with TTL caching.
type SystemService struct {
	cfg     SystemConfig
	dashVer string

	metricsMu      sync.RWMutex
	metricsPayload []byte
	metricsAt      time.Time
	metricsRefresh bool

	verMu     sync.RWMutex
	verCached SystemVersions
	verAt     time.Time
}

func NewSystemService(cfg SystemConfig, dashVer string) *SystemService {
	return &SystemService{cfg: cfg, dashVer: dashVer}
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
				s.refresh(context.Background()) //nolint:errcheck
				s.metricsMu.Lock()
				s.metricsRefresh = false
				s.metricsMu.Unlock()
			}()
		}
		b := s.metricsPayload
		s.metricsMu.Unlock()

		// Mark stale in response
		var resp SystemResponse
		if err := json.Unmarshal(b, &resp); err == nil {
			resp.Stale = true
			if out, err := json.Marshal(resp); err == nil {
				return http.StatusOK, out
			}
		}
		return http.StatusOK, b
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

	v := collectVersions(ctx, s.dashVer, s.cfg.GatewayTimeoutMs)
	s.verMu.Lock()
	s.verCached = v
	s.verAt = time.Now()
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
func collectVersions(ctx context.Context, dashVer string, timeoutMs int) SystemVersions {
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
		gw = detectGatewayFallback(ctx)
	} else {
		gw = parseGatewayStatusJSON(gwOut)
	}
	v.Gateway = gw
	return v
}

// parseGatewayStatusJSON parses `openclaw gateway status --json` output.
// The JSON has shape: {"service":{"loaded":true,...},...}
func parseGatewayStatusJSON(output string) SystemGateway {
	var result struct {
		Service struct {
			Loaded bool `json:"loaded"`
		} `json:"service"`
		Version string `json:"version"`
	}
	// Find first JSON object in output (may have leading non-JSON lines)
	start := strings.Index(output, "{")
	if start >= 0 {
		if err := json.Unmarshal([]byte(output[start:]), &result); err == nil {
			status := "offline"
			if result.Service.Loaded {
				status = "online"
			}
			return SystemGateway{Version: result.Version, Status: status}
		}
	}
	// Fallback: text parsing
	lower := strings.ToLower(output)
	if strings.Contains(lower, "loaded") || strings.Contains(lower, "running") {
		return SystemGateway{Status: "online"}
	}
	return SystemGateway{Status: "offline"}
}

// detectGatewayFallback checks if the gateway HTTP port is responding.
func detectGatewayFallback(ctx context.Context) SystemGateway {
	tctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(tctx, http.MethodHead, "http://127.0.0.1:18789/", nil)
	if err != nil {
		e := "probe failed"
		return SystemGateway{Status: "offline", Error: &e}
	}
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
		return SystemGateway{Status: "online"}
	}
	e := "unreachable"
	return SystemGateway{Status: "offline", Error: &e}
}

// runWithTimeout runs an external command with a context deadline.
func runWithTimeout(ctx context.Context, timeoutMs int, name string, args ...string) (string, error) {
	tctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(tctx, name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// resolveOpenclawBin finds the openclaw binary, checking PATH then known asdf locations.
// asdf shims may not be on the server's PATH when launched as a background process.
func resolveOpenclawBin() string {
	if p, err := exec.LookPath("openclaw"); err == nil {
		return p
	}
	// asdf node installs — check all installed node versions
	candidates := []string{
		"/Users/mudrii/.asdf/installs/nodejs/22.22.0/bin/openclaw",
		"/Users/mudrii/.asdf/shims/openclaw",
		"/usr/local/bin/openclaw",
		"/opt/homebrew/bin/openclaw",
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c); err == nil {
			return c
		}
	}
	return "openclaw" // last resort — may fail but gives a clear error
}


