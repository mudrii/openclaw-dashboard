package appsystem

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// SystemService collects host metrics and versions with TTL caching.
type SystemService struct {
	cfg         appconfig.SystemConfig
	dashVer     string
	shutdownCtx context.Context // lifecycle context — cancelled on graceful shutdown; do NOT use for per-request ops

	// fetchLatest probes the npm registry for the newest published openclaw
	// version. Set per-instance so each test can override it without touching a
	// shared package var (which previously raced with the goroutine in
	// getLatestVersionCached during test cleanup).
	fetchLatest func(ctx context.Context, timeoutMs int) string

	// refresh collects fresh metrics and reports a hard failure. A per-instance
	// field (like fetchLatest) so tests can inject a failing refresh to exercise
	// the hard-fail back-off window; defaults to refreshMetrics.
	refresh func(ctx context.Context) ([]byte, bool)

	// Collector seams used by refreshMetrics; tests replace them to model
	// failing or hung collectors without touching the host.
	collectDisk       func(path string) SystemDisk
	collectCPURAMSwap func(ctx context.Context, cpuTimeoutMs int) (SystemCPU, SystemRAM, SystemSwap)
	collectOpenclaw   func(ctx context.Context, oclawBin string) SystemOpenclaw

	metricsMu           sync.RWMutex
	metricsPayload      []byte
	metricsStalePayload []byte // pre-computed version with "stale":true
	metricsHardFail     bool   // cached payload came from a collection where every core collector failed
	metricsAt           time.Time
	metricsRefresh      bool
	hardFailUntil       time.Time       // back-off window: skip background refresh while now < hardFailUntil
	coldCall            *coldCollection // in-flight synchronous collection shared by cold callers

	diskInFlight atomic.Bool // a statfs call is still running (possibly hung)

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

// Fallbacks applied when a caller passes a non-positive port or timeout.
const (
	defaultGatewayPort           = 18789 // OpenClaw gateway default HTTP port
	defaultGatewayProbeTimeoutMs = 1500  // gateway healthz/readyz/HEAD probes
	defaultCommandTimeoutMs      = 5000  // external CLI invocations
	npmLatestTimeoutMs           = 3000  // npm dist-tags lookup
	processInfoTimeout           = 3 * time.Second
)

// coldCollection is a synchronous collection shared by every GetJSON caller
// that finds the cache empty while it runs. code and body are written before
// done is closed and are read-only afterwards.
type coldCollection struct {
	done chan struct{}
	code int
	body []byte
}

// NewSystemService returns a SystemService for cfg. dashVer is reported as the
// dashboard version; serverCtx is the server lifecycle context used for
// background refreshes (and the selected OpenClaw target), never for
// per-request work.
func NewSystemService(cfg appconfig.SystemConfig, dashVer string, serverCtx context.Context) *SystemService {
	s := &SystemService{
		cfg:               cfg,
		dashVer:           dashVer,
		shutdownCtx:       serverCtx,
		fetchLatest:       FetchLatestNpmVersion,
		collectDisk:       CollectDiskRoot,
		collectCPURAMSwap: collectCPURAMSwapParallel,
	}
	s.collectOpenclaw = func(ctx context.Context, oclawBin string) SystemOpenclaw {
		return CollectOpenclawRuntime(ctx, oclawBin, s.cfg.GatewayTimeoutMs, s.cfg.GatewayPort, SystemVersions{}, s.cfg.DeepStatus)
	}
	s.refresh = s.refreshMetrics
	return s
}

// SetMetricsTimestampForTest overrides the metrics cache timestamp so tests
// can age the cached payload.
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
//
// The cache decision (fresh / stale / cold) happens inside a single critical
// section so a reader never observes a torn snapshot of (payload, metricsAt,
// hardFailUntil, metricsRefresh) under writer contention. The captured payload
// and refresh intent are then acted on after the lock is released.
func (s *SystemService) GetJSON(ctx context.Context) (int, []byte) {
	if !s.cfg.Enabled {
		return http.StatusServiceUnavailable, []byte(`{"ok":false,"error":"system metrics disabled"}`)
	}

	ttl := time.Duration(s.cfg.MetricsTTLSeconds) * time.Second

	// Single decision under one lock: classify cache state and, if a stale
	// hit needs a background refresh, claim the refresh slot atomically. A
	// cold miss joins the in-flight synchronous collection or becomes its
	// leader, so concurrent cold callers share one collection.
	s.metricsMu.Lock()
	var (
		freshPayload []byte
		stalePayload []byte
		cachedCode   = http.StatusOK
		kickRefresh  bool
		cold         *coldCollection
		coldLeader   bool
	)
	if s.metricsHardFail {
		cachedCode = http.StatusServiceUnavailable
	}
	switch {
	case s.metricsPayload != nil && time.Since(s.metricsAt) < ttl:
		freshPayload = s.metricsPayload
	case s.metricsPayload != nil:
		// Stale hit. Return the pre-marked stale payload (or fall back to the
		// fresh-marked payload if none was pre-computed). Kick a background
		// refresh unless we are in the hard-fail back-off window or another
		// refresh is already in flight.
		stalePayload = s.metricsStalePayload
		if stalePayload == nil {
			stalePayload = s.metricsPayload
		}
		if !time.Now().Before(s.hardFailUntil) && !s.metricsRefresh {
			s.metricsRefresh = true
			kickRefresh = true
		}
	default:
		if s.coldCall == nil {
			s.coldCall = &coldCollection{done: make(chan struct{})}
			coldLeader = true
		}
		cold = s.coldCall
	}
	s.metricsMu.Unlock()

	if freshPayload != nil {
		return cachedCode, freshPayload
	}
	if stalePayload != nil {
		if kickRefresh {
			go func() {
				data, hardFail := s.refresh(s.shutdownCtx)
				if data == nil || hardFail {
					slog.Warn("[system] background refresh failed", "data_nil", data == nil, "hard_fail", hardFail)
				}
				s.metricsMu.Lock()
				s.metricsRefresh = false
				if hardFail {
					backoff := 2 * time.Duration(s.cfg.MetricsTTLSeconds) * time.Second
					s.hardFailUntil = time.Now().Add(backoff)
				} else {
					s.hardFailUntil = time.Time{}
				}
				s.metricsMu.Unlock()
			}()
		}
		return cachedCode, stalePayload
	}

	if !coldLeader {
		select {
		case <-cold.done:
			return cold.code, cold.body
		case <-ctx.Done():
			return http.StatusServiceUnavailable, coldUnavailableBody
		}
	}

	// No cache — collect synchronously on behalf of every cold caller. The
	// result is shared and cached, so it must not inherit this request's
	// cancellation; refreshMetrics bounds it with the cold-path timeout.
	cold.code, cold.body = s.collectCold(context.WithoutCancel(ctx))
	s.metricsMu.Lock()
	s.coldCall = nil
	s.metricsMu.Unlock()
	close(cold.done)
	return cold.code, cold.body
}

// coldUnavailableBody is returned when a cold collection produced no payload.
var coldUnavailableBody = []byte(`{"ok":false,"degraded":true,"error":"system metrics unavailable"}`)

// collectCold runs one synchronous collection and maps it to a response.
func (s *SystemService) collectCold(ctx context.Context) (int, []byte) {
	data, hardFail := s.refresh(ctx)
	if data == nil {
		return http.StatusServiceUnavailable, coldUnavailableBody
	}
	if hardFail {
		return http.StatusServiceUnavailable, data
	}
	return http.StatusOK, data
}

// refresh collects fresh metrics and returns (jsonBytes, isHardFail).
// isHardFail=true when ALL core collectors failed (no useful data).
//
// Cold-path bounding: the entire collection runs under a single
// context.WithTimeout(ColdPathTimeoutMs) so a hung gateway can never drag the
// whole refresh past the frontend's fetch deadline. Versions and OpenClaw
// runtime are collected in parallel; on the cold path that halves the worst
// case from ~2 × GatewayTimeoutMs down to ~1 × GatewayTimeoutMs (or the cold
// budget, whichever fires first).
func (s *SystemService) refreshMetrics(ctx context.Context) ([]byte, bool) {
	ctx = appopenclaw.WithTarget(ctx, appopenclaw.TargetFromContext(s.shutdownCtx))
	coldPath := time.Duration(s.cfg.ColdPathTimeoutMs) * time.Millisecond
	if coldPath <= 0 {
		coldPath = time.Duration(appconfig.DefaultColdPathTimeoutMs) * time.Millisecond
	}
	coldCtx, cancel := context.WithTimeout(ctx, coldPath)
	defer cancel()

	var ver SystemVersions
	var openclaw SystemOpenclaw
	var disk SystemDisk
	var cpu SystemCPU
	var ram SystemRAM
	var swap SystemSwap
	oclawBin := s.openclawBin()
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		ver = s.getVersionsCached(coldCtx)
		if latest := s.getLatestVersionCached(); latest != "" {
			ver.Latest = latest
		}
	}()
	go func() {
		defer wg.Done()
		// Run independently of the versions goroutine — both probe the gateway,
		// so serializing them would double the cold-path wall time. We pass
		// SystemVersions{} here and patch openclaw.Status.{Current,Latest}Version
		// from `ver` after wg.Wait() once both goroutines have finished.
		openclaw = s.collectOpenclaw(coldCtx, oclawBin)
	}()
	go func() { defer wg.Done(); disk = s.collectDiskBounded(coldCtx) }()
	go func() {
		defer wg.Done()
		cpu, ram, swap = s.collectCPURAMSwap(coldCtx, s.cfg.CPUTimeoutMs)
	}()
	wg.Wait()

	deadlineHit := coldCtx.Err() == context.DeadlineExceeded

	if openclaw.Status.CurrentVersion == "" {
		openclaw.Status.CurrentVersion = ver.Openclaw
	}
	if openclaw.Status.LatestVersion == "" {
		openclaw.Status.LatestVersion = ver.Latest
	}

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
	if deadlineHit {
		resp.Degraded = true
		resp.Errors = append(resp.Errors, "cold path: deadline exceeded")
	}

	b, err := json.Marshal(resp)
	if err != nil {
		slog.Error("[dashboard] collectMetrics: json.Marshal failed", "error", err)
		return nil, true
	}

	// Pre-compute stale payload to avoid JSON round-trip on every stale hit
	resp.Stale = true
	staleB, staleErr := json.Marshal(resp)
	if staleErr != nil {
		staleB = b
	}

	s.metricsMu.Lock()
	s.metricsPayload = b
	s.metricsStalePayload = staleB
	s.metricsHardFail = allFailed
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
	s.verRefresh = false
	// Cache only when ctx finished cleanly. If a cold-path deadline cut us
	// short, the result may be partial (e.g. empty Openclaw or unknown Gateway
	// status); persisting it would poison the warm cache and skip the next
	// real collection silently.
	if ctx.Err() == nil {
		s.verCached = v
		s.verAt = time.Now()
	}
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
	v := s.latestVer // capture before goroutine mutates
	s.latestRefresh = true
	s.latestMu.Unlock()

	go func() {
		latest := s.fetchLatest(s.shutdownCtx, npmLatestTimeoutMs)
		now := time.Now()
		s.latestMu.Lock()
		if latest != "" {
			s.latestVer = latest
		}
		// Negative caching: stamp latestAt unconditionally so an empty result
		// (npm down, decode failure, non-200) suppresses retries for the full
		// VersionsTTLSeconds window. This protects the npm registry from
		// request storms during outages — at the cost of a slight recovery
		// delay (≤ TTL) when npm comes back. NOT analogous to the verAt cold-
		// path poisoning fix: that case caches *partial* results from a
		// deadline-cancelled in-progress collection; this case caches a
		// completed-but-failed external probe. Different concerns, different
		// answers. Locked in by TestGetLatestVersionCached_NegativeCaching.
		s.latestAt = now
		s.latestRefresh = false
		s.latestMu.Unlock()
	}()

	return v
}
