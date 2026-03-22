# TECHNICAL.md — OpenClaw Dashboard Internals

> **Version:** 2026.3.23 · **Repo:** [github.com/mudrii/openclaw-dashboard](https://github.com/mudrii/openclaw-dashboard)
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
| `internal/appruntime/` | Runtime dir resolution, version detection, Homebrew seeding |
| `internal/appchat/` | Prompt builder and gateway client |
| `internal/apprefresh/` | Dashboard data collector and aggregators |
| `internal/appserver/` | HTTP handlers, refresh coordinator, static serving |
| `internal/appsystem/` | Host metrics and OpenClaw runtime probes |
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
openclaw-dashboard ─── debounce check ──► RunRefreshCollector() ──► data.json.tmp
  │           (internal/appserver)          (internal/apprefresh)  │
  │           (30s default)                    │                    │ rename (atomic)
  │           if < 30s:                        │ reads OpenClaw     │
  │           still return cached              │ filesystem         ▼
  │                                            ▼                data.json
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

`internal/apprefresh` marshals JSON, writes `data.json.tmp`, then `os.Rename`s it to `data.json`. Rename within the same directory is atomic on Unix; a failed write or marshal does not replace the previous file.

### Concurrency

`Server.mu` (`sync.Mutex`) in `internal/appserver` coordinates `lastRefresh`, `refreshRunning`, and overlapping work. Debounce and “only one collector at a time” are enforced via `sync.Mutex`.

---

## 3. Data Sources

The refresh collector (`internal/apprefresh`, invoked by `openclaw-dashboard --refresh` or from the runtime `refresh.sh`) reads these files from the OpenClaw directory (default `~/.openclaw`):

| Source Path | What It Provides |
|-------------|-----------------|
| `openclaw.json` | Bot config: models, skills, compaction mode |
| `agents/*/sessions/sessions.json` | Session metadata (keys, tokens, context, model, timestamps) |
| `agents/*/sessions/*.jsonl` + `.jsonl.deleted.*` | Per-message token usage and cost data |
| `cron/jobs.json` | Cron job definitions, schedules, state, last run status |
| `.git/` (via `git log`) | Last 5 commits (hash, message, relative time) |
| Process table (`pgrep` + `ps`) | Gateway PID, uptime, RSS memory |

### Gateway Detection

```bash
pgrep -f openclaw-gateway
```

If a PID is found, a follow-up `ps -p <pid> -o etime=,rss=` extracts uptime and RSS memory.

### Runtime Observability (`/api/system` — `openclaw` block)

In addition to the `data.json` pipeline, the `/api/system` endpoint includes a live `openclaw` block collected from three sources in parallel:

| Source | Data Collected |
|--------|---------------|
| `GET /healthz` | `live`, `uptimeMs`, `healthEndpointOk` |
| `GET /readyz` | `ready`, `failing[]`, `readyEndpointOk` |
| `openclaw status --json` | `currentVersion`, `latestVersion`, `connectLatencyMs`, `security` |

The `readyz` endpoint returns a `503` body with JSON when some dependencies are failing. `fetchJSONMapAllowStatus` accepts configurable HTTP status codes so the body is parsed rather than discarded.

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

The `modelName()` function maps raw provider/model IDs (e.g., `anthropic/claude-opus-4-6`) to friendly display names (e.g., `Claude Opus 4.6`). It strips the provider prefix and matches against known substrings:

| Pattern | Display Name |
|---------|-------------|
| `opus-4-6` | Claude Opus 4.6 |
| `opus` | Claude Opus 4.5 |
| `sonnet` | Claude Sonnet |
| `haiku` | Claude Haiku |
| `grok-4-fast` | Grok 4 Fast |
| `gemini-2.5-pro` | Gemini 2.5 Pro |
| `minimax-m2.5` | MiniMax M2.5 |
| `k2p5`, `kimi` | Kimi K2.5 |
| `gpt-5.3-codex` | GPT-5.3 Codex |
| *(fallback)* | Raw model string |

### Session Type Detection

Session keys are classified by substring matching:

| Key Pattern | Type |
|-------------|------|
| `cron:` | `cron` |
| `subagent:` | `subagent` |
| `group:` | `group` |
| `telegram` | `telegram` |
| ends with `:main` | `main` |
| *(other)* | `other` |

Sessions with `:run:` in the key are skipped (duplicate cron run sessions).

### Token Aggregation

For each `.jsonl` file, the collector reads assistant usage records and aggregates into eight `map[string]*tokenBucket` buckets. Parsed per-file summaries are persisted in `.token-usage-cache.json` in the dashboard runtime directory and reused when a transcript file's size and mtime have not changed, so refresh does not rescan the full transcript history every run:

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

Cost is extracted from `message.usage.cost.total` in JSONL assistant messages. Only JSON object-shaped cost values are parsed.

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
      → computes 13 boolean dirty flags via stableSnapshot()
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

Three pure SVG charts render in a `.grid-3` layout, controlled by a `chartDays` variable (7 or 30):

| Chart | Function | Visualization |
|-------|----------|--------------|
| **Daily Cost Trend** | `renderCostChart()` | Line chart with area fill — plots `dailyChart[].total` |
| **Cost by Model** | `renderModelChart()` | Stacked bar chart — breaks down daily cost by top 6 models + "Other" |
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
- Routes: `GET|HEAD /`, `GET|HEAD /api/refresh`, `GET|HEAD /api/system`, `POST /api/chat`, allowlisted static files (`/themes.json`, `/favicon.ico`, `/favicon.png`)
- All other paths return 404; non-GET/HEAD/POST (except `OPTIONS`) returns 405
- **Graceful shutdown**: handles SIGINT/SIGTERM, drains in-flight requests (5s timeout)
- **Pre-warm**: runs `runRefresh()` once in the background at startup so the first browser hit is fast
- **Dual mtime cache**: `cachedDataRaw` ([]byte for `/api/refresh`) and `cachedData` (parsed map for `/api/chat`) share a `sync.RWMutex` with coherence — updating either cache invalidates the other
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
2. Validates JSON body (64KB limit) and non-empty `question` (2000 char limit)
3. Sanitises `history`: only `user`/`assistant` roles, truncates content to 4000 chars, caps at `ai.maxHistory` entries
4. Loads `data.json` (mtime-cached) and builds a compact system prompt
5. Calls OpenClaw gateway endpoint:
   - `POST http://localhost:<ai.gatewayPort>/v1/chat/completions`
   - headers: `Authorization: Bearer <OPENCLAW_GATEWAY_TOKEN>`
   - Response capped at 1MB
6. Returns:
   - HTTP 200 `{"answer":"..."}` on success
   - HTTP 400 for bad input, 413 for oversized body, 429 when rate-limited, 502 for gateway errors, 503 if AI disabled

### Quiet Logging

Uses `log.Printf` for `/api/refresh`, `/api/chat`, and errors. Static file requests are not logged.

### LAN Mode

When bound to `0.0.0.0`, the server auto-detects the local IP and prints it for convenience.

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
| Bot emoji | — | — | `bot.emoji` | `⚡` |
| Daily cost high | — | — | `alerts.dailyCostHigh` | `50` |
| Daily cost warn | — | — | `alerts.dailyCostWarn` | `20` |
| Context % threshold | — | — | `alerts.contextPct` | `80` |
| Memory threshold | — | — | `alerts.memoryMb` | `640` |

**Implementation detail:** The Go binary applies `config.json` defaults, then environment variables, then CLI flags for bind/port. AI settings come from `config.json`; `OPENCLAW_GATEWAY_TOKEN` is read from `ai.dotenvPath`. The refresh collector uses `OPENCLAW_HOME` (or `~/.openclaw`) and does not read `config.openclawPath`.

---

## 8. data.json Schema

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
| `gateway.status` | `"online" \| "offline"` | Process detection result |
| `gateway.pid` | `number \| null` | Process ID |
| `gateway.uptime` | `string` | Elapsed time from `ps` (e.g., `"3-02:15:30"`) |
| `gateway.memory` | `string` | Formatted RSS (e.g., `"245 MB"`) |
| `gateway.rss` | `number` | Raw RSS in KB |

> Note: The dashboard UI shows two separate cards in System Settings — **Gateway Runtime** (populated from live `/api/system` data by `SystemBar.render()`) and **Gateway Config** (populated from `data.json`'s `agentConfig.gateway` by `Renderer.render()`). The `gatewayPanel`/`gatewayPanelInner` element from earlier versions has been removed; use `gatewayRuntimePanelInner` or `gatewayConfigPanelInner` instead.

### Cost Fields

| Key | Type | Description |
|-----|------|-------------|
| `compactionMode` | `string` | From openclaw.json (`"auto"`, `"manual"`, etc.) |
| `totalCostToday` | `number` | Sum of all model costs today |
| `totalCostAllTime` | `number` | Sum of all model costs ever |
| `projectedMonthly` | `number` | `totalCostToday × 30` |
| `costBreakdown` | `array` | All-time cost per model: `[{model, cost}]` |
| `costBreakdownToday` | `array` | Today's cost per model: `[{model, cost}]` |

### Sessions

| Key | Type | Description |
|-----|------|-------------|
| `sessions` | `array` | Top 20 most recent sessions (last 24h) |
| `sessions[].name` | `string` | Session label (truncated to 50 chars) |
| `sessions[].key` | `string` | Session key (e.g., `"telegram:group:-123:main"`) |
| `sessions[].agent` | `string` | Agent name (directory name) |
| `sessions[].model` | `string` | Raw model ID |
| `sessions[].contextPct` | `number` | Context window usage percentage (0-100) |
| `sessions[].lastActivity` | `string` | Time string (`"HH:MM:SS"`) |
| `sessions[].updatedAt` | `number` | Unix epoch milliseconds |
| `sessions[].totalTokens` | `number` | Total tokens in session |
| `sessions[].type` | `string` | `"cron"`, `"subagent"`, `"group"`, `"telegram"`, `"main"`, `"other"` |
| `sessionCount` | `number` | Total known session IDs (not just displayed) |

### Cron Jobs

| Key | Type | Description |
|-----|------|-------------|
| `crons` | `array` | All cron job definitions |
| `crons[].name` | `string` | Job name |
| `crons[].schedule` | `string` | Human-readable schedule (`"Every 6h"`, cron expr, etc.) |
| `crons[].enabled` | `boolean` | Whether the job is active |
| `crons[].lastRun` | `string` | Formatted timestamp or `""` |
| `crons[].lastStatus` | `string` | `"ok"`, `"error"`, `"none"` |
| `crons[].lastDurationMs` | `number` | Last run duration in ms |
| `crons[].nextRun` | `string` | Formatted next run timestamp or `""` |
| `crons[].model` | `string` | Model from job payload |

### Sub-Agent Activity

| Key | Type | Description |
|-----|------|-------------|
| `subagentRuns` | `array` | Last 30 sub-agent runs (all time) |
| `subagentRunsToday` | `array` | Last 20 sub-agent runs (today) |
| `subagentRuns7d` | `array` | Last 50 sub-agent runs (7 days) |
| `subagentRuns30d` | `array` | Last 100 sub-agent runs (30 days) |
| `subagentRuns[].task` | `string` | Session key (truncated to 60 chars) |
| `subagentRuns[].model` | `string` | Last model used |
| `subagentRuns[].cost` | `number` | Total session cost (4 decimal places) |
| `subagentRuns[].durationSec` | `number` | Session duration in seconds |
| `subagentRuns[].status` | `string` | Always `"completed"` |
| `subagentRuns[].timestamp` | `string` | `"YYYY-MM-DD HH:MM"` |
| `subagentRuns[].date` | `string` | `"YYYY-MM-DD"` |
| `subagentCostAllTime` | `number` | Total sub-agent cost (all time) |
| `subagentCostToday` | `number` | Total sub-agent cost (today) |
| `subagentCost7d` | `number` | Total sub-agent cost (7 days) |
| `subagentCost30d` | `number` | Total sub-agent cost (30 days) |

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
| `[].cost` | `number` | Total cost (2 decimal places) |
| `[].inputRaw` | `number` | Raw input token count |
| `[].outputRaw` | `number` | Raw output token count |
| `[].cacheReadRaw` | `number` | Raw cache read token count |
| `[].totalTokensRaw` | `number` | Raw total token count |

Sorted by cost descending.

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
| `dailyChart[].total` | `number` | Total cost for the day |
| `dailyChart[].tokens` | `number` | Total tokens for the day |
| `dailyChart[].calls` | `number` | Total API calls for the day |
| `dailyChart[].subagentCost` | `number` | Sub-agent cost for the day |
| `dailyChart[].subagentRuns` | `number` | Sub-agent run count for the day |
| `dailyChart[].models` | `object` | Per-model cost breakdown: `{modelName: cost}` (top 6 + "Other") |

### Alerts

| Key | Type | Description |
|-----|------|-------------|
| `alerts[].type` | `string` | `"warning"`, `"error"`, `"info"` |
| `alerts[].icon` | `string` | Emoji icon |
| `alerts[].message` | `string` | Human-readable alert text |
| `alerts[].severity` | `string` | `"critical"`, `"high"`, `"medium"`, `"low"` |

---

## 9. Installation & Service Management

### macOS — LaunchAgent

`install.sh` generates a plist at `~/Library/LaunchAgents/com.openclaw.dashboard.plist`:

- **RunAtLoad:** `true` — starts on login
- **KeepAlive:** `true` — restarts on crash
- **WorkingDirectory:** install dir
- **Logs:** `<install_dir>/server.log`

Commands:
```bash
launchctl load ~/Library/LaunchAgents/com.openclaw.dashboard.plist
launchctl unload ~/Library/LaunchAgents/com.openclaw.dashboard.plist
```

### Linux — systemd User Service

`install.sh` generates `~/.config/systemd/user/openclaw-dashboard.service`:

- **Restart:** `always` (5s delay)
- **WantedBy:** `default.target`

Commands:
```bash
systemctl --user start openclaw-dashboard
systemctl --user stop openclaw-dashboard
systemctl --user status openclaw-dashboard
```

### Install Flow

1. Check prerequisites (OpenClaw directory at `OPENCLAW_HOME` or `~/.openclaw`)
2. Clone repo (or `git pull` if exists, or `curl` tarball if no git)
3. `chmod +x` scripts
4. Copy `examples/config.minimal.json` → `config.json` (if not exists)
5. Run initial data generation: `./openclaw-dashboard --refresh` (or `bash refresh.sh`, which invokes the same flag)
6. Create and load OS-specific service
7. Print URLs

### Uninstall Flow

1. Stop and remove service (LaunchAgent or systemd)
2. Kill any running `openclaw-dashboard` processes
3. `rm -rf` the install directory

---

## 10. Dependencies & Requirements

| Dependency | Required For | Notes |
|------------|-------------|-------|
| **Go** | Building from source | Optional if using pre-built `openclaw-dashboard` binaries from releases |
| **Bash** | `refresh.sh`, `install.sh`, `uninstall.sh` | POSIX-compatible |
| **Git** | Git log panel, installer | Optional (panel shows empty without it) |
| **OpenClaw** | Data source | Standard `~/.openclaw` directory structure |

**Zero external packages (runtime):** No npm, no pip, no CDN, no third-party Go modules — stdlib only. Pre-built binaries do not require a local Go toolchain.

**Browser requirements:** CSS Grid, CSS custom properties, `fetch` API, `conic-gradient` — any modern browser (Chrome 69+, Firefox 65+, Safari 12.1+).

---

## 11. Security Considerations

| Concern | Details |
|---------|---------|
| **Default bind** | `127.0.0.1` — localhost only, safe |
| **LAN mode** | `--bind 0.0.0.0` exposes the dashboard to the local network with **no authentication** |
| **CORS** | Allows localhost/127.0.0.1 origins; fallback header is `http://localhost:8080` |
| **No HTTPS** | Plain HTTP only; use a reverse proxy for TLS |
| **Sensitive data in data.json** | Session keys, model usage, costs, cron config, gateway PID |
| **Gateway token handling** | `/api/chat` uses `OPENCLAW_GATEWAY_TOKEN` loaded from dotenv (`ai.dotenvPath`) |
| **Prompt safety** | `/api/chat` includes client-supplied `history` in gateway payload; treat this as untrusted input |
| **No auth/authz** | Anyone who can reach the port can see all data |
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
- **Session limit** — only top 20 most recent sessions shown (last 24h)
- **Sub-agent detection** — sessions not found in `sessions.json` are assumed to be sub-agents
- **Deleted session logs are included** — `.jsonl.deleted.*` files are intentionally scanned and counted

---

## 13. Development Guide

### Quick Start

```bash
cd ~/src/openclaw-dashboard

# Test data refresh
./openclaw-dashboard --refresh
# or: bash assets/runtime/refresh.sh
cat data.json | jq . | head -50

# Start dev server
./openclaw-dashboard --port 8080
# → http://127.0.0.1:8080

# LAN access
./openclaw-dashboard --bind 0.0.0.0 --port 9090
```

### Editing

- **Frontend:** Edit `web/index.html` directly. No build step. Refresh browser.
- **Data processing:** Edit `internal/apprefresh/` or the thin root wrappers. Rebuild the binary (or `go run ./cmd/openclaw-dashboard`) to apply.
- **Server:** Edit `internal/appserver/`, `internal/appchat/`, or the thin root wrappers. Rebuild to apply.

### Testing Checklist

```bash
# Go tests (run with race detector)
go test -race -v ./...
```

- [ ] `go test -race ./...` passes (all tests green)
- [ ] `./openclaw-dashboard --refresh` (or `bash refresh.sh`) produces valid JSON
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

| File | Tests | Coverage |
|------|------:|----------|
| `server_test.go` | 22 | Cache coherence, HEAD/GET, static allowlist, path traversal, CORS, routing, index rendering, data missing |
| `chat_test.go` | 11 | Gateway calls (success, errors, empty, oversized), system prompt building |
| `config_test.go` | 11 | Config defaults/overrides/clamping, dotenv parsing (quotes, comments, equals), expandHome |
| `version_test.go` | 12 | VERSION file, fallback, empty file |
| `system_test.go` | 43 | Openclaw runtime collection, gateway probes, `fetchJSONMapAllowStatus`, `parseGatewayStatusJSON`, CPU/RAM/swap/disk collectors, versions caching, thundering herd prevention |

### PR Guidelines

1. **Zero-dependency constraint** — no npm, no pip, no CDN, no external fonts
2. **Single-file frontend** — CSS and JS stay embedded in `web/index.html`
3. **Go stdlib only** — no third-party imports in Go source
4. **Test mobile + desktop** — check both responsive breakpoints
5. **Run automated tests** — `go test -race ./...` before submitting changes

### Adding a New Dashboard Panel

1. Add HTML structure in `web/index.html` (follow existing `.glass .panel` pattern)
2. Add render logic in the `render()` function
3. If it needs new data, add extraction logic in `internal/apprefresh` (inside `collectDashboardData` or helpers it calls)
4. Add the new key to the `map[string]any` returned from `collectDashboardData`
5. Optionally add a `panels.<name>` toggle in `config.json`

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
