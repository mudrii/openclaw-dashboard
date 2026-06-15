package apprefresh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
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

// TestPidAlive covers the real syscall seam, including the EPERM case: pid 1
// (launchd/init) always exists but is owned by root, so a non-root signal-0
// probe returns EPERM — which must be treated as ALIVE, not dead. On a shared
// host the gateway can run under a different uid, and mis-reading EPERM as dead
// would discard the install-independent lock metadata INT-3 adds.
func TestPidAlive(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Errorf("pidAlive(os.Getpid()) = false, want true")
	}
	if pidAlive(0) {
		t.Errorf("pidAlive(0) = true, want false (non-positive pid)")
	}
	if pidAlive(-5) {
		t.Errorf("pidAlive(-5) = true, want false (negative pid)")
	}
	// A pid that is essentially guaranteed not to exist → ESRCH → dead.
	if pidAlive(2147483600) {
		t.Errorf("pidAlive(huge) = true, want false (no such process)")
	}
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
	uptime, _ := gw["uptime"].(string)
	if uptime != "1h 59m" && uptime != "2h 0m" && uptime != "2h 1m" {
		t.Errorf("uptime = %v, want about 2h (derived from lock createdAt)", gw["uptime"])
	}
}

func TestCollectGatewayHealth_LockDoesNotOverrideFailedHealthz(t *testing.T) {
	stubGatewayLock(t, gatewayLock{pid: 4321, createdAt: time.Now().Add(-2 * time.Hour)}, true)
	stubHealthz(t, false)
	stubPgrep(t, "", nil)
	gw := collectGatewayHealthWithLock(context.Background(), "/some/openclaw", 18789)
	if gw["status"] != "offline" {
		t.Errorf("status = %v, want offline when /healthz fails and pgrep finds nothing", gw["status"])
	}
	if gw["pid"] != nil {
		t.Errorf("pid = %v, want nil because stale lock metadata must not be trusted after failed /healthz", gw["pid"])
	}
}

func TestReadGatewayLockMeta_ReadsRealLockForCurrentProcess(t *testing.T) {
	prevTmp := os.Getenv("TMPDIR")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Cleanup(func() { _ = os.Setenv("TMPDIR", prevTmp) })

	configPath := filepath.Join(t.TempDir(), "openclaw.json")
	t.Setenv("OPENCLAW_CONFIG_PATH", configPath)
	lockDir := gatewayLockDir()
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(lockDir, gatewayLockFilename(configPath))
	createdAt := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	body := `{"pid":` + strconv.Itoa(os.Getpid()) + `,"createdAt":"` + createdAt.Format(time.RFC3339) + `"}`
	if err := os.WriteFile(lockPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	lk, ok := readGatewayLockMeta("/ignored")
	if !ok {
		t.Fatal("readGatewayLockMeta ok=false, want true for current process lock")
	}
	if lk.pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", lk.pid, os.Getpid())
	}
	if !lk.createdAt.Equal(createdAt) {
		t.Errorf("createdAt = %v, want %v", lk.createdAt, createdAt)
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

// TestGatewayLockConfigPath covers both branches of the config-path resolution:
// the OPENCLAW_CONFIG_PATH override and the default <openclawPath>/openclaw.json.
func TestGatewayLockConfigPath(t *testing.T) {
	t.Run("env override wins", func(t *testing.T) {
		t.Setenv("OPENCLAW_CONFIG_PATH", "/custom/oc.json")
		if got := gatewayLockConfigPath("/ignored"); got != "/custom/oc.json" {
			t.Errorf("got %q, want /custom/oc.json", got)
		}
	})
	t.Run("default joins openclawPath", func(t *testing.T) {
		t.Setenv("OPENCLAW_CONFIG_PATH", "")
		if got := gatewayLockConfigPath("/home/.openclaw"); got != "/home/.openclaw/openclaw.json" {
			t.Errorf("got %q, want /home/.openclaw/openclaw.json", got)
		}
	})
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
