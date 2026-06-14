package apprefresh

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// gatewayLock is the subset of openclaw's gateway lock payload the dashboard
// uses for process metadata (infra/gateway-lock.ts).
type gatewayLock struct {
	pid       int
	createdAt time.Time
}

// gatewayLockFilename returns the lock filename for a config path:
// gateway.<sha256(configPath)[:8]>.lock — matching openclaw's scheme so the
// dashboard reads the same lock the gateway wrote, on every install layout.
func gatewayLockFilename(configPath string) string {
	sum := sha256.Sum256([]byte(configPath))
	return "gateway." + hex.EncodeToString(sum[:])[:8] + ".lock"
}

// parseGatewayLock parses a lock file payload. It requires a non-zero pid;
// createdAt is best-effort (zero time when absent or unparseable).
func parseGatewayLock(data []byte) (gatewayLock, bool) {
	var payload struct {
		PID       int    `json:"pid"`
		CreatedAt string `json:"createdAt"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return gatewayLock{}, false
	}
	if payload.PID <= 0 {
		return gatewayLock{}, false
	}
	lk := gatewayLock{pid: payload.PID}
	if t, err := time.Parse(time.RFC3339, payload.CreatedAt); err == nil {
		lk.createdAt = t
	}
	return lk, true
}

// gatewayLockConfigPath resolves the openclaw config path used to key the lock:
// OPENCLAW_CONFIG_PATH when set, else <openclawPath>/openclaw.json.
func gatewayLockConfigPath(openclawPath string) string {
	if env := os.Getenv("OPENCLAW_CONFIG_PATH"); env != "" {
		return env
	}
	return filepath.Join(openclawPath, "openclaw.json")
}

// gatewayLockDir resolves openclaw's lock directory: <tmpdir>/openclaw-<uid>
// (uid suffix when available), matching resolveGatewayLockDir.
func gatewayLockDir() string {
	suffix := "openclaw"
	if uid := os.Getuid(); uid >= 0 {
		suffix = "openclaw-" + strconv.Itoa(uid)
	}
	return filepath.Join(os.TempDir(), suffix)
}

// pidAlive reports whether a process is alive. Func var so tests can stub it
// without spawning processes; signal 0 performs an existence/permission check.
var pidAlive = func(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// readGatewayLockMeta reads the gateway lock for this install and returns the
// recorded pid and createdAt when the lock exists and its process is alive.
// Func var so collectGatewayHealth tests can stub it. Best-effort: any failure
// (no lock, stale pid, parse error) returns ok=false and the caller falls back
// to pgrep.
var readGatewayLockMeta = func(openclawPath string) (gatewayLock, bool) {
	configPath := gatewayLockConfigPath(openclawPath)
	lockPath := filepath.Join(gatewayLockDir(), gatewayLockFilename(configPath))
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return gatewayLock{}, false
	}
	lk, ok := parseGatewayLock(data)
	if !ok || !pidAlive(lk.pid) {
		return gatewayLock{}, false
	}
	return lk, true
}
