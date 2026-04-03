# Changelog

## v2026.4.3 — 2026-04-03

### Changed
- **SRP refactor** — `buildSystemPrompt()` decomposed into six focused section helpers (`appendGatewaySection`, `appendCostsSection`, `appendSessionsSection`, `appendCronsSection`, `appendAlertsSection`, `appendConfigSection`), each under 30 lines
- **DRY helpers** — New `typeutil.go` with five type-assertion helpers (`getMap`, `getStr`, `getFloat`, `getSlice`, `fmtAny`); eliminates repeated two-stage assertion boilerplate throughout `chat.go`
- **Dependency injection for HTTP client** — `SystemService` now accepts an injectable `*http.Client`; test code no longer reaches the network; default client is created internally when `nil` is passed
- **Named constants** — Magic numbers extracted to named constants in both Go (`httpClientTimeout`, `gatewayAPIPath`, `httpReadTimeout`, `httpWriteTimeout`, `httpIdleTimeout`, `shutdownTimeout`) and JS (`CHART_MAX_LABELS`, `TICK_INTERVAL_MS`, `SYSTEM_POLL_INITIAL_MS`, `AUTO_REFRESH_SECONDS`)
- **Uniform JSON error responses** — All error paths (405, disabled-system 503, etc.) now go through `sendJSONRaw()` — no more text/plain mixed with application/json
- **`ai.maxTokens` config field** — New `AIConfig.MaxTokens` field (default 512, range 1–4096, values outside clamped to 512) passed to gateway as `max_tokens`
- **Frontend `deepClone` abstraction** — `State.snapshot()` uses `structuredClone` when available with a `JSON.parse/stringify` fallback; eliminates repeated deep-clone pattern
- **`DirtyChecker` fast-path** — Reference equality check before JSON.stringify; short-circuits on array length mismatch; reduces CPU for common no-change renders

### Added
- **`typeutil.go`** — DRY helper functions with full nil/missing/wrong-type safety
- **`typeutil_test.go`** — 17 tests covering all helpers and edge cases
- **`main_test.go`** — 4 tests: `localIP` format validation, determinism, default `MaxTokens`, `MaxTokens` clamping (6 table-driven subtests)
- **`server_test.go`** additions — 5 new tests: corrupt data 200, gateway unreachable 502, method-not-allowed returns JSON, malformed history filtered, chat body too large 413
- **`.golangci.yml`** — golangci-lint config (errcheck, govet, staticcheck, unused, ineffassign, gosimple, gocritic, revive, misspell; `exhaustruct` disabled)
- **CI Go job** — `.github/workflows/tests.yml` now runs `go test -race -coverprofile=coverage.out ./...` + `golangci/golangci-lint-action@v6`
- **`pytest-cov`** — Added to `requirements.txt` for Python coverage reporting

### Fixed
- **H1: `runRefresh` now participates in graceful shutdown** — was rooted at `context.Background()`, meaning `refresh.sh` could outlive SIGTERM by up to 15s; now uses `s.shutdownCtx` as the parent so cancellation propagates
- **H2: Stale flag no longer relies on byte-level injection** — replaced `bytes.Replace("stale":false → true)` with unmarshal → `resp.Stale = true` → remarshal; previous approach would silently stop working if `omitempty` was ever added to the field
- **M1: Exact path match for `/api/refresh`** — was `strings.HasPrefix` (matched `/api/refreshXXX`), now uses `==`
- **M3: Linux CPU dual-sample sleep is context-aware** — `time.Sleep(200ms)` replaced with `select { case <-time.After: case <-ctx.Done(): }` so shutdown doesn't block on the inter-sample gap
- **M4: 404 error responses include CORS headers** — `http.NotFound` calls in `handleStaticFile` and `ServeHTTP` now set CORS headers first via a new `notFound` helper; previously a missing file caused a confusing CORS error in the browser instead of a clean 404
- **L3: npm registry response cap tightened** — `io.LimitReader` in `fetchLatestNpmVersion` reduced from 1 MiB to 64 KiB (sufficient for any npm metadata response)
- Import ordering in `system_service.go` — `"bytes"` import removed (no longer needed after H2 fix)

---

## v2026.3.23 — 2026-03-22

### Changed
- **Go-only codebase** — Python server (`server.py`, `system_metrics.py`) and all Python test infrastructure removed; Go binary is now the sole runtime
- **Package reorganization** — Source moved into `internal/` packages for cleaner separation (server, config, system, chat)
- **`cmd/openclaw-dashboard`** entry point added; binary can now be installed via `go install`
- **Version detection** — Prefers `VERSION` file from binary's directory or parent; falls back to git tag; ignores parenthetical commit-hash suffixes for comparison
- **Cache refresh metadata** — Refresh timestamp and mtime tracked together, preventing stale-serve after rapid sequential refreshes

### Fixed
- **Data race in refresh path** — `lastRefresh` field access now fully guarded by `mu`; race detector passes clean
- **Broken Dockerfile** — Updated `FROM` base, `COPY` paths, and `CMD` for Go-only layout; `AC27` passes
- **`/api/system` stdout parsing** — `openclaw gateway status --json` stderr no longer included in parsed output
- **Version comparison** — Strip parenthetical suffixes (`v1.2.3 (abc1234)` → `v1.2.3`) before semver compare
- **Runtime badge** — `sysOclaw` prefix no longer duplicated in header pill text

---

## v2026.3.8 — 2026-03-08

### Added
- **Gateway runtime observability** — `/api/system` now includes a structured `openclaw` block: `runtime`, `readiness`, `live status`, `failing` dependencies, PID, memory, and config snapshots from `openclaw gateway readyz`
- **Gateway Runtime card** — System Settings section split into **Gateway Runtime** (live `/api/system` data) and **Gateway Config** (data from `data.json`) cards
- **Readiness alerts** — Alerts panel now surfaces `Gateway not ready: <dependency>` when `readyz` reports failing dependencies

### Fixed
- **`readyz` 503 parsing** — Payload is now parsed even when HTTP status is 503, keeping failing deps visible in the UI
- **`failing` list consistency** — Consistently passed through API payload and displayed in both Runtime and Alerts surfaces
- **localStorage key migration** — Configuration collapse defaults migrated from old key to new key without visual flash

### Changed
- **Binary/asset path resolution** — Repo root resolved correctly when binary is launched from a `dist/` subdirectory
- **Go and Python tests expanded** — New tests for system/runtime contract and frontend rendering

---

## v2026.3.7 — 2026-03-07

### Fixed (Security — P0)
- **Rate limiting** — Token-bucket (10 req/min per IP) on `/api/chat`; returns `429 Too Many Requests` with `Retry-After: 60` header
- **HTTP server timeouts** — Go server enforces Read 30s / Write 90s / Idle 120s; prevents slow-client and resource exhaustion attacks
- **Path traversal guard hardened** — Resolved path is checked to remain inside the serve root
- **Goroutine lifecycle** — Server lifecycle context propagated to all goroutines; all goroutines exit cleanly on SIGTERM

### Fixed (P1 Parity)
- **Python `ThreadingHTTPServer`** — Python server upgraded from single-threaded to `ThreadingHTTPServer`; a long refresh no longer blocks all concurrent clients
- **CORS on all error paths** — CORS headers now set on 4xx/5xx responses in both servers (previously only on 2xx)
- **Python gateway timeouts** — Explicit connect + read timeouts on all gateway HTTP calls

### Changed
- **Unified cache layer (Go)** — `loadData()` fills raw bytes + parsed map atomically under one lock; eliminates double-`stat`/double-read on concurrent requests
- **Mtime-based Python cache** — Python `/api/chat` re-reads `data.json` only when mtime changes (was per-request)
- **Pre-rendered index (Go)** — `index.html` embed pre-rendered at startup; zero per-request disk reads
- **Static file allowlist (Python)** — Python server restricts static file serving to an explicit allowlist; was previously serving any file in the repo directory including `.git/config`
- **Linux CPU sample interval** — Reduced from 200ms to 50ms for faster `/api/system` response time

### Added
- **87 Go tests** pass with `-race` flag
- **165 Python tests** pass

---

## v2026.3.6 — 2026-03-05

### Added
- **Collapsible sections** — 9 collapsible dashboard sections with right-aligned chevron toggles, localStorage persistence, FOUC prevention, Expand All / Collapse All buttons, and full ARIA keyboard navigation
- **OpenClaw version freshness** — Version pill in the top metrics bar is colour-coded: green (up to date), yellow (1 release behind), red (2+ releases behind); latest version polled from npm registry
- **npm latest version check** — `/api/system` now returns `versions.latest` (best-effort, non-blocking)

### Fixed (Multi-Model Audit — 30+ fixes)
- **Python static file allowlist** — Was exposing all repo files; now restricted to explicit list
- **CORS preflight handlers** — `OPTIONS` handler added to both Go and Python servers
- **HEAD body suppression** — All endpoints correctly suppress body on HEAD requests
- **UTF-8 rune-safe truncation** — Chat history truncation now operates on rune boundaries, not bytes
- **XSS defense** — Theme buttons use data attributes + event delegation instead of inline `onclick`
- **CSS `--glass` variable** — Properly defined and applied across all 6 built-in themes
- **`_gwOnlineConfirmed` cleared on fetch failure** — Gateway online state correctly reset when `/api/system` fetch fails

### Changed
- **Layout** — Models, Skills, Git Log panels moved inside Agent Configuration collapsible section
- **GW pill removed** from Top Metrics Bar — gateway status displayed in System Health panel instead

---

## v2026.3.5 — 2026-03-04

### Fixed
- **README panel numbering** — Removed duplicate panel entries; panels now correctly numbered 1–12 with no repeats
- **README test counts** — Updated Architecture comparison table: Python 123 tests (was 14), Go 57 tests (was 39)
- **README config example** — Added `system` block with per-metric thresholds to the `config.json` example
- **README Architecture table** — Added `/api/system` row showing both Python and Go implementations
- **Release assets** — All 4 platform binaries now properly built from source and attached to GitHub release (darwin-arm64, darwin-amd64, linux-amd64, linux-arm64) with SHA256 checksums

---

## v2026.3.4 — 2026-03-04

### Added
- **Top Metrics Status Bar** — New always-on bar at the top of the dashboard showing live CPU, RAM, swap, disk usage, OpenClaw version, and gateway status. Updates every 10 seconds (configurable). Supports both Go binary and Python server backends.
- **`GET /api/system` endpoint** — New endpoint returning JSON with all host metrics, per-metric thresholds, and version info. Uses TTL cache with stale-serving semantics (returns cached data immediately, refreshes in background). Returns `degraded: true` on partial failures, `503` only on full cold-start failure.
- **Per-metric configurable thresholds** — CPU, RAM, swap, and disk each have independent `warn` and `critical` percent thresholds (defaults: 80%/95%). Configurable via `config.json` under `system.cpu`, `system.ram`, `system.swap`, `system.disk`.
- **Cross-platform collectors (Go)** — macOS: `top -l 2` (current delta, not boot average), `vm_stat`, `sysctl vm.swapusage`. Linux: `/proc/stat` dual-sample with steal field, single `/proc/meminfo` read shared between RAM+Swap. Disk via `syscall.Statfs` on both platforms.
- **Python backend parity** — `system_metrics.py` implements identical API shape using stdlib/subprocess only. Uses pre-compiled regexes, atomic refresh flag (`should_start` pattern), HTTP-only gateway probe, per-metric threshold clamping.
- **Dynamic OpenClaw binary resolution** — Both Go and Python backends probe `$HOME/.asdf/shims`, all installed asdf nodejs versions, and common system paths — no hardcoded user paths.
- **Configurable gateway port** — `system.gatewayPort` (synced from `ai.gatewayPort`) used for the gateway liveness HTTP probe in both backends.
- **Parallel collection** — Go backend collects CPU/RAM/Swap/Disk/Versions concurrently via `sync.WaitGroup`, reducing wall-clock time from ~4s to ~1.5s per cycle.
- **Stderr capture in subprocess calls** — `runWithTimeout()` now appends stderr from failed subprocesses to the error message for better diagnostics.
- **15 new Go tests** — Schema, HEAD, CORS, disabled 503, thresholds in response, global+per-metric clamping, cache hit, degraded 200, disk, defaults.
- **13 new Python unit tests** — Parser tests for `parse_top_cpu`, `parse_vm_stat`, `parse_swap_usage_darwin`, `parse_proc_meminfo` covering edge cases.
- **5 new Python integration tests** — `TestSystemEndpoint`: schema, HEAD no body, CORS, content-type, degraded 200.

### Changed
- **Poll interval** — Default `system.pollSeconds` is 10s (previously 5s) to give `top -l 2` comfortable headroom within the TTL.
- **`tests.yml` CI** — Added `test_system_metrics.py` to static analysis step; added `requirements.txt` for pip cache compatibility.

### Technical Details
- `system_types.go` — `SystemResponse`, `SystemCPU`, `SystemRAM`, `SystemSwap`, `SystemDisk`, `SystemVersions`, `SystemGateway`, `SystemThresholds`, `ThresholdPair` structs
- `system_collect_darwin.go` — Pre-compiled regexes (`reTopIdle`, `reVmPageSize`, etc.), `collectCPURAMSwapParallel()`
- `system_collect_linux.go` — `ramFromMeminfo()`, `swapFromMeminfo()` helpers for shared meminfo map, steal field in CPU total
- `system_service.go` — `SystemService` with `sync.RWMutex` cache, `resolveOpenclawBin()`, `detectGatewayFallback()`, configurable port
- `config.go` — `MetricThreshold` struct, `SystemConfig` with per-metric fields, clamping invariants (`0 < warn < critical ≤ 100`)
- `server.go` — `/api/system` route with `system.enabled` gate → 503
- `system_metrics.py` — `_MetricsState`/`_VersionsState` containers (no `globals()` anti-pattern)
- `index.html` — `div#systemTopBar`, `.sys-pill` CSS, `SystemBar` JS object with `??` operators, `Math.max(ms, 2000)` poll guard

---

## v2026.3.3 — 2026-03-03

### Fixed
- **Cache coherence bug** — `getDataRawCached()` now invalidates the parsed data cache when the raw cache is updated. Previously, `/api/refresh` could bump the shared mtime without clearing the parsed cache, causing `/api/chat` to silently use stale dashboard data for its AI context.
- **HEAD requests on `/api/refresh`** — HEAD responses no longer write a body (HTTP spec compliance). Added missing `Content-Length` header.
- **Gateway error status code** — `/api/chat` now returns HTTP 502 (Bad Gateway) instead of 200 when the upstream gateway fails. Clients can now distinguish "AI answered" from "AI is down".
- **`.env` quote stripping** — `readDotenv()` now strips surrounding double and single quotes from values (e.g., `KEY="value"` → `value`).

### Added
- **Graceful shutdown** — Go binary now handles SIGINT/SIGTERM signals and drains in-flight requests (5s timeout) before exiting. Clean container stops, no more orphaned `refresh.sh` processes or `data.json.tmp` files.
- **Gateway response size limit** — `callGateway()` now caps response body at 1MB via `LimitReader`. Prevents memory exhaustion from a misbehaving gateway.
- **Comprehensive Go test suite** — 39 tests with `-race` flag covering:
  - Cache coherence between raw and parsed caches
  - HEAD vs GET behavior for all endpoints
  - Static file allowlist and path traversal defense
  - CORS origin reflection and rejection
  - Chat input validation (empty, too long, too large, invalid JSON)
  - Gateway calls (success, errors, empty responses, oversized responses)
  - System prompt building with empty and populated data
  - Config loading (defaults, overrides, invalid JSON, zero-value clamping)
  - Dotenv parsing (comments, blanks, equals-in-value, quotes, missing file)
  - Version detection (git tag, VERSION file, fallback)

### Changed
- **Agent Bindings UI** — Changed from `flex-wrap` to a symmetric 2-column CSS grid layout. Cards are now equal-width with text overflow ellipsis for long names.

---

## v2026.2.24 — 2026-02-24

### Fixed
- **Accurate model display for sub-agents** — sub-agents now show their actual model (e.g., "GPT 5.3 Codex", "Claude Opus 4.6") instead of defaulting to the parent agent's model (k2p5). Root cause: sub-agents store model in `providerOverride`/`modelOverride` fields, which the dashboard wasn't reading.
- **5-level model resolution priority chain** — Gateway live data → providerOverride/modelOverride → session store `model` field → JSONL `model_change` event → agent default. Ensures the most accurate model is always displayed.

### Added
- **Gateway API query** in `refresh.sh` — queries `openclaw sessions --json` for live session model data as the primary source of truth. Graceful fallback if gateway is unavailable.
- **Model alias resolution** — sub-agent models now display friendly names (e.g., "GPT 5.3 Codex" instead of "openai-codex/gpt-5.3-codex").

## v2026.2.23 — 2026-02-23

### Added
- **AI Chat panel** (`💬`) — floating action button opens a chat panel backed by your OpenClaw gateway
  - Natural language queries about costs, sessions, cron jobs, alerts, and configuration
  - System prompt built from live `data.json` on every request (always up to date)
  - Stateless gateway calls via OpenAI-compatible `/v1/chat/completions` — no agent memory bleed
  - Conversation history with configurable depth (`ai.maxHistory`, default 6 turns)
  - 4 quick-action chips for common questions
  - Dismissible with Escape key or clicking outside
- **`/api/chat` endpoint** in `server.py` — POST `{"question": "...", "history": [...]}` returns `{"answer": "..."}` or `{"error": "..."}`
- **`read_dotenv()`** — parses `~/.openclaw/.env` to load `OPENCLAW_GATEWAY_TOKEN` without requiring env var exports
- **`build_dashboard_prompt()`** — compresses `data.json` into a structured ~300-token system prompt
- **`call_gateway()`** — stateless HTTP call to the OpenClaw gateway with 60s timeout
- New `ai` section in `config.json`: `enabled`, `gatewayPort`, `model`, `maxHistory`, `dotenvPath`
- 14 new tests in `tests/test_chat.py` covering config validation, dotenv parsing, prompt building, gateway error handling, and HTTP endpoint behaviour (AC-CHAT-1 through AC-CHAT-8)
- Converted `test_critical.py` and `test_hierarchy_recent.py` from pytest to stdlib `unittest` — no external test dependencies required

### Prerequisites (one-time setup in `~/.openclaw/openclaw.json`)
The gateway's `chatCompletions` endpoint is disabled by default. Enable it once:
```json
"gateway": {
  "http": { "endpoints": { "chatCompletions": { "enabled": true } } }
}
```
The gateway hot-reloads this change — no restart needed.

### Changed
- Architecture diagram updated to show `/api/chat` endpoint

---

## v2026.2.21 — 2026-02-21

### Fixed
- Handle dict-style model config for agents in refresh script

### Changed
- Ignore `.worktrees/` directory in git

---

## v2026.2.20 — 2026-02-20

### Added
- 6 built-in themes (Midnight, Nord, Catppuccin Mocha, GitHub Light, Solarized Light, Catppuccin Latte)
- Theme switcher in header bar, persisted via `localStorage`
- Custom theme support via `themes.json`
- Sub-agent activity panel with 7d/30d tabs
- Charts & trends panel (cost trend, model breakdown, sub-agent activity) — pure SVG
- Token usage panel with per-model breakdown

### Fixed
- Dynamic channel/binding status for Slack/Discord
