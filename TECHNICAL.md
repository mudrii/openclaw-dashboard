# TECHNICAL.md — OpenClaw Dashboard Internals

> **Source audit:** 2026-09-07, based on v2026.9.6 plus the documented audit fixes. **Repo:** [github.com/mudrii/openclaw-dashboard](https://github.com/mudrii/openclaw-dashboard)
>
> This document covers architecture, data flow, and implementation details for developers and contributors. For features and quick start, see [README.md](README.md).

---

## Table of Contents

1. [File Structure](#1-file-structure)
2. [Data Pipeline](#2-data-pipeline)
3. [Data Sources](#3-data-sources)
4. [Data Processing Logic](#4-data-processing-logic)
5. [Frontend Architecture](#5-frontend-architecture)
6. [Server Architecture](#6-server-architecture)
7. [Configuration Cascade](#7-configuration-cascade)
8. [data.json Schema](#8-datajson-schema)
9. [Installation & Service Management](#9-installation--service-management)
10. [Dependencies & Requirements](#10-dependencies--requirements)
11. [Security Considerations](#11-security-considerations)
12. [Known Limitations](#12-known-limitations)
13. [Development Guide](#13-development-guide)

---

## 1. File Structure

| Path | Purpose |
|------|---------|
| `cmd/openclaw-dashboard/` | CLI entrypoint |
| `internal/appconfig/` | Config loading and normalization |
| `internal/appopenclaw/` | Selected-runtime CLI access, bounded output, redaction, operation allowlists |
| `internal/appruntime/` | Runtime dir resolution, version detection, Homebrew seeding |
| `internal/appchat/` | Prompt builder and gateway client |
| `internal/apprefresh/` | Dashboard data collector and aggregators |
| `internal/appserver/` | HTTP handlers, refresh coordinator, static serving |
| `internal/appsystem/` | Host metrics and OpenClaw runtime probes |
| `internal/appservice/` | Service lifecycle backend — launchd (macOS), systemd (Linux), unsupported stub |
| `web/index.html` | Embedded single-file frontend |
| `assets/runtime/` | Runtime defaults (`config.json`, `themes.json`, `refresh.sh`) |
| `testdata/` | Reusable fixtures for tests |
| `examples/` | Example configs |
| `docs/CONFIGURATION.md` | Configuration reference |

---

## 2. Data Pipeline

```
Browser                                              Browser
  │                                                    ▲
  │ GET /api/refresh?t=<cache-bust>                    │ JSON response
  ▼                                                    │
openclaw-dashboard ─── debounce check ──► RunRefreshCollector() ──► private temp file
  │           (internal/appserver)          (internal/apprefresh)  │
  │           (30s default)                    │                    │ rename (atomic)
  │           if < 30s:                        │ reads OpenClaw     │
  │           still return cached              │ filesystem         ▼
  │                  (CLI RPCs or legacy files) ▼                data.json
  └──────── read data.json ◄──────────────────────────────────────┘
       (mtime-cached in memory)

Browser                                              OpenClaw Gateway
  │ POST /api/chat {"question","history"}                 ▲
  ▼                                                       │ POST /v1/chat/completions
openclaw-dashboard handleChat()                           │ Bearer token from dotenv
  ├─ load data.json (mtime-cached)                        │
  ├─ buildSystemPrompt(data)                              │
  └─ callGateway(...) ────────────────────────────────────┘
```

### Debounce Mechanism

`internal/appserver` tracks `lastRefresh` on the `Server` struct. `handleRefresh` starts a background refresh only when `time.Since(lastRefresh)` is at least the configured interval **and** no refresh is already running. Until then, responses still serve the current `data.json` (stale-while-revalidate). Default: **30 seconds** (configurable via runtime `config.json` → `refresh.intervalSeconds`).

### Atomic Write

`internal/apprefresh` marshals JSON, creates a unique `data.json.tmp-*` file with mode 0600, writes and syncs it, then renames it to `data.json`. The token-usage cache uses the same publication helper. Unique temporary inodes prevent a concurrent one-shot refresh from modifying a file another writer has already published. Rename within the same directory is atomic on Unix; failed writes preserve the previous destination. This does not claim directory-entry durability across power loss.

### Concurrency

`Server.mu` (`sync.Mutex`) in `internal/appserver` coordinates `lastRefresh`, `refreshRunning`, and overlapping work within one server. Separate processes can collect concurrently and publish complete snapshots in completion order. CLI calls have time/output bounds; background refresh and metrics workers use the server lifecycle context.

---

## 3. Data Sources

The refresh collector chooses runtime RPCs when a target is explicitly selected or migrated SQLite state is detected. It never opens private SQLite tables. See [runtime compatibility](docs/RUNTIME-COMPATIBILITY.md) for the complete source/capability map. Core modern sources are:

| Source | Projection |
|--------|------------|
| `sessions.list` | Paginated session rows, active-run identities, reported stored total |
| `sessions.usage` | All-agent totals, per-model usage, daily aggregates, cache/pricing completeness |
| `tasks.list`, `cron.list`, `cron.runs` | Durable tasks, automation definitions and run history |
| `status`, `system.info`, `channels.status` | Selected gateway health, runtime information and accounts |
| `config.get` (container), local `openclaw.json` (native) | Agent/model/channel configuration |
| `skills.status`, `doctor.memory.status`, inventory CLI calls | Runtime inventories and readiness |

Runtime selection is carried through context into `appopenclaw.CommandContext`; explicit native mode suppresses inherited container routing. Container collection never substitutes host state on failure. Each collection records `source`, `state`, `complete`, `collectedAt`, `attemptedAt`, and an optional `errorCode`. Same-target data can be retained as `stale` for at most 24 hours; consumers must display that state.

For unmigrated native state, legacy collection uses the following files/commands under the OpenClaw directory (default `~/.openclaw`):

| Source Path | What It Provides |
|-------------|-----------------|
| `openclaw.json` | Bot config: models, skills, compaction mode |
| `agents/*/sessions/sessions.json` | Session metadata (keys, tokens, context, model, timestamps) |
| `agents/*/sessions/*.jsonl` + `.jsonl.deleted.*` | Per-message token usage and cost data |
| `openclaw cron list --json` (gateway) | Cron definitions, schedules, run status, **delivery status, consecutive errors/skips** (flapping). OpenClaw 2026.6 moved cron into shared SQLite; falls back to legacy `cron/jobs.json` + `cron/jobs-state.json` on pre-migration installs |
| `openclaw tasks list --json --runtime subagent` (gateway) | Sub-agent runs: agent, task, status, duration, timestamp (no per-run cost/tokens — not exposed by the tasks store) |
| `.git/` (via `git log`) | Last 5 commits (hash, message, relative time) |
| Gateway lock file + process table | Gateway PID, uptime, RSS memory |
| `journalctl --user` (Linux) | Gateway logs when systemd routes them to journald (no file to tail) |
| `openclaw models list --json` | Live model display names + context windows (TTL-cached) |

### Gateway Detection

For native targets, process metadata (PID/uptime/RSS) is read first from openclaw's install-independent
lock file (`<tmpdir>/openclaw-<uid>/gateway.<sha256(configPath)[:8]>.lock`, payload
`{pid, createdAt, …}`), which is correct on every install layout. When no usable lock
exists, the dashboard falls back to:

```bash
pgrep -f "openclaw/dist/index.js gateway"   # npm layout
```

If a PID is found (lock or pgrep), a follow-up `ps -p <pid> -o rss=` (or `etime=,rss=`
on the pgrep path) extracts RSS memory; the lock path derives uptime from `createdAt`.
The HTTP `/healthz` probe remains the authoritative liveness signal.
Container selection suppresses host PID and loopback health probes; selected-runtime RPCs supply runtime facts and unavailable probes retain an explicit reason.

### Linux journald Log Fallback

On systemd hosts the gateway's systemd `--user` unit emits no `StandardOutput=`
directive, so gateway output goes to journald and there is no file to tail. When a
configured log source has no file on disk, the collector synthesizes log records from
`journalctl --user -u <unit>.service -o json --no-pager` (mapping `PRIORITY`→severity,
`MESSAGE`→message, `__REALTIME_TIMESTAMP`→time). The unit resolves from
`OPENCLAW_SYSTEMD_UNIT` > `logs.systemdUnit` > `openclaw-gateway` (with an
`OPENCLAW_PROFILE` suffix). Gated on `runtime.GOOS == "linux"`; macOS is unaffected.

### Runtime Observability (`/api/system` — `openclaw` block)

In addition to the `data.json` pipeline, `/api/system` includes an `openclaw` block. Native collection uses these sources; container mode skips the host HTTP probes and executes status against the selected container:

| Source | Data Collected |
|--------|---------------|
| `GET /healthz` | `live`, `uptimeMs`, `healthEndpointOk` |
| `GET /readyz` | `ready`, `failing[]`, `readyEndpointOk` |
| `openclaw status --json` | `currentVersion`/`runtimeVersion`, `latestVersion`, `connectLatencyMs`, `tasks`, `pluginCompatibility`, `channelSummary`, plus additive runtime blocks such as `gateway`, `gatewayService`, `nodeService`, `memory`, `memoryPlugin`, `os`, `sessions`, `secretDiagnostics`, `taskAudit`, `update`, and queued system events |

The `readyz` endpoint returns a `503` body with JSON when some dependencies are failing. `fetchJSONMapAllowStatus` accepts configurable HTTP status codes so the body is parsed rather than discarded.

The rich `status --json` blocks (`tasks`, `eventLoop`, `pluginCompatibility`,
`lastHeartbeat`, `channelSummary`) feed the **Runtime Health** panel. Additional
OpenClaw runtime blocks are passed through additively so newer status payloads
remain visible without breaking older dashboards. Minimal status still parses,
and absent blocks are omitted. `eventLoop` and `lastHeartbeat` are deep-status-only:
set `system.deepStatus=true` to invoke `openclaw status --json --deep` (slower).
Lean status already provides the task queue, plugin-compatibility warnings, and
channel summary.

**Channel health (apprefresh `/readyz`).** Separately from the appsystem block above,
the refresh collector has its own `/readyz` probe whose `failing[]` drives per-channel
health: a channel whose config key is in `failing[]` is marked
`connected=false, health="unhealthy"` (overriding the session-activity heuristic);
non-channel reasons are ignored and probe failure falls back to the heuristic.

The frontend's `SystemBar._gatewayState(d)` helper decides whether to trust the runtime data or fall back to the `versions.gateway` status field from `data.json`. Runtime is trusted when any of these signals is present: `healthEndpointOk`, `readyEndpointOk`, `uptimeMs > 0`, or `failing.length > 0`.

**Gateway Readiness Alert flow:**
1. `SystemBar.render()` checks `gwLive && !gwReady && gwState.source === 'runtime'`
2. Builds alert message from `gwRuntime.failing[]` (e.g., `"Gateway not ready: discord, slack"`)
3. Inserts/updates `<div id="gw-readiness-alert" class="alert-item alert-medium">` at the top of `#alertsSection`
4. Removes the alert when gateway recovers (`ready=true`) or goes offline (`live=false`)

---

## 4. Data Processing Logic

All aggregation and collector processing now runs under `internal/apprefresh/`.

### Model Name Normalization

The `ModelName()` function maps raw provider/model IDs (e.g., `anthropic/claude-opus-4-6`) to friendly display names (e.g., `Claude Opus 4.6`). It strips the provider prefix and matches against known substrings:

| Pattern | Display Name |
|---------|-------------|
| `opus-4-6` | Claude Opus 4.6 |
| `opus` | Claude Opus (version retained when present) |
| `sonnet` | Claude Sonnet |
| `haiku` | Claude Haiku |
| `grok-4-fast` | Grok 4 Fast |
| `gemini-2.5-pro` | Gemini 2.5 Pro |
| `minimax-m2.5` | MiniMax M2.5 |
| `k2p5` | Kimi K2.5 |
| other `kimi` IDs | Kimi |
| `gpt-5.3-codex` | GPT-5.3 Codex |
| *(unknown id)* | Live catalog name, else raw model string |

**Live model catalog.** A TTL-cached snapshot of `openclaw models list --json`
(`model_catalog_cache.go`, mirroring the session-model cache) supplies display names
and context windows for current/future models. A meaningful catalog name wins;
a name equal to the bare model ID falls through to the curated rules. `lookupModelLimits`
also consults the catalog's context window when the `openclaw.json` model registry has
no entry for a model.

### Session Type Detection

Legacy session keys are classified by substring matching. Modern rows preserve the runtime's `kind` and `hasActiveRun` fields:

| Key Pattern | Type |
|-------------|------|
| `cron:` | `cron` |
| `subagent:` | `subagent` |
| `group:` | `group` |
| `telegram` | `telegram` |
| ends with `:main` | `main` |
| *(other)* | `other` |

The legacy collector skips keys containing `:run:`. Modern pagination preserves the runtime listing, including its total count and completeness state.

### Token Aggregation

Modern usage comes from four `sessions.usage` queries: all-time, today, seven calendar days, and thirty calendar days, using the configured timezone and all-agent scope. A fresh upstream cache with no pending/stale files establishes token completeness. Complete costs additionally require a reported total and no missing price entries. Unknown totals remain null while known token/subtotal data remains available. Models sharing a chart display name have their costs summed.

For legacy `.jsonl` files, the collector reads assistant usage records and aggregates into eight `map[string]*TokenBucket` buckets. Parsed per-file summaries are persisted in `.token-usage-cache.json` and reused when size and mtime match:

- **`models_all`** — all-time per-model totals
- **`models_today`** — today-only per-model totals (compared against `todayStr` in the configured timezone)
- **`models_7d`** — last 7 days per-model totals
- **`models_30d`** — last 30 days per-model totals
- **`subagent_all`** — all-time subagent-only totals
- **`subagent_today`** — today subagent-only totals
- **`subagent_7d`** — last 7 days subagent-only totals
- **`subagent_30d`** — last 30 days subagent-only totals

Each bucket tracks: `calls`, `input`, `output`, `cacheRead`, `totalTokens`, `cost`.

Messages from `delivery-mirror` models are excluded.

### Cost Calculation

Legacy cost comes from `message.usage.cost.total` in JSONL assistant messages. Modern cost comes from runtime aggregates; it is never reconstructed from the limited session page or the frozen legacy chart backstop.

### Alert Generation

Alerts are generated based on configurable thresholds:

| Alert | Threshold (default) | Severity |
|-------|---------------------|----------|
| Daily cost high | `alerts.dailyCostHigh` (50) | `high` |
| Daily cost warn | `alerts.dailyCostWarn` (20) | `medium` |
| High context usage | `alerts.contextPct` (80%) | `medium` |
| High memory | `alerts.memoryMb` (640) × 1024 KB | `medium` |
| Gateway offline | *(always checked)* | `critical` |
| Cron job failed | `lastStatus === 'error'` | `high` |

### Projected Monthly Cost

```go
projectedMonthly := totalCostToday * 30
```

Modern projection is null when the daily cost is incomplete or unreported.

---

## 5. Frontend Architecture

### Technology

Pure vanilla HTML/CSS/JS. No frameworks, no build step, no external dependencies.

### CSS Design System

CSS custom properties defined in `:root`:

```css
--bg: #0a0a0f          /* Page background */
--surface: rgba(255,255,255,0.03)  /* Glass card fill */
--border: rgba(255,255,255,0.06)   /* Glass card border */
--accent: #6366f1       /* Primary accent (indigo) */
--accent2: #9333ea      /* Secondary accent (purple) */
--green: #4ade80        /* Status: online/ok */
--yellow: #facc15       /* Status: warning */
--red: #f87171          /* Status: error/critical */
--text: #e5e5e5         /* Primary text */
--muted: #737373        /* Secondary text */
--dim: #525252          /* Tertiary text */
```

Glass morphism: `.glass` class applies semi-transparent background + subtle border, with hover brightening.

### Layout Grid

| Grid Class | Columns | Usage |
|------------|---------|-------|
| `.health-row` | `repeat(6, 1fr)` | System health metrics bar |
| `.cost-row` | `1fr 1fr 1fr 2fr` | Cost cards + donut chart |
| `.grid-2` | `1fr 1fr` | Two-column sections |
| `.grid-3` | `1fr 1fr 1fr` | Bottom row (models, skills, git) |

### Responsive Breakpoints

| Breakpoint | Changes |
|------------|---------|
| `≤ 1024px` | Cost row → 2-col; Health row → 3-col |
| `≤ 768px` | Grid-2, grid-3 → 1-col; Cost/health → 2-col |

### Data Flow

```
DataLayer.fetch()
  → fetch('/api/refresh?t=' + Date.now())
  → parse JSON → store in State.data (frozen snapshot)
  → DirtyChecker.diff(current, prev)
      → compares section data and collection state, ignoring volatile attempt timestamps
  → Renderer.render(snapshot, dirtyFlags)
      → renderHeader (bot name, emoji, gateway status)
      → renderAlerts
      → renderHealthRow (gateway, PID, uptime, memory, compaction, sessions)
      → renderCostCards + donut chart
      → renderCronTable
      → renderSessionsTable
      → renderTokenUsage (tabbed: today/7d/30d/all-time)
      → renderSubagentActivity (tabbed: today/7d/30d/all-time)
      → renderSubagentTokens (tabbed: today/7d/30d/all-time)
      → renderModels, Skills, GitLog
```

### Auto-Refresh

`setInterval` runs every 1 second, decrementing a `timer` from 60. At zero, `loadData()` fires and timer resets. The countdown is displayed in the header. Manual refresh via the "↻ Refresh" button calls `loadData()` directly.

### Donut Chart

Pure CSS `conic-gradient` on a circular div. The gradient segments are computed from `costBreakdown` percentages:

```javascript
donut.style.background = `conic-gradient(#6366f1 0% 45%, #9333ea 45% 70%, ...)`;
```

A centered `.donut-hole` div (55% size, page background color) creates the hole effect.

### Tab State

Three tab variables within the `State` module control today/7d/30d/all-time views: `State.tabs.uTab` (token usage), `State.tabs.srTab` (subagent runs), `State.tabs.stTab` (subagent tokens). Tab buttons call `State.setTab()` which updates the tab value and triggers `App.renderNow()` to re-render with the current tab state.

The tab switching pattern uses `State.setTab(prefix, tab)` which updates the internal tab variable and invokes the render cycle. Tab button CSS classes are managed by the `Renderer` during each render pass.

### Charts & Trends

Two visible pure SVG charts render in the charts section, controlled by a `chartDays` variable (7 or 30). The sub-agent chart implementation is present but hidden until task-store cost/token data is available again:

| Chart | Function | Visualization |
|-------|----------|--------------|
| **Daily Cost Trend** | `renderCostChart()` | Line chart with area fill — plots `dailyChart[].total` |
| **Cost by Model** | `renderModelChart()` | Stacked bar chart — breaks down daily cost by top 6 models + "Other" |
| **Sub-Agent Activity** | `renderSubChart()` | Hidden for now; task-store runs expose status/duration but not cost/token aggregates |
| **Sub-Agent Activity** | `renderSubagentChart()` | Dual-axis: bars for run count (left axis), line for cost (right axis) |

All charts are generated as inline `<svg>` elements with `viewBox="0 0 400 300"`. No external charting library. Data comes from the `dailyChart` array in `data.json`. Chart toggle buttons (`cTab7` / `cTab30`) call `renderCharts()` directly.

### Theme Engine

The theme system loads themes from `themes.json` at startup and applies them by setting 19 CSS custom properties on `document.documentElement`:

| Method | Purpose |
|--------|---------|
| `Theme.load()` | Fetches `themes.json`, restores saved theme from `localStorage('ocDashTheme')`, calls `Theme.apply()` |
| `Theme.apply(id)` | Sets all 19 `--*` CSS variables from the theme's `colors` object, saves to `localStorage` |
| `Theme.renderMenu()` | Builds the dropdown menu, grouping themes by `type` (`dark` / `light`) |
| `Theme.toggleMenu()` | Toggles `.open` class on `#themeMenu` |

The 19 CSS variables controlled by themes: `bg`, `surface`, `surfaceHover`, `border`, `accent`, `accent2`, `green`, `yellow`, `red`, `orange`, `purple`, `text`, `textStrong`, `muted`, `dim`, `darker`, `tableBg`, `tableHover`, `scrollThumb`.

Theme state is stored within the `Theme` module object (theme definitions and active theme ID). Clicking outside the theme picker closes the menu via a `document.addEventListener('click', ...)` handler.

---

## 6. Server Architecture

The `openclaw-dashboard` binary embeds `web/index.html` via `//go:embed` and implements the HTTP API.

### Go Server (`openclaw-dashboard` binary)

- Single binary with static assets embedded from `web/` and runtime defaults loaded from the resolved dashboard directory with fallback to `assets/runtime/`
- Concurrent request handling (Go's `net/http` goroutine-per-request model)
- Read routes (`GET|HEAD`): `/`, `/index.html`, `/api/refresh`, `/api/system`, `/api/logs`, `/api/errors`, `/api/automation/runs`, `/api/session/context`, `/api/workboard`, `/api/chat/status`, `/api/operations/status`, and allowlisted static assets (`/themes.json`, `/favicon.ico`, `/favicon.png`). Write routes (`POST`): `/api/chat`, `/api/operations`.
- All other paths return 404; non-GET/HEAD/POST (except `OPTIONS`) returns 405
- **Graceful shutdown**: handles SIGINT/SIGTERM, drains in-flight requests (5s timeout)
- **Pre-warm**: runs `runRefresh()` once in the background at startup so the first browser hit is fast
- **Coherent mtime cache**: `cachedDataRaw` and `cachedData` are populated together under a `sync.RWMutex`; readers receive an immutable raw slice or a shallow top-level map copy. Strict JSON v2 decoding rejects corrupt snapshots.
- **Allowlisted static files**: only configured paths are served from disk; arbitrary path traversal is rejected
- **Gateway response limit**: caps upstream response at 1MB

### `/api/refresh` Endpoint

1. `handleRefresh` applies debounce and may start a refresh goroutine (calls `RunRefreshCollector` in `internal/apprefresh`)
2. Returns cached or disk `data.json` with headers:
   - `Content-Type: application/json`
   - `Cache-Control: no-cache`
   - `Content-Length: <size>`
   - `Access-Control-Allow-Origin: <origin>` when origin is `http://localhost:*` or `http://127.0.0.1:*`
   - fallback CORS origin: `http://localhost:<configured-port>`
3. Stale-while-revalidate: returns existing data immediately while a stale refresh runs in the background
4. On error: returns 503 (no `data.json`) or 500 (other)

### `/api/chat` Endpoint

1. Checks `ai.enabled` from `config.json`
   - Rejects foreign/opaque browser origins before gateway work (HTTP 403). HTTP/HTTPS origins matching the request Host, HTTP loopback development origins, and origin-less CLI clients are allowed. TLS proxies must preserve the public Host.
   - Enforces 10 requests/minute per client IP and checks selected-runtime chat endpoint configuration and credential availability. This check does not perform inference or verify authentication.
2. Validates JSON body (64KB limit) and non-empty `question` (2000 char limit)
3. Sanitises `history`: only `user`/`assistant` roles, truncates content to 4000 chars, caps at `ai.maxHistory` entries
4. Loads `data.json` (mtime-cached) and builds a compact system prompt
5. Calls OpenClaw gateway endpoint:
   - `POST http://localhost:<ai.gatewayPort>/v1/chat/completions`
   - headers: `Authorization: Bearer <OPENCLAW_GATEWAY_TOKEN>`
   - Response capped at 1MB
6. Returns:
   - HTTP 200 `{"answer":"..."}` on success
   - HTTP 400 for bad input, 403 for rejected origins, 413 for oversized body, 429 when rate-limited, 502/504 for gateway failure/timeout, 503 for disabled or unconfigured chat, and 500 for invalid dashboard data

`POST /api/operations` is separately opt-in, loopback-only and bearer-authorized. It accepts only automation enable/disable/run and abort of an exact active run. Fresh identity/revision checks, unique request IDs, durable audit reservations and outcome reporting constrain mutations. See [configuration](docs/CONFIGURATION.md#runtime-compatibility-and-optional-operations) for credentials, replay-retention limits and uncertain outcomes.

### Quiet Logging

Uses structured logging for `/api/refresh`, `/api/chat`, and error paths. Static file requests are not logged.

The server also exposes log and error feed endpoints. `/api/logs` returns merged tail output from the configured log sources, and `/api/errors` groups warning/error signatures over a rolling time window for the dashboard error feed.

### LAN Mode

Non-loopback binds require `OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK=1`. When bound to `0.0.0.0`, the server prints the detected local IP. Read endpoints have no application-level authentication; use trusted network access controls.

---

## 7. Configuration Cascade

Each setting resolves through a priority chain (highest wins):

| Setting | CLI Flag | Env Var | config.json Path | Default |
|---------|----------|---------|-------------------|---------|
| Bind address | `--bind` / `-b` | `DASHBOARD_BIND` | `server.host` | `127.0.0.1` |
| Port | `--port` / `-p` | `DASHBOARD_PORT` | `server.port` | `8080` |
| Debounce interval | — | — | `refresh.intervalSeconds` | `30` |
| OpenClaw path (refresh) | — | `OPENCLAW_HOME` | *(not read by runtime)* | `~/.openclaw` |
| AI chat enabled | — | — | `ai.enabled` | `true` |
| Gateway port | — | — | `ai.gatewayPort` | `18789` |
| Chat model | — | — | `ai.model` | `""` |
| Max history (server cap) | — | — | `ai.maxHistory` | `6` |
| Dotenv path for gateway token | — | — | `ai.dotenvPath` | `"~/.openclaw/.env"` |
| Bot name | — | — | `bot.name` | `OpenClaw Dashboard` |
| Bot emoji | — | — | `bot.emoji` | `🦞` (`⚡` fallback if configured empty) |
| Daily cost high | — | — | `alerts.dailyCostHigh` | `50` |
| Daily cost warn | — | — | `alerts.dailyCostWarn` | `20` |
| Context % threshold | — | — | `alerts.contextPct` | `80` |
| Memory threshold | — | — | `alerts.memoryMb` | `640` |

**Implementation detail:** Bind/port use CLI > environment > config > defaults. Runtime selection comes from `openclaw` configuration plus inherited container selection; `OPENCLAW_STATE_DIR` overrides profile-derived local paths. Gateway credentials resolve from the process environment, dotenv, then supported literal/env SecretRef config values. The collector does not read `config.openclawPath`. See [configuration](docs/CONFIGURATION.md) for the full environment and selection contract.

---

## 8. data.json Schema

The payload has `schemaVersion: 2`. The tables below describe shared fields and mark legacy-only behavior. Modern additions include `timezone`, `runtimeTarget`, `stateDir`, `collections`, `tasks`, `usageAll/Today/7d/30d`, `runtimeHealth`, `runtimeInfo`, `diagnostics`, `channels`, `memory`, `memoryIndex`, `skillInventory`, `pluginInventory`, and `modelReadiness`. Collection state and nullability are part of the wire contract; an unavailable measurement must not become zero. See the [runtime source map](docs/RUNTIME-COMPATIBILITY.md#data-sources-and-limits) and `internal/apprefresh/refresh.go` for assembly.

### Top-Level Fields

| Key | Type | Description |
|-----|------|-------------|
| `botName` | `string` | Display name from config (`"OpenClaw Dashboard"`) |
| `botEmoji` | `string` | Emoji from config (`"🦞"`) |
| `lastRefresh` | `string` | Human-readable timestamp (`"2026-02-16 13:45:00 UTC"`) |
| `lastRefreshMs` | `number` | Unix epoch milliseconds |

### Gateway (data.json — config/status from refresh collector)

| Key | Type | Description |
|-----|------|-------------|
| `gateway.status` | `"online" \| "offline" \| "unknown"` | Native health probe result; unknown when a host probe is inapplicable |
| `gateway.pid` | `number \| null` | Process ID |
| `gateway.uptime` | `string` | Elapsed time from `ps` (e.g., `"3-02:15:30"`) |
| `gateway.memory` | `string` | Formatted RSS (e.g., `"245 MB"`) |
| `gateway.rss` | `number` | Raw RSS in KB |

> Note: The dashboard UI shows two separate cards in System Settings — **Gateway Runtime** (populated from live `/api/system` data by `SystemBar.render()`) and **Gateway Config** (populated from `data.json`'s `agentConfig.gateway` by `Renderer.render()`). The `gatewayPanel`/`gatewayPanelInner` element from earlier versions has been removed; use `gatewayRuntimePanelInner` or `gatewayConfigPanelInner` instead.

### Cost Fields

| Key | Type | Description |
|-----|------|-------------|
| `compactionMode` | `string` | From openclaw.json (`"auto"`, `"manual"`, etc.) |
| `totalCostToday` | `number \| null` | Complete daily total, or null when incomplete/unreported |
| `totalCostAllTime` | `number \| null` | Complete all-time total, or null |
| `projectedMonthly` | `number \| null` | Complete `totalCostToday × 30`, or null |
| `costBreakdown` | `array \| null` | Complete all-time cost per model: `[{model, cost}]`, or null |
| `costBreakdownToday` | `array \| null` | Complete daily cost per model, or null |

### Sessions

| Key | Type | Description |
|-----|------|-------------|
| `sessions` | `array` | Modern paginated rows (up to 1,000); legacy top 20 recent sessions |
| `sessions[].name` | `string` | Display name; modern rows truncate to 100 Unicode code points |
| `sessions[].key` | `string` | Session key (e.g., `"telegram:group:-123:main"`) |
| `sessions[].agent` | `string` | Agent name (directory name) |
| `sessions[].model` | `string` | Friendly display name; modern `modelId` preserves the identifier |
| `sessions[].contextPct` | `number \| null` | Context usage (0-100) when fresh tokens and a limit are reported |
| `sessions[].lastActivity` | `string` | Time string (`"HH:MM:SS"`) |
| `sessions[].updatedAt` | `number` | Unix epoch milliseconds |
| `sessions[].totalTokens` | `number \| null` | Fresh valid token count; `tokenState` and `contextState` explain gaps |
| `sessions[].type` | `string` | `"cron"`, `"subagent"`, `"group"`, `"telegram"`, `"main"`, `"other"` |
| `sessionCount` / `sessionTotal` | `number` | Runtime-reported stored total (at least loaded rows); legacy known session IDs |

### Cron Jobs

| Key | Type | Description |
|-----|------|-------------|
| `crons` | `array` | All cron job definitions |
| `crons[].id` | `string` | Job id (uuid) |
| `crons[].agentId` | `string` | Owning agent id |
| `crons[].name` | `string` | Job name |
| `crons[].schedule` | `string` | Human-readable schedule (`"Every 6h"`, cron expr, etc.) |
| `crons[].enabled` | `boolean` | Whether the job is active |
| `crons[].lastRun` | `string` | Formatted timestamp or `""` |
| `crons[].lastStatus` | `string` | `"ok"`, `"error"`, `"none"` |
| `crons[].lastDurationMs` | `number \| null` | Last duration in ms; absent differs from an explicit zero |
| `crons[].nextRun` | `string` | Formatted next run timestamp or `""` |
| `crons[].model` | `string` | Model from job payload |
| `crons[].lastDeliveryStatus` | `string` | Last delivery outcome from sidecar state |
| `crons[].lastDiagnostics` | `array` | Diagnostic strings for the last run/delivery |
| `crons[].consecutiveErrors` | `number` | Consecutive failing runs |
| `crons[].consecutiveSkipped` | `number` | Consecutive skipped deliveries/runs |
| `crons[].flapping` | `boolean` | True when consecutive errors cross the flapping threshold |

### Sub-Agent Activity

| Key | Type | Description |
|-----|------|-------------|
| `subagentRuns` | `array` | Last 30 sub-agent runs (all time) |
| `subagentRunsToday` | `array` | Last 20 sub-agent runs (today) |
| `subagentRuns7d` | `array` | Last 50 sub-agent runs (7 days) |
| `subagentRuns30d` | `array` | Last 100 sub-agent runs (30 days) |
| `subagentRuns[].task` | `string` | Task description (whitespace-collapsed, truncated to 80 chars) |
| `subagentRuns[].agent` | `string` | Owning agent id (e.g. `main`) |
| `subagentRuns[].durationSec` | `number \| null` | Ended − started/created; null for modern unfinished tasks |
| `subagentRuns[].status` | `string` | Task status: `succeeded`, `failed`, `running`, `queued`, `cancelled`, `timed_out`, `lost` |
| `subagentRuns[].error` | `string` | Failure reason when the run did not succeed (empty otherwise) |
| `subagentRuns[].timestamp` | `string` | `"YYYY-MM-DD HH:MM"` (from createdAt) |
| `subagentRuns[].date` | `string` | `"YYYY-MM-DD"` (windowing key) |
| `subagentCostAllTime` / `subagentCostToday` / `subagentCost7d` / `subagentCost30d` | `number \| null` | Null in modern mode because tasks do not report cost; legacy compatibility values remain |

### Token Usage

Applies to `tokenUsage`, `tokenUsageToday`, `tokenUsage7d`, `tokenUsage30d`, `subagentUsage`, `subagentUsageToday`, `subagentUsage7d`, `subagentUsage30d`:

| Key | Type | Description |
|-----|------|-------------|
| `[].model` | `string` | Friendly model name |
| `[].calls` | `number` | Number of assistant messages |
| `[].input` | `string` | Formatted input tokens (`"1.2M"`) |
| `[].output` | `string` | Formatted output tokens |
| `[].cacheRead` | `string` | Formatted cache read tokens |
| `[].totalTokens` | `string` | Formatted total tokens |
| `[].cost` | `number \| null` | Complete cost (2 decimal places), or null; modern rows also expose `knownCost`, `costComplete`, `tokensComplete` and `missingCostEntries` |
| `[].inputRaw` | `number` | Raw input token count |
| `[].outputRaw` | `number` | Raw output token count |
| `[].cacheReadRaw` | `number` | Raw cache read token count |
| `[].totalTokensRaw` | `number` | Raw total token count |

Legacy rows are sorted by cost; modern rows preserve the upstream model aggregate order. Modern rows also preserve `modelId`, `cacheWrite` and `cacheWriteRaw`.

### Models & Skills

| Key | Type | Description |
|-----|------|-------------|
| `availableModels[].provider` | `string` | Provider name (title-cased) |
| `availableModels[].name` | `string` | Model alias or ID |
| `availableModels[].id` | `string` | Full model ID |
| `availableModels[].status` | `string` | `"active"` (primary) or `"available"` |
| `skills[].name` | `string` | Skill name |
| `skills[].active` | `boolean` | Whether enabled |
| `skills[].type` | `string` | Always `"builtin"` |

### Git Log

| Key | Type | Description |
|-----|------|-------------|
| `gitLog[].hash` | `string` | Short commit hash |
| `gitLog[].message` | `string` | Commit message subject |
| `gitLog[].ago` | `string` | Relative time (`"2 hours ago"`) |

### Daily Chart (Charts & Trends)

| Key | Type | Description |
|-----|------|-------------|
| `dailyChart` | `array` | Last 30 days of daily aggregated data |
| `dailyChart[].date` | `string` | `"YYYY-MM-DD"` |
| `dailyChart[].label` | `string` | `"MM-DD"` (for chart X-axis labels) |
| `dailyChart[].total` | `number \| null` | Complete cost for the day, or null |
| `dailyChart[].tokens` | `number \| null` | Reported tokens; null for unreported days during incomplete collection |
| `dailyChart[].calls` | `number \| null` | Reported messages/calls; null when unreported |
| `dailyChart[].subagentCost` | `number` | Sub-agent cost for the day |
| `dailyChart[].subagentRuns` | `number` | Sub-agent run count for the day |
| `dailyChart[].models` | `object` | Cost by display name; shared names are summed. Legacy charts use top 6 + "Other". Modern incomplete pricing leaves this empty. |

Modern charts omit the legacy daily `subagentCost` and `subagentRuns` fields. Modern `skillInventory` takes precedence over the explicit legacy `skills` overrides in the UI.

### Alerts

| Key | Type | Description |
|-----|------|-------------|
| `alerts[].type` | `string` | `"warning"`, `"error"`, `"info"` |
| `alerts[].icon` | `string` | Emoji icon |
| `alerts[].message` | `string` | Human-readable alert text |
| `alerts[].severity` | `string` | `"critical"`, `"high"`, `"medium"`, `"low"` |

---

## 9. Installation & Service Management

### Binary Service Subcommands

The binary includes built-in service management via the `install`, `uninstall`, `start`, `stop`, `restart`, and `status` subcommands (backed by `internal/appservice/`). All commands are available directly and via the `service` namespace alias:

```bash
openclaw-dashboard install [--bind HOST] [--port PORT]
openclaw-dashboard status
openclaw-dashboard stop
openclaw-dashboard start
openclaw-dashboard restart
openclaw-dashboard uninstall
# or: openclaw-dashboard service <cmd>
```

`install` bakes `--bind` and `--port` (defaulting to values from `config.json` and env vars) into the generated plist / unit file. If `OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK=1` is required for the chosen bind host, that override is persisted into the service environment as well. `openclaw-dashboard uninstall` preserves config and data; `uninstall.sh` removes the runtime directory after deregistering the service.

**Implementation:**
- Platform selection: Go build tags (`//go:build darwin`, `//go:build linux`, `//go:build !darwin && !linux`)
- macOS backend (`launchd.go`): writes plist to `~/Library/LaunchAgents/com.openclaw.dashboard.plist`, invokes `launchctl load/unload/start/stop/list`
- Linux backend (`systemd.go`): writes unit to `~/.config/systemd/user/openclaw-dashboard.service`, invokes `systemctl --user daemon-reload/enable/start/stop/disable/restart/show` and `journalctl`
- All external commands injected via `runCmdFunc` field for testability (no mocking frameworks)
- HTTP liveness probe (`probe.go`, package-level `http.Client`, 2s timeout) — `Status()` sets `Running=true` only when both PID > 0 AND HTTP probe succeeds
- Homebrew/Nix package-share runtime seeding preserves existing `config.json` and `themes.json`, while syncing the package-managed `VERSION` file on startup so the reported version matches the installed package

**Status output format:**
```
openclaw-dashboard v2026.4.8
Status:     running
PID:        48291
Uptime:     3h 12m
Port:       8080
Auto-start: enabled (LaunchAgent)

--- recent log ---
[dashboard] v2026.4.8
[dashboard] Serving on http://127.0.0.1:8080/
```

### macOS — LaunchAgent

`openclaw-dashboard install` writes a plist at `~/Library/LaunchAgents/com.openclaw.dashboard.plist`:

- **RunAtLoad:** `true` — starts on login
- **KeepAlive:** `true` — restarts on crash
- **WorkingDirectory:** install dir
- **Logs:** `<install_dir>/server.log`

Commands:
```bash
launchctl load ~/Library/LaunchAgents/com.openclaw.dashboard.plist
launchctl unload ~/Library/LaunchAgents/com.openclaw.dashboard.plist
```

> Note: these commands are now handled automatically by `openclaw-dashboard install` / `openclaw-dashboard uninstall`.

### Linux — systemd User Service

`openclaw-dashboard install` writes `~/.config/systemd/user/openclaw-dashboard.service`:

- **Restart:** `always` (5s delay)
- **WantedBy:** `default.target`

Commands:
```bash
systemctl --user start openclaw-dashboard
systemctl --user stop openclaw-dashboard
systemctl --user status openclaw-dashboard
```

> Note: these commands are now handled automatically by `openclaw-dashboard install` / `openclaw-dashboard uninstall`.

### Install Flow

1. Check prerequisites (OpenClaw directory at `OPENCLAW_HOME` or `~/.openclaw`)
2. Create `${OPENCLAW_HOME:-~/.openclaw}/dashboard`
3. Download the latest release archive for the current OS/arch, or fall back to a tag/main source archive + `go build -trimpath`
4. Seed `refresh.sh` and copy `assets/runtime/config.json` to `config.json` if missing
5. Run initial data generation: `./openclaw-dashboard --refresh`
6. Register and start the OS-specific service via `./openclaw-dashboard install`
7. Print URLs and local file paths

### Uninstall Flow

1. `openclaw-dashboard uninstall` stops and removes the service but preserves runtime files
2. `uninstall.sh` additionally removes `${OPENCLAW_HOME:-~/.openclaw}/dashboard`
3. Fallback cleanup removes any remaining plist / user unit and matching processes

---

## 10. Dependencies & Requirements

| Dependency | Required For | Notes |
|------------|-------------|-------|
| **Go** | Building from source | Optional if using pre-built `openclaw-dashboard` binaries from releases |
| **Bash** | `refresh.sh`, `install.sh`, `uninstall.sh` | These wrappers require Bash; the container smoke script uses POSIX sh |
| **Git** | Git log panel, installer | Optional (panel shows empty without it) |
| **OpenClaw CLI** | Migrated runtime collection | Must reach the selected native/container runtime; not included in the dashboard image |
| **Process tools** | Native PID, uptime and RSS | procps-compatible ps/pgrep on Linux; macOS supplies its own tools |
| **Timezone database** | Configured IANA timezones | Supplied by the OS or Go installation; the Alpine runtime image installs tzdata explicitly |

**Zero external packages (runtime):** No npm, no pip, no CDN, no third-party Go modules — stdlib only. Pre-built binaries do not require a local Go toolchain.

**Browser requirements:** A current browser supporting the embedded JavaScript and CSS APIs, including `fetch`, `AbortController`, optional chaining, CSS Grid and CSS custom properties. Historical minimum-version claims are not maintained; verify the rebuilt UI in the browsers you support.

---

## 11. Security Considerations

| Concern | Details |
|---------|---------|
| **Default bind** | `127.0.0.1` — local access; this is not user authentication |
| **LAN mode** | `OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK=1 --bind 0.0.0.0` exposes the dashboard to the local network with **no authentication** |
| **CORS** | Reflects valid HTTP localhost/127.0.0.1/IPv6 loopback origins; otherwise returns the configured localhost origin. Chat independently rejects foreign browser origins before execution. |
| **No HTTPS** | Plain HTTP only; use a reverse proxy for TLS |
| **Sensitive data in data.json** | Session keys, model usage, costs, cron config, gateway PID |
| **Gateway token handling** | `/api/chat` uses `OPENCLAW_GATEWAY_TOKEN` loaded from dotenv (`ai.dotenvPath`) |
| **Prompt safety** | `/api/chat` includes client-supplied `history` in gateway payload; treat this as untrusted input |
| **Read access / operations** | Anyone who can reach the port can read dashboard data. Optional operations have separate bearer authorization and loopback/origin checks. |
| **Subprocess execution** | `refresh.sh` locates and runs the `openclaw-dashboard` binary with `--refresh`; keep install paths and scripts writable only by trusted users |

---

## 12. Known Limitations

- **Timezone** — configurable via `config.json` `timezone` (IANA names, default `UTC`); the collector uses `time.LoadLocation` and falls back to UTC with a stderr warning if the name is unknown
- **No authentication** — relies on network-level access control
- **Polling only** — no WebSocket; frontend polls every 60s, server debounces at 30s
- **Limited historical data** — `dailyChart` provides 30 days of daily aggregates; no finer granularity
- **Some legacy config keys are ignored** — `openclawPath` is not read (use `OPENCLAW_HOME` env var); panel visibility is not configurable
- **Chat history cap is split client/server** — frontend keeps a local 6-message history window; backend also enforces `ai.maxHistory`
- **Simplistic cost projection** — `today × 30`, not based on historical average
- **Context % calculation** — `totalTokens / contextTokens × 100` (may exceed 100% in edge cases, capped in display)
- **Session limit** — modern collection walks up to 1,000 rows and reports truncation; the UI pages by 50. Legacy collection retains its 20-row recent-session view
- **Legacy transcript recovery** — active transcripts take precedence over dated deleted copies; when only deleted copies survive, the latest copy per lineage is used
- **Platform coverage** — macOS and Linux have different metrics/service implementations. A host test run does not execute the other platform's build-tagged tests

---

## 13. Development Guide

### Quick Start

```bash
cd ~/src/openclaw-dashboard

# Build the binary
make build

# Test data refresh
./openclaw-dashboard --refresh
# or: bash assets/runtime/refresh.sh
cat data.json | jq . | head -50

# Start dev server
./openclaw-dashboard --port 8080
# → http://127.0.0.1:8080

# LAN access
OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK=1 ./openclaw-dashboard --bind 0.0.0.0 --port 9090
```

### Editing

- **Frontend:** Edit `web/index.html`, run `make build`, restart the candidate binary, then reload the browser. There is no separate frontend bundler, but Go embeds the HTML at build time.
- **Data processing:** Edit `internal/apprefresh/` or the thin root wrappers. Rebuild the binary (or `go run ./cmd/openclaw-dashboard`) to apply.
- **Server:** Edit `internal/appserver/`, `internal/appchat/`, or the thin root wrappers. Rebuild to apply.

### Testing Checklist

```bash
# Preferred repo check surface
make check
```

- [ ] `make check` passes (vet, lint, race tests, govulncheck, staticcheck, build)
- [ ] `./openclaw-dashboard --refresh` (or `bash assets/runtime/refresh.sh`) produces valid JSON
- [ ] `data.json` contains expected keys
- [ ] Dashboard renders on desktop (1440px+)
- [ ] Dashboard renders on tablet (768–1024px)
- [ ] Dashboard renders on mobile (< 768px)
- [ ] Auto-refresh countdown works
- [ ] Tab switching (today/7d/30d/all-time) works for all tabbed panels
- [ ] Gateway offline state renders correctly
- [ ] Gateway readiness alert appears when `live=true, ready=false`
- [ ] Gateway Runtime card populates from `/api/system` data
- [ ] Alerts display with correct severity styling

#### Go Test Coverage

Tests live in the root facade, CLI entrypoint and all eight internal packages. `make test` runs the race detector; `make cover` writes statement coverage for the current platform. Inspect `go tool cover -func=coverage.out` and `coverage.html` rather than relying on stale test counts. Cross-package callers may exercise functions not counted by their package-local coverage profile. The dated [code audit](docs/plans/2026-09-07-code-audit.md) records measured coverage and remaining validation gaps.

### PR Guidelines

1. **Zero-dependency constraint** — no npm, no pip, no CDN, no external fonts
2. **Single-file frontend** — CSS and JS stay embedded in `web/index.html`
3. **Go stdlib only** — no third-party imports in Go source
4. **Test mobile + desktop** — check both responsive breakpoints
5. **Run automated checks** — `make check` before submitting changes

### Adding a New Dashboard Panel

1. Add HTML structure in `web/index.html` (follow existing `.glass .panel` pattern)
2. Add render logic in the `render()` function
3. If it needs new data, add extraction logic in `internal/apprefresh` (inside `collectDashboardData` or helpers it calls)
4. Add the new key to the `map[string]any` returned from `collectDashboardData`
5. If visibility should become configurable, add and document an explicit config schema field first; unknown `panels.*` keys are currently unsupported and logged as config warnings.

### Adding a New Alert Type

In `internal/apprefresh`, extend `BuildAlerts` (or the call site in `collectDashboardData`) with another `append`, for example:

```go
alerts = append(alerts, map[string]any{
	"type":     "warning",
	"icon":     "⚠️",
	"message":  "Description",
	"severity": "medium", // critical | high | medium | low
})
```

The frontend renders alerts automatically from the array. Severity maps to CSS classes: `.alert-critical`, `.alert-high`, `.alert-medium`, `.alert-low`.
