package appsystem

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
