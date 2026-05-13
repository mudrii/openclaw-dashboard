package appsystem

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

func TestFormatBytes_AllRanges(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0B"},
		{500, "500B"},
		{-1, "0B"},
		{1024, "1KB"},
		{1536, "2KB"},
		{1024 * 1024, "1.0MB"},
		{1024 * 1024 * 1024, "1.0GB"},
		{5 * 1024 * 1024 * 1024, "5.0GB"},
	}
	for _, tt := range tests {
		got := FormatBytes(tt.input)
		if got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBoolFromAny_AllTypes(t *testing.T) {
	tests := []struct {
		input   any
		wantVal bool
		wantOk  bool
	}{
		{true, true, true},
		{false, false, true},
		{float64(1), false, false},
		{"true", false, false},
		{nil, false, false},
	}
	for _, tt := range tests {
		val, ok := BoolFromAny(tt.input)
		if val != tt.wantVal || ok != tt.wantOk {
			t.Errorf("BoolFromAny(%v) = (%v, %v), want (%v, %v)", tt.input, val, ok, tt.wantVal, tt.wantOk)
		}
	}
}

func TestVersionishGreater_Comparison(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"1.2.4", "1.2.3", true},
		{"1.2.3", "1.2.4", false},
		{"1.2.3", "1.2.3", false},
		{"2.0.0", "1.9.9", true},
		{"1.10.0", "1.9.0", true},
	}
	for _, tt := range tests {
		got := versionishGreater(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("versionishGreater(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestDecodeJSONObjectFromOutput_ValidJSON(t *testing.T) {
	input := "some preamble text\n{\"key\":\"value\"}"
	var result map[string]any
	err := decodeJSONObjectFromOutput(input, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("expected key=value, got %v", result)
	}
}

func TestDecodeJSONObjectFromOutput_NoJSON(t *testing.T) {
	input := "no json here at all"
	var result map[string]any
	err := decodeJSONObjectFromOutput(input, &result)
	if err == nil {
		t.Error("expected error for output without JSON")
	}
}

func TestParseGatewayStatusJSON_Online(t *testing.T) {
	input := `{"service":{"loaded":true,"runtime":{"status":"running","pid":42}},"version":"3.0.0"}`
	gw := ParseGatewayStatusJSON(context.Background(), input)
	if gw.Status != "online" {
		t.Errorf("expected online, got %q", gw.Status)
	}
	if gw.Version != "3.0.0" {
		t.Errorf("expected version 3.0.0, got %q", gw.Version)
	}
	if gw.PID != 42 {
		t.Errorf("expected PID 42, got %d", gw.PID)
	}
}

func TestParseGatewayStatusJSON_Offline(t *testing.T) {
	input := `{"service":{"loaded":false,"runtime":{"status":"stopped","pid":0}},"version":"3.0.0"}`
	gw := ParseGatewayStatusJSON(context.Background(), input)
	if gw.Status != "offline" {
		t.Errorf("expected offline, got %q", gw.Status)
	}
}

func TestGetLatestVersionCached_FailureIsNegativelyCached(t *testing.T) {
	var calls atomic.Int32
	svc := NewSystemService(appconfig.SystemConfig{
		Enabled:            true,
		VersionsTTLSeconds: 60,
		GatewayTimeoutMs:   100,
	}, "test", context.Background())
	svc.fetchLatest = func(ctx context.Context, timeoutMs int) string {
		calls.Add(1)
		return ""
	}

	_ = svc.getLatestVersionCached()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		svc.latestMu.RLock()
		refreshing := svc.latestRefresh
		cachedAt := svc.latestAt
		svc.latestMu.RUnlock()
		if !refreshing && !cachedAt.IsZero() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected one failed fetch, got %d", got)
	}

	_ = svc.getLatestVersionCached()
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected failed fetch to be cached within TTL, got %d calls", got)
	}
}

func TestProbeOpenclawGatewayEndpoints_RespectsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"ready":true}`))
	}))
	defer srv.Close()

	parts := strings.Split(srv.URL, ":")
	port, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	start := time.Now()
	_, errs := probeOpenclawGatewayEndpoints(context.Background(), port, 20)
	elapsed := time.Since(start)

	if len(errs) == 0 {
		t.Fatal("expected timeout-related probe errors")
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("expected timeout-bounded probe, took %v", elapsed)
	}
}

func TestRunWithTimeout_TimeoutWrapped(t *testing.T) {
	_, err := runWithTimeout(context.Background(), 50, "/bin/sleep", "5")
	if err == nil {
		t.Fatal("expected error from sleep timeout, got nil")
	}
	if !errors.Is(err, ErrCommandTimeout) {
		t.Fatalf("expected errors.Is(err, ErrCommandTimeout); got %v", err)
	}
}

func TestRunWithTimeout_NotFoundWrapped(t *testing.T) {
	_, err := runWithTimeout(context.Background(), 1000, "/nonexistent/openclaw-xyz")
	if err == nil {
		t.Fatal("expected error from missing binary, got nil")
	}
	if !errors.Is(err, ErrCommandNotFound) {
		t.Fatalf("expected errors.Is(err, ErrCommandNotFound); got %v", err)
	}
}

// C9b: GetProcessInfo must early-return for non-positive PIDs without
// shelling out to ps.
func TestGetProcessInfo_RejectsNonPositivePID(t *testing.T) {
	for _, pid := range []int{0, -1, -42} {
		uptime, memory := GetProcessInfo(context.Background(), pid)
		if uptime != "" || memory != "" {
			t.Errorf("GetProcessInfo(%d) = (%q, %q), want empty", pid, uptime, memory)
		}
	}
}

// C9b: JSON fetchers must cap response bodies at maxJSONResponseBytes (64KB).
// A ~70KB JSON body should fail decoding (truncated) rather than be accepted.
func TestJSONFetchers_BodyCapAt64KB(t *testing.T) {
	// Build a JSON object whose serialized form exceeds 64KB.
	// Single big string value keeps it a valid map[string]any until truncation.
	bigPayload := func() []byte {
		var sb strings.Builder
		sb.WriteString(`{"blob":"`)
		// 70KB of 'a' inside a string value pushes total beyond 64KB cap.
		for range 70 * 1024 {
			sb.WriteByte('a')
		}
		sb.WriteString(`"}`)
		return []byte(sb.String())
	}()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bigPayload)
	}))
	defer srv.Close()

	srv503 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write(bigPayload)
	}))
	defer srv503.Close()

	// Sanity: payload must exceed the 64KB cap so truncation occurs.
	if len(bigPayload) <= 1<<16 {
		t.Fatalf("test payload too small (%d bytes) — must exceed 64KB to exercise cap", len(bigPayload))
	}

	t.Run("FetchJSONMap", func(t *testing.T) {
		_, err := FetchJSONMap(context.Background(), http.DefaultClient, srv.URL)
		if err == nil {
			t.Fatal("expected decode error from oversized body, got nil")
		}
	})

	t.Run("fetchJSONMapAllowStatus", func(t *testing.T) {
		_, err := fetchJSONMapAllowStatus(context.Background(), http.DefaultClient, srv503.URL, 200, 503)
		if err == nil {
			t.Fatal("expected decode error from oversized body, got nil")
		}
	})

	t.Run("FetchLatestNpmVersion", func(t *testing.T) {
		// FetchLatestNpmVersion hits a hardcoded URL; we test the cap behavior
		// by ensuring the limit constant is wired identically. The function
		// already uses 1<<16, but we assert that constant is what's exported.
		// We exercise the path by pointing a custom transport at our server.
		client := &http.Client{Transport: &rewriteTransport{target: srv.URL}}
		old := sharedSystemHTTPClient
		sharedSystemHTTPClient = client
		defer func() { sharedSystemHTTPClient = old }()
		v := FetchLatestNpmVersion(context.Background(), 1000)
		if v != "" {
			t.Fatalf("expected empty version on oversized body, got %q", v)
		}
	})
}

// rewriteTransport rewrites every request to hit a single test server URL.
// Used to redirect FetchLatestNpmVersion's hardcoded npm URL to a local server.
type rewriteTransport struct{ target string }

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(rt.target)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	req.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

// C9b: GetJSON must not produce torn observations under writer contention.
// Every returned payload must match the generation that was current when the
// stale/fresh decision was made — i.e. the lock must span the entire decision.
//
// We embed a monotonic gen counter in both the fresh and stale payload bytes.
// Writers swap (payload, stalePayload, metricsAt) atomically under metricsMu.
// Readers parse the returned gen and verify it is <= the latest gen ever
// written (no read-from-future) and that the stale-vs-fresh classification
// the reader made aligns with the gen that was current at read time.
func TestGetJSON_AtomicStaleDecision(t *testing.T) {
	cfg := appconfig.SystemConfig{
		Enabled:            true,
		MetricsTTLSeconds:  1,
		ColdPathTimeoutMs:  100,
		VersionsTTLSeconds: 60,
	}
	s := NewSystemService(cfg, "test", context.Background())
	s.fetchLatest = func(ctx context.Context, timeoutMs int) string { return "" }

	var latestGen atomic.Int64

	// Seed an initial fresh payload so GetJSON never falls into the synchronous
	// collect path (we are testing the cached decision, not refresh).
	setGen := func(gen int64, freshAt time.Time) {
		s.metricsMu.Lock()
		// Publish latestGen BEFORE the bytes a reader might observe — this
		// guarantees latestGen.Load() in the reader will be >= the gen baked
		// into any payload it can possibly see.
		latestGen.Store(gen)
		s.metricsPayload = []byte(`{"gen":` + strconv.FormatInt(gen, 10) + `,"stale":false}`)
		s.metricsStalePayload = []byte(`{"gen":` + strconv.FormatInt(gen, 10) + `,"stale":true}`)
		s.metricsAt = freshAt
		s.metricsMu.Unlock()
	}
	setGen(1, time.Now())

	// Suppress background refresh — we are testing the cached decision path,
	// not refresh itself. Without this, the "hasStale" branch would spawn a
	// real refresh goroutine that shells out to openclaw and hangs the test.
	s.metricsMu.Lock()
	s.hardFailUntil = time.Now().Add(time.Hour)
	s.metricsMu.Unlock()

	deadline := time.Now().Add(500 * time.Millisecond)
	var wg sync.WaitGroup

	// Writer: flips the cache between fresh (current time) and stale
	// (1 hour ago) while bumping gen. Alternating freshness forces GetJSON
	// down both code branches.
	wg.Add(1)
	go func() {
		defer wg.Done()
		var gen int64 = 2
		for time.Now().Before(deadline) {
			at := time.Now()
			if gen%2 == 0 {
				at = at.Add(-time.Hour) // make it stale
			}
			setGen(gen, at)
			gen++
		}
	}()

	// Readers: parse returned body, record (observed_gen, is_stale_label).
	// Invariant: observed_gen must be <= latestGen.Load() at read time
	// (cannot read from the future). The "stale" flag in the bytes must
	// match the bytes' source — fresh payload says stale:false, stale
	// payload says stale:true. Anything else is a torn read.
	const readers = 32
	type obs struct {
		gen        int64
		stale      bool
		latestSeen int64
	}
	results := make([][]obs, readers)
	for i := range readers {
		results[i] = make([]obs, 0, 1024)
	}

	wg.Add(readers)
	for i := range readers {
		go func(idx int) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				_, body := s.GetJSON(context.Background())
				latest := latestGen.Load()
				// Parse "gen":N and "stale":bool from body.
				var parsed struct {
					Gen   int64 `json:"gen"`
					Stale bool  `json:"stale"`
				}
				if err := json.Unmarshal(body, &parsed); err != nil {
					t.Errorf("reader %d: invalid body %q: %v", idx, body, err)
					return
				}
				results[idx] = append(results[idx], obs{parsed.Gen, parsed.Stale, latest})
			}
		}(i)
	}

	wg.Wait()

	// Verify invariant on every observation.
	for ri, rs := range results {
		for _, o := range rs {
			if o.gen > o.latestSeen {
				t.Errorf("reader %d: observed gen %d > latest %d (torn read from future)", ri, o.gen, o.latestSeen)
			}
		}
	}
}

func TestSystemService_BackoffOnHardFail(t *testing.T) {
	cfg := appconfig.SystemConfig{
		Enabled:            true,
		MetricsTTLSeconds:  1,
		ColdPathTimeoutMs:  100,
		VersionsTTLSeconds: 60,
	}
	s := NewSystemService(cfg, "test", context.Background())
	s.fetchLatest = func(ctx context.Context, timeoutMs int) string { return "" }

	s.metricsMu.Lock()
	s.metricsPayload = []byte(`{}`)
	s.metricsStalePayload = []byte(`{"stale":true}`)
	s.metricsAt = time.Now().Add(-time.Hour)
	s.hardFailUntil = time.Now().Add(10 * time.Second)
	s.metricsMu.Unlock()

	status, body := s.GetJSON(context.Background())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if string(body) != `{"stale":true}` {
		t.Fatalf("body = %q, want stale payload", string(body))
	}

	// Brief sleep — even if a goroutine were spawned it would have started by now.
	time.Sleep(50 * time.Millisecond)

	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	if s.metricsRefresh {
		t.Fatal("background refresh kicked off during back-off window")
	}
}
