package appsystem

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// TestGetJSON_HardFailBackOff proves the back-pressure window: after a stale-hit
// background refresh hard-fails, GetJSON must NOT kick another refresh until the
// back-off window elapses — so a flapping collector can't be hammered every
// request. Uses the injectable refresh seam; synctest.Wait replaces polling and
// sleeps by blocking until every goroutine in the bubble has settled.
func TestGetJSON_HardFailBackOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewSystemService(appconfig.SystemConfig{Enabled: true, MetricsTTLSeconds: 60}, "test", context.Background())

		var calls atomic.Int32
		s.refresh = func(context.Context) ([]byte, bool) {
			calls.Add(1)
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
		synctest.Wait() // background refresh finished and set the back-off window
		if got := calls.Load(); got != 1 {
			t.Fatalf("refresh calls after first GetJSON = %d, want 1", got)
		}

		// Second call within the 120s back-off window must NOT kick a new refresh.
		if code, _ := s.GetJSON(context.Background()); code != 200 {
			t.Fatalf("second GetJSON code = %d, want 200 (stale still served)", code)
		}
		synctest.Wait() // any erroneous refresh goroutine would have run by now
		if got := calls.Load(); got != 1 {
			t.Errorf("refresh called again during back-off window: calls = %d, want 1", got)
		}

		// Once the window elapses the next stale hit refreshes again.
		time.Sleep(121 * time.Second)
		s.GetJSON(context.Background())
		synctest.Wait()
		if got := calls.Load(); got != 2 {
			t.Errorf("refresh calls after back-off window = %d, want 2", got)
		}
	})
}
