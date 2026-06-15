package appsystem

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// TestGetJSON_HardFailBackOff proves the back-pressure window: after a stale-hit
// background refresh hard-fails, GetJSON must NOT kick another refresh until the
// back-off window elapses — so a flapping collector can't be hammered every
// request. Uses the injectable refresh seam; the large TTL keeps the back-off
// window far longer than the test runtime.
func TestGetJSON_HardFailBackOff(t *testing.T) {
	s := NewSystemService(appconfig.SystemConfig{Enabled: true, MetricsTTLSeconds: 60}, "test", context.Background())

	var calls int32
	s.refresh = func(context.Context) ([]byte, bool) {
		atomic.AddInt32(&calls, 1)
		return nil, true // hard fail
	}

	// Seed a stale cache so GetJSON takes the stale-hit → background-refresh path.
	s.metricsMu.Lock()
	s.metricsPayload = []byte(`{"ok":true}`)
	s.metricsAt = time.Now().Add(-time.Hour)
	s.metricsMu.Unlock()

	// First call: returns stale and kicks the (failing) background refresh.
	if code, _ := s.GetJSON(context.Background()); code != 200 {
		t.Fatalf("first GetJSON code = %d, want 200 (stale served)", code)
	}

	// Wait for the background refresh goroutine to finish: it clears
	// metricsRefresh and sets hardFailUntil under one lock, so refresh==false
	// implies the back-off window is now set.
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.metricsMu.RLock()
		settled := !s.metricsRefresh && atomic.LoadInt32(&calls) >= 1
		s.metricsMu.RUnlock()
		if settled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background refresh did not complete")
		}
		time.Sleep(time.Millisecond)
	}

	// Second call within the 120s back-off window must NOT kick a new refresh.
	before := atomic.LoadInt32(&calls)
	if code, _ := s.GetJSON(context.Background()); code != 200 {
		t.Fatalf("second GetJSON code = %d, want 200 (stale still served)", code)
	}
	time.Sleep(20 * time.Millisecond) // let any erroneous goroutine run
	if got := atomic.LoadInt32(&calls); got != before {
		t.Errorf("refresh called again during back-off window: calls %d -> %d", before, got)
	}
}
