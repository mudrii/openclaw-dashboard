package apprefresh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

// stubGatewayLock replaces the lock-metadata seam with a deterministic fake.
func stubGatewayLock(t *testing.T, lk gatewayLock, ok bool) {
	t.Helper()
	prev := readGatewayLockMeta
	readGatewayLockMeta = func(_ string) (gatewayLock, bool) { return lk, ok }
	t.Cleanup(func() { readGatewayLockMeta = prev })
}

// TestCollectGatewayHealth_LockProvidesPID proves INT-3: when the gateway lock
// names a live pid, the dashboard uses it for metadata (status online, pid set)
// without depending on pgrep matching the install-specific cmdline. pgrep is
// rigged to fail to prove the lock alone is sufficient.
func TestCollectGatewayHealth_LockProvidesPID(t *testing.T) {
	stubGatewayLock(t, gatewayLock{pid: 4321, createdAt: time.Now().Add(-2 * time.Hour)}, true)
	stubPgrep(t, "", nil) // pgrep finds nothing
	gw := collectGatewayHealthWithLock(context.Background(), "/some/openclaw", 0)
	if gw["status"] != "online" {
		t.Errorf("status = %v, want online (lock pid alive)", gw["status"])
	}
	if gw["pid"] != 4321 {
		t.Errorf("pid = %v, want 4321 (from lock)", gw["pid"])
	}
}

// TestCollectGatewayHealth_LockMissFallsBackToPgrep proves the lock is additive:
// when no usable lock exists, behavior is the existing pgrep path.
func TestCollectGatewayHealth_LockMissFallsBackToPgrep(t *testing.T) {
	stubGatewayLock(t, gatewayLock{}, false)
	stubPgrep(t, "55555\n", nil)
	gw := collectGatewayHealthWithLock(context.Background(), "/some/openclaw", 0)
	if gw["pid"] != 55555 {
		t.Errorf("pid = %v, want 55555 (pgrep fallback)", gw["pid"])
	}
}

// TestGatewayLockFilename pins the openclaw lock filename scheme:
// gateway.<sha256(configPath)[:8]>.lock (infra/gateway-lock.ts).
func TestGatewayLockFilename(t *testing.T) {
	configPath := "/Users/x/.openclaw/openclaw.json"
	sum := sha256.Sum256([]byte(configPath))
	want := "gateway." + hex.EncodeToString(sum[:])[:8] + ".lock"
	if got := gatewayLockFilename(configPath); got != want {
		t.Errorf("gatewayLockFilename = %q, want %q", got, want)
	}
}

// TestParseGatewayLock covers the lock payload parse: pid + createdAt extracted,
// malformed JSON and a missing/zero pid report ok=false so the caller ignores it.
func TestParseGatewayLock(t *testing.T) {
	t.Run("valid payload", func(t *testing.T) {
		data := []byte(`{"pid":4321,"createdAt":"2026-06-14T00:00:00Z","configPath":"/x","startTime":99}`)
		lk, ok := parseGatewayLock(data)
		if !ok {
			t.Fatalf("ok=false, want true")
		}
		if lk.pid != 4321 {
			t.Errorf("pid = %d, want 4321", lk.pid)
		}
		if !lk.createdAt.Equal(time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("createdAt = %v, want 2026-06-14T00:00:00Z", lk.createdAt)
		}
	})
	t.Run("missing pid is rejected", func(t *testing.T) {
		if _, ok := parseGatewayLock([]byte(`{"createdAt":"2026-06-14T00:00:00Z"}`)); ok {
			t.Errorf("ok=true for pid-less payload, want false")
		}
	})
	t.Run("malformed JSON is rejected", func(t *testing.T) {
		if _, ok := parseGatewayLock([]byte(`{not json`)); ok {
			t.Errorf("ok=true for malformed JSON, want false")
		}
	})
	t.Run("bad createdAt still ok with zero time", func(t *testing.T) {
		lk, ok := parseGatewayLock([]byte(`{"pid":7,"createdAt":"not-a-time"}`))
		if !ok {
			t.Fatalf("ok=false, want true (pid present)")
		}
		if !lk.createdAt.IsZero() {
			t.Errorf("createdAt = %v, want zero on parse failure", lk.createdAt)
		}
	})
}
