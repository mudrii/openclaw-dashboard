# Implementation Plan: Live Log Tail & Error Feed Panel

**Issue:** mudrii/openclaw-dashboard#14
**Author:** agrr971
**Status:** Open (enhancement)
**Created:** 2026-04-09

---

## Problem Statement

The dashboard shows *that* something is wrong (alerts banner) but not *what*. Debugging
requires SSH + manual log correlation across multiple terminal windows. A solo operator
spends 5-10 minutes per incident just locating the relevant stack trace. An in-dashboard
log tail collapses that loop to seconds.

---

## Log File Discovery (Actual System State)

OpenClaw writes the following log files under `~/.openclaw/`:

| File | Size | Lines | Format |
|------|------|-------|--------|
| `logs/gateway.log` | 5.4 MB | 54K | `<ISO-8601> [component] [channel] message` |
| `logs/gateway.err.log` | 3.5 MB | 23K | Unstructured config warnings/errors |
| `logs/commands.log` | 163 B | ~5 | JSONL command history |
| `logs/config-audit.jsonl` | 23 KB | -- | JSONL audit trail |
| `cron/runs/*.jsonl` | varies | -- | Per-cron-run JSONL (NOT .json) |
| `agents/*/sessions/*.jsonl` | varies | -- | Per-session token usage JSONL |

### Gateway Log Format (Primary Source)

```
2026-04-09T20:43:01.613+08:00 [discord] [default] starting provider (@MudriiClawBot)
2026-04-09T20:08:00.683+08:00 [health-monitor] [slack:default] restarting (reason: stale-socket)
```

**Pattern:** `<timestamp> [<component>] [<channel>] <message>`
- Timestamp: RFC3339 with timezone offset
- Component: `discord`, `slack`, `health-monitor`, `plugins`, `cron`, etc.
- Channel: optional sub-qualifier

### Gateway Error Log Format

```
Config (/path/openclaw.json): missing env var "KEY" at path -- feature unavailable
```

- No timestamps (use file mtime as fallback)
- Implicitly all severity=error/warn

---

## Architecture Analysis

### Current Data Flow

```
Browser GET /api/refresh
  -> Server.handleRefresh()
    -> stale-while-revalidate check
    -> Server.runRefresh() [at most one goroutine]
      -> apprefresh.RunRefreshCollector()
        -> collectDashboardData() [WaitGroup: gateway, crons, gitLog]
        -> writes data.json atomically (tmp + rename)
    -> returns cached data.json bytes
```

### Design Decision: Separate Endpoints

The issue correctly proposes `/api/logs` and `/api/errors` as **separate endpoints** from
`/api/refresh`. This diverges from the single-endpoint architecture but is justified:

1. **Fast mode (3s poll)** -- cannot trigger full `collectDashboardData` every 3s
2. **Independent lifecycle** -- logs change much faster than session/cost data
3. **Selective fetching** -- mobile users can skip logs entirely
4. **Lightweight** -- log tailing is I/O-only, no subprocess spawning

### Files To Create

| File | Purpose |
|------|---------|
| `internal/apprefresh/logtail.go` | Log file tail reader, timestamp parser, severity classifier, multi-source merger |
| `internal/apprefresh/logtail_test.go` | Table-driven tests for parser, tail reader, severity classification |
| `internal/apprefresh/errorfeed.go` | Error signature normalizer, dedup store, bounded cache |
| `internal/apprefresh/errorfeed_test.go` | Tests for normalizer, dedup logic, cache eviction |
| `internal/appserver/server_logs.go` | HTTP handlers for `/api/logs` and `/api/errors` |
| `internal/appserver/server_logs_test.go` | Endpoint tests (httptest pattern) |

### Files To Modify

| File | Change |
|------|--------|
| `internal/appconfig/config.go` | Add `LogsConfig` struct, defaults, validation |
| `internal/appconfig/config_test.go` | Test new config defaults and clamping |
| `internal/appserver/server_core.go` | Add log collector fields to Server struct |
| `internal/appserver/server_routes.go` | Add 2 route cases in `ServeHTTP` switch |
| `web/index.html` | Add 2 new panels + JS logic + filter UI |
| `assets/runtime/config.json` | Add default log config keys |

---

## Implementation Steps (TDD Order)

### Step 1: Config -- `LogsConfig` Struct

**File:** `internal/appconfig/config.go`

```go
type LogsConfig struct {
    Enabled            bool     `json:"enabled"`
    TailLines          int      `json:"tailLines"`
    FastRefreshMs      int      `json:"fastRefreshMs"`
    ErrorWindowHours   int      `json:"errorWindowHours"`
    MaxErrorSignatures int      `json:"maxErrorSignatures"`
    Sources            []string `json:"sources"`
}
```

Add to `Config` struct:
```go
Logs LogsConfig `json:"logs"`
```

Defaults:
```go
Logs: LogsConfig{
    Enabled:            true,
    TailLines:          200,
    FastRefreshMs:      3000,
    ErrorWindowHours:   24,
    MaxErrorSignatures: 1000,
    Sources:            []string{}, // empty = auto-discover from OPENCLAW_HOME/logs/
},
```

Validation in `Load()`:
```go
if cfg.Logs.TailLines <= 0 || cfg.Logs.TailLines > 1000 {
    cfg.Logs.TailLines = 200
}
if cfg.Logs.FastRefreshMs < 1000 || cfg.Logs.FastRefreshMs > 30000 {
    cfg.Logs.FastRefreshMs = 3000
}
if cfg.Logs.ErrorWindowHours <= 0 || cfg.Logs.ErrorWindowHours > 168 {
    cfg.Logs.ErrorWindowHours = 24
}
if cfg.Logs.MaxErrorSignatures <= 0 || cfg.Logs.MaxErrorSignatures > 10000 {
    cfg.Logs.MaxErrorSignatures = 1000
}
```

**Tests:** Verify defaults, clamping edge cases, JSON round-trip.

---

### Step 2: Log Tail Reader -- `logtail.go`

**File:** `internal/apprefresh/logtail.go`

#### 2a. Data Types

```go
type LogEntry struct {
    Timestamp  string    `json:"timestamp"`
    TimeParsed time.Time `json:"-"`
    Source     string    `json:"source"`
    Component  string    `json:"component"`
    Severity   string    `json:"severity"`
    Message    string    `json:"message"`
    Raw        string    `json:"raw"`
}
```

#### 2b. Tail Reader (Efficient Backward Read)

Read last N lines from a file without loading entire file into memory:

```go
func tailFile(path string, n int) ([]string, error)
```

Strategy: `os.Open` -> `Seek(0, io.SeekEnd)` -> read backward in 8KB chunks ->
split by newline -> collect last N lines. This handles the 5.4MB gateway.log
without memory pressure.

#### 2c. Gateway Log Parser

```go
var reGatewayLine = regexp.MustCompile(
    `^(\d{4}-\d{2}-\d{2}T[\d:.]+[+-]\d{2}:\d{2})\s+` +
    `\[([^\]]+)\]\s*` +
    `(?:\[([^\]]*)\]\s*)?` +
    `(.*)$`,
)

func parseGatewayLine(line string) (LogEntry, bool)
```

#### 2d. Severity Classifier

No explicit severity in gateway.log -- infer from content:

```go
func classifySeverity(component, message string) string
```

Rules:
- `error`, `fail`, `fatal`, `panic` in message -> `"error"`
- `warn`, `missing`, `stale`, `timeout`, `unavailable` in message -> `"warn"`
- Everything else -> `"info"`
- All lines from `gateway.err.log` -> `"error"` or `"warn"` (heuristic)

#### 2e. Multi-Source Merge

```go
func CollectLogs(openclawPath string, cfg appconfig.LogsConfig, loc *time.Location) []LogEntry
```

1. Discover log files: if `cfg.Sources` empty, glob `{openclawPath}/logs/*.log`
2. For each file: `tailFile(path, cfg.TailLines)` -> parse -> classify severity
3. Merge all entries, sort by `TimeParsed` descending
4. Limit to `cfg.TailLines` total entries

**Tests (table-driven):**
- Parse valid gateway line -> correct fields
- Parse line missing channel bracket -> still works
- Parse non-matching line -> returns raw with severity "info"
- Severity classification: "error" in message -> error, "stale-socket" -> warn
- Tail reader: file with 10 lines, request 5 -> returns last 5
- Tail reader: file with 3 lines, request 10 -> returns all 3
- Tail reader: empty file -> returns nil
- Multi-source merge: 2 files -> sorted by timestamp descending

---

### Step 3: Error Feed -- `errorfeed.go`

**File:** `internal/apprefresh/errorfeed.go`

#### 3a. Signature Normalizer

```go
var reUUID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
var reNumericID = regexp.MustCompile(`\b\d{4,}\b`)
var reTimestamp = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T[\d:.]+[+-]\d{2}:\d{2}`)

func normalizeMessage(msg string) string {
    s := reTimestamp.ReplaceAllString(msg, "<TS>")
    s = reUUID.ReplaceAllString(s, "<UUID>")
    s = reNumericID.ReplaceAllString(s, "<ID>")
    return s
}
```

#### 3b. Error Signature and Store

```go
type ErrorSignature struct {
    Source     string            `json:"source"`
    Severity  string            `json:"severity"`
    Signature string            `json:"-"`
    Sample    string            `json:"sample"`
    Count     int               `json:"count"`
    FirstSeen string            `json:"firstSeen"`
    LastSeen  string            `json:"lastSeen"`
    Recent    []ErrorOccurrence `json:"recent"`
}

type ErrorOccurrence struct {
    Timestamp string `json:"timestamp"`
    Message   string `json:"message"`
    Component string `json:"component"`
}

type ErrorStore struct {
    mu         sync.Mutex
    signatures map[string]*ErrorSignature
    maxSize    int
}
```

#### 3c. Collection Function

```go
func CollectErrors(entries []LogEntry, windowHours int, maxSignatures int) []ErrorSignature
```

1. Filter entries by severity == "error" or "warn"
2. Filter by `time.Now().Add(-windowHours * time.Hour)`
3. Normalize message -> compute signature key (`source + ":" + normalizedMsg`)
4. Group into `ErrorSignature` with count, first/last seen, last 3 occurrences
5. If signatures exceed `maxSignatures`, evict oldest by `lastSeen`
6. Return sorted by `lastSeen` descending

**Tests:**
- Single error -> 1 signature, count=1
- Two identical errors -> 1 signature, count=2, correct first/last seen
- UUID in message normalized -> same signature
- Numeric ID normalized -> same signature
- Window filter: old error excluded
- Max signatures: eviction of oldest
- Mixed severity: only error/warn included

---

### Step 4: HTTP Handlers -- `server_logs.go`

**File:** `internal/appserver/server_logs.go`

#### 4a. `/api/logs` Handler

```go
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
    if !s.cfg.Logs.Enabled {
        s.sendJSON(w, r, http.StatusServiceUnavailable,
            map[string]string{"error": "log tail disabled"})
        return
    }

    // Query params: source, since, limit
    // Resolve OPENCLAW_HOME
    // Call apprefresh.CollectLogs(...)
    // Apply source/since/limit filters
    // Return JSON
}
```

#### 4b. `/api/errors` Handler

```go
func (s *Server) handleErrors(w http.ResponseWriter, r *http.Request) {
    if !s.cfg.Logs.Enabled {
        s.sendJSON(w, r, http.StatusServiceUnavailable,
            map[string]string{"error": "error feed disabled"})
        return
    }

    // Resolve OPENCLAW_HOME
    // Call apprefresh.CollectLogs(...)
    // Call apprefresh.CollectErrors(entries, windowHours, maxSignatures)
    // Return JSON
}
```

#### 4c. Route Registration

**File:** `internal/appserver/server_routes.go` -- add to `ServeHTTP` switch:

```go
case isRead && r.URL.Path == "/api/logs":
    s.handleLogs(w, r)
case isRead && r.URL.Path == "/api/errors":
    s.handleErrors(w, r)
```

**Tests (httptest):**
- GET /api/logs -> 200 with JSON body
- GET /api/logs when disabled -> 503
- GET /api/logs?source=gateway -> filtered results
- GET /api/logs?limit=5 -> at most 5 entries
- GET /api/errors -> 200 with deduplicated errors
- GET /api/errors when disabled -> 503

---

### Step 5: Frontend -- Panel A (Live Log Tail)

**File:** `web/index.html`

#### 5a. HTML Structure

Add after Sub-Agent Activity section in a new "Diagnostics" row:

- Section with `data-section="logs"`
- Source filter chips: All / Gateway / Cron
- Severity toggles: Info / Warn / Error
- Regex filter input box
- Pause/Resume button
- Fast mode (3s) toggle
- Monospace scrolling container (`max-height: 400px, overflow-y: auto`)

#### 5b. JS Module -- LogTail

New `LogTail` object managing:
- `fetch()`: GET `/api/logs` with query params
- `render()`: Build log line HTML, all dynamic content through `esc()` for XSS safety
- `togglePause()` / `toggleFast()`: Timer management
- `setSource()` / `setSeverity()`: Client-side filtering (no refetch)
- Regex filter with match highlighting via `<mark>` tags (applied after `esc()`)
- Auto-scroll to bottom of container

**XSS Safety:** All log content passes through the existing `esc()` function
before DOM insertion, matching the codebase-wide pattern. Regex highlighting
wraps `<mark>` tags around already-escaped content.

#### 5c. Fast Mode Timer

- 3s `setInterval` when active
- `visibilitychange` listener: clear interval on `document.hidden`, restart on focus
- Configurable via `cfg.Logs.FastRefreshMs`

#### 5d. Auto-Pause on Hover

- `mouseenter` on scroll container sets `paused = true`
- `mouseleave` restores previous pause state
- Prevents scroll jumping while user is reading

---

### Step 6: Frontend -- Panel B (Error Feed)

#### 6a. HTML Structure

- Section with `data-section="errors"`
- Sort buttons: Recent / Frequent
- Table: Severity, Source, Count, Message (truncated), Last Seen, Expand arrow
- Expandable detail rows: full message, first seen, last 3 occurrences

#### 6b. JS Module -- ErrorFeed

New `ErrorFeed` object managing:
- `fetch()`: GET `/api/errors`
- `render()`: Build table rows, all content through `esc()`
- `sortBy(field)`: Client-side sort by count or lastSeen
- `toggle(i)`: Show/hide detail row for error at index i
- Severity color coding: error -> `var(--red)`, warn -> `var(--orange)`

---

### Step 7: Header Badge

Add error count badge to existing header bar:
- Red pill badge showing count of error-severity signatures
- Hidden when count is 0
- Click scrolls to errors section via `scrollIntoView({behavior:'smooth'})`

---

### Step 8: Integration into Main Refresh Cycle

- Call `LogTail.fetch()` and `ErrorFeed.fetch()` from OCUI refresh cycle
- Add `'logs'` and `'errors'` to `Sections.DEFAULTS`
- Log/error fetches are fire-and-forget (`.catch(() => {})`) to avoid blocking main data

---

## Acceptance Criteria Mapping

| # | Criterion | Implementation Step |
|---|-----------|---------------------|
| 1 | `/api/logs` returns last N lines, merged, sorted, as JSON | Step 4a |
| 2 | `/api/errors` returns deduplicated signatures with count/first/last seen | Step 4b |
| 3 | Live Log Tail renders monospace scrolling view, newest at bottom | Step 5b |
| 4 | Filter chips filter without refetching | Step 5b (client-side) |
| 5 | Free-text regex filter with inline highlights | Step 5b |
| 6 | Pause button + auto-pause on hover | Step 5c, 5d |
| 7 | Fast mode 3s polling, throttled on blur | Step 5c |
| 8 | Error Feed groups identical errors with counts | Step 3, 6b |
| 9 | Click to expand shows stack trace + last 3 occurrences | Step 6b |
| 10 | Header badge with error count, click scrolls to feed | Step 7 |
| 11 | All lines escaped via `esc()` | Steps 5b, 6b |
| 12 | Renders correctly in all 6 themes | Uses existing CSS vars |
| 13 | Empty state renders cleanly | Steps 5b, 6b (fallback messages) |
| 14 | Works on macOS and Linux | Pure Go stdlib |

---

## Risk Register

| Risk | Severity | Mitigation |
|------|----------|------------|
| Large log files (5+ MB) cause slow tail reads | High | Efficient backward seek -- never read full file |
| No timestamps in `gateway.err.log` | Medium | Use file mtime as fallback timestamp |
| Log format changes across OpenClaw versions | Medium | Graceful fallback to raw-line display |
| Memory growth from error signature store | Low | Bounded to `maxErrorSignatures` (default 1000) |
| Fast mode (3s) on slow systems | Medium | Throttle on `visibilitychange`; configurable interval |
| XSS from malicious log content | High | All fields through `esc()` before DOM insertion |
| Log path traversal attack | High | Validate paths under OpenClaw home; reject `..` |
| Cron runs are JSONL, not plain text | Low | Separate parser branch for `.jsonl` files |

---

## Issue Corrections

The issue contains several inaccuracies relative to the current codebase:

1. **"`refresh.sh` -- shell wrapper changes"** -- No `refresh.sh` exists. The dashboard is pure Go.
2. **Log file paths** -- Issue guesses generic paths. Actual: `~/.openclaw/logs/gateway.log`, `~/.openclaw/logs/gateway.err.log`, `~/.openclaw/cron/runs/*.jsonl`.
3. **Severity levels in logs** -- Gateway logs have NO explicit severity. Must be inferred from message content.
4. **"7-module JS structure"** -- Correct, but DirtyChecker and OCUI main loop also need updates.
5. **Session/cron context in errors** -- Correlating gateway log lines to sessions requires parsing session IDs from messages, which is fragile.

---

## Estimated Scope

| Component | New Files | Modified Files | Est. LoC |
|-----------|-----------|----------------|----------|
| Config | -- | 2 | ~40 |
| Log tail reader | 2 | -- | ~250 |
| Error feed | 2 | -- | ~200 |
| HTTP handlers | 2 | 2 | ~120 |
| Frontend (HTML + JS) | -- | 1 | ~300 |
| Config defaults | -- | 1 | ~10 |
| **Total** | **6** | **6** | **~920** |

---

## Implementation Order

```
Step 1: Config (LogsConfig)           ~30 min
Step 2: Log tail reader + tests       ~2 hr
Step 3: Error feed + tests            ~1.5 hr
Step 4: HTTP handlers + tests         ~1 hr
Step 5: Frontend Panel A (Log Tail)   ~2 hr
Step 6: Frontend Panel B (Error Feed) ~1.5 hr
Step 7: Header badge                  ~15 min
Step 8: Integration + smoke test      ~30 min
```

---

## Definition of Done

- [ ] All 14 acceptance criteria from the issue are met
- [ ] TDD workflow followed: acceptance test -> unit test -> implementation -> refactor
- [ ] Zero new Go dependencies (stdlib only)
- [ ] Zero frontend dependencies
- [ ] All log content XSS-escaped via `esc()`
- [ ] Log paths validated (no traversal)
- [ ] Works on macOS and Linux
- [ ] All 6 themes render correctly
- [ ] Empty states render cleanly
- [ ] `go vet ./...` passes
- [ ] `go test ./...` passes
- [ ] `go test -race ./...` passes
- [ ] Fast mode throttles on tab blur
- [ ] Error store bounded to configured max
- [ ] Large file (5+ MB) tail completes in < 100ms
