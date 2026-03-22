package appsystem

import (
	"context"
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
	prev := fetchLatestVersion
	defer func() { fetchLatestVersion = prev }()

	var calls atomic.Int32
	fetchLatestVersion = func(ctx context.Context, timeoutMs int) string {
		calls.Add(1)
		return ""
	}

	svc := NewSystemService(appconfig.SystemConfig{
		Enabled:            true,
		VersionsTTLSeconds: 60,
		GatewayTimeoutMs:   100,
	}, "test", context.Background())

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
