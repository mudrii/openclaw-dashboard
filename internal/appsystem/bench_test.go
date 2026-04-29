package appsystem

import (
	"context"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

func BenchmarkGetJSON_CacheHit(b *testing.B) {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
