package appsystem

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// silenceSlog discards structured log output for the duration of a benchmark.
// The cache-priming refresh emits WARN lines on the same stream as benchmark
// results, which corrupts the samples benchstat parses.
func silenceSlog(tb testing.TB) {
	tb.Helper()
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	tb.Cleanup(func() { slog.SetDefault(old) })
}

func BenchmarkGetJSON_CacheHit(b *testing.B) {
	// Hermetic: every probe the priming refresh makes (gateway healthz/readyz,
	// npm dist-tags) is routed to a local httptest server, so the benchmark
	// never leaves the machine and never waits out a network timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"latest":"2026.9.1","status":"ok"}`))
	}))
	b.Cleanup(srv.Close)
	swapSharedSystemHTTPClient(b, &http.Client{Transport: &rewriteTransport{target: srv.URL}})
	silenceSlog(b)

	cfg := appconfig.SystemConfig{
		Enabled:            true,
		MetricsTTLSeconds:  3600,
		PollSeconds:        10,
		DiskPath:           "/",
		GatewayTimeoutMs:   100,
		GatewayPort:        18789,
		VersionsTTLSeconds: 3600,
		CPU:                appconfig.MetricThreshold{Warn: 80, Critical: 95},
		RAM:                appconfig.MetricThreshold{Warn: 80, Critical: 95},
		Swap:               appconfig.MetricThreshold{Warn: 50, Critical: 80},
		Disk:               appconfig.MetricThreshold{Warn: 80, Critical: 95},
	}

	ctx := b.Context()

	svc := NewSystemService(cfg, "1.0.0", ctx)

	// Prime cache with a synchronous refresh
	svc.GetJSON(ctx)

	// Reset TTL to ensure cache hits
	svc.metricsMu.Lock()
	svc.metricsAt = time.Now()
	svc.metricsMu.Unlock()

	b.ResetTimer()
	for b.Loop() {
		svc.GetJSON(ctx)
	}
}

func BenchmarkFormatBytes(b *testing.B) {
	values := []int64{0, 512, 1024, 1048576, 1073741824, 5368709120}
	b.ResetTimer()
	for b.Loop() {
		for _, v := range values {
			FormatBytes(v)
		}
	}
}
