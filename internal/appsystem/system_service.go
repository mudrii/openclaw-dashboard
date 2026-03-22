package appsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// SystemService collects host metrics and versions with TTL caching.
type SystemService struct {
	cfg       appconfig.SystemConfig
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

	latestMu      sync.RWMutex
	latestVer     string
	latestAt      time.Time
	latestRefresh bool

	binOnce sync.Once
	binPath string
}

var fetchLatestVersion = FetchLatestNpmVersion

func NewSystemService(cfg appconfig.SystemConfig, dashVer string, serverCtx context.Context) *SystemService {
	return &SystemService{cfg: cfg, dashVer: dashVer, serverCtx: serverCtx}
}

func (s *SystemService) SetMetricsTimestampForTest(ts time.Time) {
	s.metricsMu.Lock()
	s.metricsAt = ts
	s.metricsMu.Unlock()
}

func (s *SystemService) openclawBin() string {
	s.binOnce.Do(func() { s.binPath = ResolveOpenclawBin() })
	return s.binPath
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

		// Mark stale in response — use JSON round-trip for safety instead of fragile byte
		// replacement. Byte-level replace silently fails if JSON ordering/spacing differs (B2 fix).
		var sr SystemResponse
		if err := json.Unmarshal(b, &sr); err == nil {
			sr.Stale = true
			if sb, err := json.Marshal(sr); err == nil {
				return http.StatusOK, sb
			}
		}
		// Fallback: serve original payload (stale field inaccurate but data still useful)
		log.Printf("[system] stale injection: could not round-trip JSON, serving original")
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
	// Collect versions first (heavily cached — 300s TTL, effectively free on hot path).
	// This guarantees collectOpenclawRuntime receives real version data instead of an
	// empty SystemVersions{}, eliminating the fragile post-hoc patching that previously
	// existed (B1 fix).
	ver := s.getVersionsCached(ctx)
	if latest := s.getLatestVersionCached(); latest != "" {
		ver.Latest = latest
	}

	// Run OpenClaw runtime + disk + CPU/RAM/Swap in parallel for minimum wall-clock time.
	var openclaw SystemOpenclaw
	var disk SystemDisk
	var cpu SystemCPU
	var ram SystemRAM
	var swap SystemSwap
	oclawBin := s.openclawBin()
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		openclaw = CollectOpenclawRuntime(ctx, oclawBin, s.cfg.GatewayTimeoutMs, s.cfg.GatewayPort, ver)
	}()
	go func() { defer wg.Done(); disk = CollectDiskRoot(s.cfg.DiskPath) }()
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
		Openclaw: openclaw,
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
	if len(openclaw.Errors) > 0 {
		resp.Degraded = true
		for _, e := range openclaw.Errors {
			resp.Errors = append(resp.Errors, "openclaw: "+e)
		}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("[dashboard] collectMetrics: json.Marshal failed: %v", err)
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

	v := CollectVersionsLocal(ctx, s.dashVer, s.cfg.GatewayTimeoutMs, s.cfg.GatewayPort, s.openclawBin())
	s.verMu.Lock()
	s.verCached = v
	s.verAt = time.Now()
	s.verRefresh = false
	s.verMu.Unlock()
	return v
}

func (s *SystemService) getLatestVersionCached() string {
	ttl := time.Duration(s.cfg.VersionsTTLSeconds) * time.Second
	s.latestMu.RLock()
	if s.latestAt != (time.Time{}) && time.Since(s.latestAt) < ttl {
		v := s.latestVer
		s.latestMu.RUnlock()
		return v
	}
	cached := s.latestVer
	s.latestMu.RUnlock()

	s.latestMu.Lock()
	if s.latestAt != (time.Time{}) && time.Since(s.latestAt) < ttl {
		v := s.latestVer
		s.latestMu.Unlock()
		return v
	}
	if s.latestRefresh {
		v := s.latestVer
		s.latestMu.Unlock()
		return v
	}
	s.latestRefresh = true
	s.latestMu.Unlock()

	go func() {
		latest := fetchLatestVersion(s.serverCtx, s.cfg.GatewayTimeoutMs)
		now := time.Now()
		s.latestMu.Lock()
		if latest != "" {
			s.latestVer = latest
		}
		s.latestAt = now
		s.latestRefresh = false
		s.latestMu.Unlock()
	}()

	return cached
}

// collectDiskRoot uses syscall.Statfs — works on both darwin and linux.
func CollectDiskRoot(path string) SystemDisk {
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

// collectVersionsLocal probes openclaw + gateway CLIs without performing any
// outbound network request. Latest-version lookup is handled asynchronously.
func CollectVersionsLocal(ctx context.Context, dashVer string, timeoutMs int, gatewayPort int, oclawBin string) SystemVersions {
	v := SystemVersions{Dashboard: dashVer}

	// OpenClaw version
	out, err := runWithTimeout(ctx, timeoutMs, oclawBin, "--version")
	if err != nil {
		v.Openclaw = "unknown"
	} else {
		v.Openclaw = strings.TrimPrefix(strings.TrimSpace(out), "openclaw ")
	}

	// Gateway status — use --json flag for reliable parsing.
	// I2 fix: attempt to parse stdout even on non-zero exit — many CLIs emit valid JSON
	// to stdout while exiting non-zero (e.g., gateway offline but status successfully queried).
	gw := SystemGateway{Status: "unknown"}
	gwOut, err := runWithTimeout(ctx, timeoutMs, oclawBin, "gateway", "status", "--json")
	if gwOut != "" {
		gw = ParseGatewayStatusJSON(ctx, gwOut)
	}
	if err != nil && gw.Status == "unknown" {
		// stdout had no usable JSON — fall back to HTTP probe
		gw = DetectGatewayFallback(ctx, gatewayPort, timeoutMs)
	}
	v.Gateway = gw

	return v
}

func CollectOpenclawRuntime(ctx context.Context, oclawBin string, timeoutMs int, gatewayPort int, versions SystemVersions) SystemOpenclaw {
	openclaw := SystemOpenclaw{
		Gateway: SystemOpenclawGateway{},
		Status: SystemOpenclawStatus{
			CurrentVersion: versions.Openclaw,
			LatestVersion:  versions.Latest,
		},
		Freshness: SystemOpenclawFreshness{},
	}
	stamp := func() string { return time.Now().UTC().Format(time.RFC3339) }

	var wg sync.WaitGroup
	var gw SystemOpenclawGateway
	var gwErrs []string
	var gwFresh string
	var status SystemOpenclawStatus
	var statusErr error
	var statusFresh string

	wg.Add(2)
	go func() {
		defer wg.Done()
		gw, gwErrs = probeOpenclawGatewayEndpoints(ctx, gatewayPort, timeoutMs)
		if len(gwErrs) == 0 {
			gwFresh = stamp()
		}
	}()
	go func() {
		defer wg.Done()
		out, err := runWithTimeout(ctx, timeoutMs, oclawBin, "status", "--json")
		// I2 fix: attempt to parse stdout even on non-zero exit — CLIs often emit valid JSON while
		// subprocess stdout is parsed regardless of returncode. Many CLIs emit valid JSON to
		// stdout while exiting non-zero (e.g., status reported but gateway connect failed).
		if out != "" {
			if parsed, parseErr := parseOpenclawStatusJSON(out, versions); parseErr == nil {
				status = parsed
				statusFresh = stamp()
			}
		}
		if err != nil {
			statusErr = fmt.Errorf("status --json: %w", err)
		}
	}()
	wg.Wait()

	openclaw.Gateway = gw
	if len(gwErrs) > 0 {
		openclaw.Errors = append(openclaw.Errors, gwErrs...)
	}
	if statusErr != nil {
		openclaw.Errors = append(openclaw.Errors, statusErr.Error())
	}
	// I2 fix: apply parsed status data regardless of error — stdout may have useful
	// data even on non-zero exit. statusFresh is non-empty only when parse succeeded.
	if statusFresh != "" {
		openclaw.Status = status
	}
	openclaw.Freshness = SystemOpenclawFreshness{
		Gateway: gwFresh,
		Status:  statusFresh,
	}

	if openclaw.Status.CurrentVersion == "" {
		openclaw.Status.CurrentVersion = versions.Openclaw
	}
	if openclaw.Status.LatestVersion == "" {
		openclaw.Status.LatestVersion = versions.Latest
	}

	return openclaw
}

func probeOpenclawGatewayEndpoints(ctx context.Context, gatewayPort int, timeoutMs int) (SystemOpenclawGateway, []string) {
	if gatewayPort <= 0 {
		gatewayPort = 18789
	}
	if timeoutMs <= 0 {
		timeoutMs = 1500
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", gatewayPort)
	timeout := time.Duration(timeoutMs) * time.Millisecond
	client := &http.Client{Timeout: timeout}
	gw := SystemOpenclawGateway{}
	var errs []string

	if m, err := FetchJSONMap(ctx, client, base+"/healthz"); err != nil {
		errs = append(errs, "gateway /healthz: "+err.Error())
	} else {
		gw.HealthEndpointOk = true
		if ok, okSet := BoolFromAny(m["ok"]); okSet {
			gw.Live = ok
		}
		if s, ok := m["status"].(string); ok && strings.EqualFold(s, "live") {
			gw.Live = true
		}
	}

	// readyz returns 503 when not ready — but the body still contains useful JSON
	// (ready, failing, uptimeMs). Parse it on both 200 and 503.
	if m, err := fetchJSONMapAllowStatus(ctx, client, base+"/readyz", 200, 503); err != nil {
		errs = append(errs, "gateway /readyz: "+err.Error())
	} else {
		gw.ReadyEndpointOk = true
		if ready, ok := BoolFromAny(m["ready"]); ok {
			gw.Ready = ready
		}
		if uptime, ok := int64FromAny(m["uptimeMs"]); ok {
			gw.UptimeMs = uptime
		}
		gw.Failing = stringSliceFromAny(m["failing"])
	}

	return gw, errs
}

// fetchJSONMapAllowStatus is like fetchJSONMap but accepts specific HTTP status
// codes as valid (e.g., readyz returns 503 with a useful JSON body).
func fetchJSONMapAllowStatus(ctx context.Context, client *http.Client, url string, allowedStatuses ...int) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	allowed := false
	for _, s := range allowedStatuses {
		if resp.StatusCode == s {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func FetchJSONMap(ctx context.Context, client *http.Client, url string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	// Reject any non-2xx status — both 4xx (client error) and 5xx (server error)
	// indicate the endpoint did not return a valid JSON payload we should trust. (I1 fix)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func parseOpenclawStatusJSON(output string, versions SystemVersions) (SystemOpenclawStatus, error) {
	status := SystemOpenclawStatus{CurrentVersion: versions.Openclaw, LatestVersion: versions.Latest}
	var raw map[string]any
	if err := decodeJSONObjectFromOutput(output, &raw); err != nil {
		return status, err
	}
	if current, ok := raw["currentVersion"].(string); ok && current != "" {
		status.CurrentVersion = current
	}
	if current, ok := raw["version"].(string); ok && current != "" && status.CurrentVersion == "" {
		status.CurrentVersion = current
	}
	if latest, ok := raw["latestVersion"].(string); ok && latest != "" {
		status.LatestVersion = latest
	}
	if ms, ok := int64FromAny(raw["connectLatencyMs"]); ok {
		status.ConnectLatencyMs = ms
	}
	if sec, ok := raw["security"].(map[string]any); ok {
		status.Security = sec
	}
	return status, nil
}

func decodeJSONObjectFromOutput(output string, v any) error {
	start := strings.Index(output, "{")
	if start < 0 {
		return fmt.Errorf("json object not found")
	}
	return json.Unmarshal([]byte(output[start:]), v)
}

func BoolFromAny(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

func int64FromAny(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		return int64(x), true
	case json.Number:
		i, err := x.Int64()
		if err == nil {
			return i, true
		}
		return 0, false
	default:
		return 0, false
	}
}

func stringSliceFromAny(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		if s, ok := it.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// parseGatewayStatusJSON parses `openclaw gateway status --json` output.
// The JSON has shape: {"service":{"loaded":true,"runtime":{...}},...}
func ParseGatewayStatusJSON(ctx context.Context, output string) SystemGateway {
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
				gw.Uptime, gw.Memory = GetProcessInfo(ctx, gw.PID)
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
func FormatBytes(b int64) string {
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
func GetProcessInfo(ctx context.Context, pid int) (uptime string, memory string) {
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
			memory = FormatBytes(rssKB * 1024)
		}
	}
	return
}

// detectGatewayFallback checks if the gateway HTTP port is responding.
// timeoutMs controls how long to wait; defaults to 1500ms if <= 0.
func DetectGatewayFallback(ctx context.Context, gatewayPort int, timeoutMs int) SystemGateway {
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
		_ = resp.Body.Close()
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

var versionishTokenRe = regexp.MustCompile(`[0-9]+|[A-Za-z]+`)

func versionishGreater(a, b string) bool {
	ta := versionishTokenRe.FindAllString(strings.ToLower(a), -1)
	tb := versionishTokenRe.FindAllString(strings.ToLower(b), -1)
	n := len(ta)
	if len(tb) < n {
		n = len(tb)
	}
	for i := 0; i < n; i++ {
		ai, aErr := strconv.Atoi(ta[i])
		bi, bErr := strconv.Atoi(tb[i])
		switch {
		case aErr == nil && bErr == nil:
			if ai != bi {
				return ai > bi
			}
		case aErr == nil:
			return true
		case bErr == nil:
			return false
		default:
			if ta[i] != tb[i] {
				return ta[i] > tb[i]
			}
		}
	}
	return len(ta) > len(tb)
}

// resolveOpenclawBin finds the openclaw binary, checking PATH then known asdf locations.
// asdf shims may not be on the server's PATH when launched as a background process.
func ResolveOpenclawBin() string {
	if p, err := exec.LookPath("openclaw"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".asdf", "shims", "openclaw"),
	}
	// Also probe asdf nodejs installs — sort newest-first using version-aware comparison.
	if nodeDir := filepath.Join(home, ".asdf", "installs", "nodejs"); nodeDir != "" {
		if entries, err := os.ReadDir(nodeDir); err == nil {
			sort.Slice(entries, func(i, j int) bool { return versionishGreater(entries[i].Name(), entries[j].Name()) })
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
func FetchLatestNpmVersion(ctx context.Context, timeoutMs int) string {
	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://registry.npmjs.org/openclaw/latest", nil)
	if err != nil {
		log.Printf("[dashboard] FetchLatestNpmVersion: request creation failed: %v", err)
		return ""
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[dashboard] FetchLatestNpmVersion: request failed: %v", err)
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[dashboard] FetchLatestNpmVersion: unexpected status %d", resp.StatusCode)
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&pkg); err != nil {
		log.Printf("[dashboard] FetchLatestNpmVersion: JSON decode failed: %v", err)
		return ""
	}
	return pkg.Version
}
