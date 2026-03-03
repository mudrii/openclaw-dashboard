package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"runtime"
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
func (s *SystemService) GetJSON(ctx context.Context) (int, []byte) {
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
				s.refresh(context.Background())
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
	data := s.refresh(ctx)
	if data == nil {
		return http.StatusServiceUnavailable, []byte(`{"ok":false,"degraded":true,"error":"system metrics unavailable"}`)
	}
	return http.StatusOK, data
}

func (s *SystemService) refresh(ctx context.Context) []byte {
	ver := s.getVersionsCached(ctx)

	resp := SystemResponse{
		OK:          true,
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
		PollSeconds: s.cfg.PollSeconds,
		CPU:         collectCPU(ctx),
		RAM:         collectRAM(ctx),
		Swap:        collectSwap(ctx),
		Disk:        collectDiskRoot(s.cfg.DiskPath),
		Versions:    ver,
	}

	if resp.CPU.Error != nil {
		resp.Degraded = true
		resp.Errors = append(resp.Errors, "cpu: "+*resp.CPU.Error)
	}
	if resp.RAM.Error != nil {
		resp.Degraded = true
		resp.Errors = append(resp.Errors, "ram: "+*resp.RAM.Error)
	}
	if resp.Swap.Error != nil {
		resp.Degraded = true
		resp.Errors = append(resp.Errors, "swap: "+*resp.Swap.Error)
	}
	if resp.Disk.Error != nil {
		resp.Degraded = true
		resp.Errors = append(resp.Errors, "disk: "+*resp.Disk.Error)
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return nil
	}
	s.metricsMu.Lock()
	s.metricsPayload = b
	s.metricsAt = time.Now()
	s.metricsMu.Unlock()
	return b
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
	out, err := runWithTimeout(ctx, timeoutMs, "openclaw", "--version")
	if err != nil {
		v.Openclaw = "unknown"
	} else {
		v.Openclaw = strings.TrimPrefix(strings.TrimSpace(out), "openclaw ")
	}

	// Gateway status
	gwOut, err := runWithTimeout(ctx, timeoutMs, "openclaw", "gateway", "status")
	gw := SystemGateway{Status: "unknown"}
	if err != nil {
		e := "unreachable"
		gw.Status = "offline"
		gw.Error = &e
	} else {
		lower := strings.ToLower(gwOut)
		if strings.Contains(lower, "running") || strings.Contains(lower, "online") {
			gw.Status = "online"
		} else {
			gw.Status = "offline"
		}
		// Try to extract version from output
		for _, line := range strings.Split(gwOut, "\n") {
			if strings.Contains(line, "version") || strings.Contains(line, "v20") {
				for _, p := range strings.Fields(line) {
					p = strings.Trim(p, "()v,")
					if len(p) > 4 && (strings.HasPrefix(p, "20") || strings.HasPrefix(p, "0.")) {
						gw.Version = p
						break
					}
				}
			}
		}
	}
	v.Gateway = gw
	return v
}

// runWithTimeout runs an external command with a context deadline.
func runWithTimeout(ctx context.Context, timeoutMs int, name string, args ...string) (string, error) {
	tctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(tctx, name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// collectHostname returns the system hostname gracefully.
func collectHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// hostOS returns runtime.GOOS.
func hostOS() string { return runtime.GOOS }

// hostArch returns runtime.GOARCH.
func hostArch() string { return runtime.GOARCH }
