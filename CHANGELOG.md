# Changelog

## v2026.3.8 — Runtime Observability

### Features

- **Runtime observability MVP** — `/api/system` now includes an `openclaw` block with live gateway runtime state sourced from three endpoints: `/healthz` (liveness + uptime), `/readyz` (readiness + failing deps), and `openclaw status --json` (version, latency, security). Both Go (`collectOpenclawRuntime`) and Python (`_collect_openclaw_runtime`) backends collect this data in parallel alongside existing metrics.
- **Gateway Runtime + Config card split** — System Settings now shows two separate cards: *Gateway Runtime* (live `/api/system` data — state, uptime, version, source indicator) and *Gateway Config* (static config snapshot from `data.json` — port, mode, bind, auth). The old combined `gatewayPanel`/`gatewayPanelInner` has been removed.
- **Gateway readiness alerts** — Alert banner synthesizes a `🟡 Gateway not ready: discord` (or any failing dep) alert from the `openclaw.gateway.failing[]` array. Auto-clears when readiness recovers or gateway goes fully offline. Distinct from the "Gateway is offline" alert — both states are mutually exclusive by design.

### Improvements

- **Health pill simplified** — `hGw` now shows `● Online` or `● Offline` (green/red) for the fully-healthy case, or `● Live` (green, no `/ Not Ready`) when live but not ready. Readiness detail is surfaced exclusively through the Alerts banner — no more misleading compound state label in the health row.
- **`_gatewayState()` helper (JS)** — New `SystemBar._gatewayState(d)` function encapsulates the runtime-vs-versions fallback decision. Returns `{source, ok, live, ready}`. Runtime data is trusted when `healthEndpointOk`, `readyEndpointOk`, `uptimeMs>0`, or `failing.length>0`.
- **`_versionsBehind()` robustness** — Strips beta/dev/build suffixes (e.g., `-beta-runtime-observability`, `-dev.1`) before comparing `YYYY.M.D` version triplets. Avoids false "behind" warnings on pre-release installs.
- **`fmtDurationMs()` added to SystemBar** — Converts uptime in ms to human-readable `Ns / Nm / Nh / Nd` string; used by both the Gateway Runtime card and the health row uptime display.
- **`localStorage` key bumped** — `ocDash-v1` → `ocDash-v2` to reset collapse defaults and prevent stale UI state after upgrade.
- **Stale-while-revalidate on `/api/refresh`** (Go) — Response uses JSON round-trip to safely inject `"stale":true` instead of fragile byte-level string replacement that would silently fail on whitespace/ordering differences (B2 fix).
- **Versions collected before parallel phase** (Go) — `getVersionsCached()` now runs before the parallel goroutine group so `collectOpenclawRuntime` always receives real version data instead of an empty struct (B1 fix).

### Bug Fixes

- **Go: `bytes` import removed** — Stale `"bytes"` import from the old byte-level stale injection was cleaned up.
- **Python: `_parse_json_array_fragment` removed** — Dead helper added during development but never called; removed to keep the codebase clean.
- **Gateway status: parse stdout on non-zero exit** — Both Go and Python now attempt to parse `openclaw gateway status --json` stdout even when the command exits non-zero. Many CLIs emit valid JSON to stdout while exiting 1 (e.g., gateway offline but status successfully queried). Falls back to HTTP probe only when stdout has no usable JSON (I2 fix).
- **Thundering herd prevention** (Python) — `_refreshing` flag in `_VersionsState` prevents multiple concurrent calls from spawning redundant collection goroutines when the cache is cold. Returns stale data to waiters while one refresh is in flight.
- **CPU sampling interval** — Increased from 50 ms to 200 ms (Linux dual-`/proc/stat` sample) to reduce noise and give a more representative utilisation window.
- **`fetchJSONMapAllowStatus` for readyz 503** — New helper accepts a set of allowed HTTP status codes so `/readyz` 503 responses (partial readiness) are parsed as valid JSON rather than discarded as errors.

### Breaking Changes

- **`gatewayPanel` / `gatewayPanelInner` removed** — Any external scripts or browser extensions targeting these DOM IDs will break. Use `gatewayRuntimePanelInner` (live data) or `gatewayConfigPanelInner` (config) instead.
- **Top-bar GW pill removed** — `sysGateway` span is no longer present in the HTML. Gateway state is still shown in the System Health row (`hGw`) and the Gateway Runtime card.
- **Install commands changed** — Release assets are raw binaries, not tarballs. The correct download format is `curl -L <url> -o openclaw-dashboard` (no `| tar xz`). See Quick Start.

### Internal

- **`system_types.go`** — Added `SystemOpenclaw`, `SystemOpenclawGateway`, `SystemOpenclawStatus`, `SystemOpenclawFreshness` structs; `SystemResponse.Openclaw` field added.
- **`system_service.go`** — Added `collectOpenclawRuntime()`, `probeOpenclawGatewayEndpoints()`, `parseOpenclawStatusJSON()`, `fetchJSONMapAllowStatus()`, `_versionTriplet()` helpers; added `regexp` import; removed stale `bytes` import.
- **`system_metrics.py`** — Added `_collect_openclaw_runtime()`, `_fetch_json_url_allow_status()`, `_parse_json_object_fragment()`; removed dead `_parse_json_array_fragment()`.
- **`version.go`** — Removed unused `resolveRepoRoot()` helper (dead after embed approach solidified).
- **`version_test.go`** — Removed `TestResolveRepoRoot_Direct` and `TestResolveRepoRoot_DistSubdir` tests (function deleted).
- **`main.go`** — Simplified to `dir := filepath.Dir(exe)` (no longer calls `resolveRepoRoot`).
- **Go dependency bumps** in `go.mod`.
- **56 new frontend tests** in `tests/test_frontend.py` — `TestGatewayPillRemoved`, `TestNoRawPlaceholderTokens`, `TestGatewayRuntimeConfigSplit`, `TestGatewayReadinessAlert`, `_versionsBehind` and `_gatewayState` behavioral tests.
- **37 new Python tests** in `tests/test_system_metrics.py` — `TestOpenclawRuntime`, `TestStaleInjectionSafe`, `TestVersionCollectionI2`, `TestVersionsCacheThunderingHerd`.
- **384 new Go tests** in `system_test.go`.

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
