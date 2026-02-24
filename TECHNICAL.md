# TECHNICAL.md — OpenClaw Dashboard Internals

> **Version:** 2026.2.23 · **Repo:** [github.com/mudrii/openclaw-dashboard](https://github.com/mudrii/openclaw-dashboard)
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

| File | Lines | Purpose |
|------|------:|---------|
| `index.html` | 1160 | Single-file frontend — embedded CSS + JS, glass morphism themed UI, 11 dashboard sections, 6 themes, 3 SVG charts |
| `themes.json` | 158 | Theme definitions — 6 built-in themes (3 dark + 3 light), 19 CSS variables each |
| `server.py` | ~400 | Python HTTP server — static files + `/api/refresh` + `/api/chat` endpoints |
| `refresh.sh` | 774 | Bash wrapper invoking inline Python to parse OpenClaw data → `data.json` |
| `install.sh` | 151 | Cross-platform installer (macOS LaunchAgent / Linux systemd) |
| `uninstall.sh` | 47 | Service teardown + file cleanup |
| `config.json` | — | Runtime configuration (bot/theme/server/refresh/alerts/ai; some compatibility keys are currently no-op) |
| `data.json` | — | Generated dashboard data (gitignored) |
| `docs/CONFIGURATION.md` | — | Configuration reference |
| `examples/config.full.json` | — | All available config options |
| `examples/config.minimal.json` | — | Minimal starter config |
| `tests/*.py` | — | Automated static + integration tests (pytest/unittest compatible) |
| `screenshots/` | — | Dashboard screenshots for README |

---

## 2. Data Pipeline

```
Browser                                              Browser
  │                                                    ▲
  │ GET /api/refresh?t=<cache-bust>                    │ JSON response
  ▼                                                    │
server.py ─── debounce check ──► refresh.sh ──► data.json.tmp
  │           (30s default)        │                │
  │           if < 30s:            │ inline Python  │ mv (atomic)
  │           serve cached         │ reads OpenClaw │
  │                                ▼                ▼
  └──────── read data.json ◄──── data.json

Browser                                              OpenClaw Gateway
  │ POST /api/chat {"question","history"}                 ▲
  ▼                                                       │ POST /v1/chat/completions
server.py handle_chat()                                   │ Bearer token from dotenv
  ├─ load data.json                                       │
  ├─ build_dashboard_prompt(data)                         │
  └─ call_gateway(...) ───────────────────────────────────┘
```

### Debounce Mechanism

`server.py` tracks `_last_refresh` (epoch timestamp). If less than `_debounce_sec` seconds have elapsed since the last successful refresh, `refresh.sh` is skipped and the cached `data.json` is served directly. Default: **30 seconds** (configurable via `config.json` → `refresh.intervalSeconds`).

### Atomic Write

`refresh.sh` writes Python output to `data.json.tmp`, then:
```bash
if [ -s "$DIR/data.json.tmp" ]; then
    mv "$DIR/data.json.tmp" "$DIR/data.json"
fi
```
The `-s` check ensures an empty/failed output doesn't clobber the existing data. `mv` on the same filesystem is atomic.

### Concurrency

`_refresh_lock` (a `threading.Lock`) prevents concurrent `refresh.sh` invocations. The subprocess has a **15-second timeout** (`REFRESH_TIMEOUT`).

---

## 3. Data Sources

`refresh.sh` reads these files from the OpenClaw directory (default `~/.openclaw`):

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

---

## 4. Data Processing Logic

All processing runs in the inline Python block within `refresh.sh`.

### Model Name Normalization

The `model_name()` function maps raw provider/model IDs (e.g., `anthropic/claude-opus-4-6`) to friendly display names (e.g., `Claude Opus 4.6`). It strips the provider prefix and matches against known substrings:

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

For each `.jsonl` file, the script reads every line, filters for `assistant` role messages with non-zero `usage.totalTokens`, and aggregates into eight `defaultdict` buckets:

- **`models_all`** — all-time per-model totals
- **`models_today`** — today-only per-model totals (compared against `today_str` in the configured timezone)
- **`models_7d`** — last 7 days per-model totals
- **`models_30d`** — last 30 days per-model totals
- **`subagent_all`** — all-time subagent-only totals
- **`subagent_today`** — today subagent-only totals
- **`subagent_7d`** — last 7 days subagent-only totals
- **`subagent_30d`** — last 30 days subagent-only totals

Each bucket tracks: `calls`, `input`, `output`, `cacheRead`, `totalTokens`, `cost`.

Messages from `delivery-mirror` models are excluded.

### Cost Calculation

Cost is extracted from `message.usage.cost.total` in JSONL assistant messages. Only `dict`-type cost objects are parsed.

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

```python
projected_from_today = total_cost_today * 30
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
loadData()
  → fetch('/api/refresh?t=' + Date.now())
  → parse JSON → store in global D
  → render()
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

Three tab variables control today/7d/30d/all-time views: `uTab` (token usage), `srTab` (subagent runs), `stTab` (subagent tokens). Tab buttons update the variable and call `render()` which reads the current tab state.

The `switchTab` pattern uses `setTabCls4(prefix, tab, cls)` which updates four tab buttons (`T`, `7`, `30`, `A` suffixes) to set the active CSS class.

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

| Function | Purpose |
|----------|---------|
| `loadThemes()` | Fetches `themes.json`, restores saved theme from `localStorage('ocDashTheme')`, calls `applyTheme()` |
| `applyTheme(id)` | Sets all 19 `--*` CSS variables from `THEMES[id].colors`, saves to `localStorage` |
| `renderThemeMenu()` | Builds the dropdown menu, grouping themes by `type` (`dark` / `light`) |
| `toggleThemeMenu()` | Toggles `.open` class on `#themeMenu` |

The 19 CSS variables controlled by themes: `bg`, `surface`, `surfaceHover`, `border`, `accent`, `accent2`, `green`, `yellow`, `red`, `orange`, `purple`, `text`, `textStrong`, `muted`, `dim`, `darker`, `tableBg`, `tableHover`, `scrollThumb`.

Theme state is stored globally in `THEMES` (all definitions) and `currentTheme` (active theme ID). Clicking outside the theme picker closes the menu via a `document.addEventListener('click', ...)` handler.

---

## 6. Server Architecture

### Core

- Built on `http.server.HTTPServer` + `SimpleHTTPRequestHandler`
- Single-threaded request handling (Python's default)
- Two custom routes:
  - `GET /api/refresh` (with optional query params)
  - `POST /api/chat`
- All other paths: static file serving from the dashboard directory

### `/api/refresh` Endpoint

1. Calls `run_refresh()` (debounced)
2. Reads `data.json` from disk
3. Returns it with headers:
   - `Content-Type: application/json`
   - `Cache-Control: no-cache`
   - `Access-Control-Allow-Origin: <origin>` when origin is `http://localhost:*` or `http://127.0.0.1:*`
   - fallback CORS origin: `http://localhost:8080`
4. On error: returns 503 (no data.json) or 500 (other)

### `/api/chat` Endpoint

1. Checks `ai.enabled` from `config.json`
2. Validates JSON body and non-empty `question`
3. Trims `history` to `ai.maxHistory` entries (server-side cap)
4. Loads `data.json` and builds a compact system prompt (`build_dashboard_prompt`)
5. Calls OpenClaw gateway endpoint:
   - `POST http://localhost:<ai.gatewayPort>/v1/chat/completions`
   - headers: `Authorization: Bearer <OPENCLAW_GATEWAY_TOKEN>`
6. Returns HTTP 200 with either:
   - `{"answer":"..."}`
   - `{"error":"..."}`

### Quiet Logging

`log_message()` is overridden to only print lines containing `/api/refresh`, `/api/chat`, or `error`. Static file requests are suppressed.

### LAN Mode

When bound to `0.0.0.0`, the server auto-detects the local IP via `socket.gethostbyname(socket.gethostname())` and prints it for convenience.

---

## 7. Configuration Cascade

Each setting resolves through a priority chain (highest wins):

| Setting | CLI Flag | Env Var | config.json Path | Default |
|---------|----------|---------|-------------------|---------|
| Bind address | `--bind` / `-b` | `DASHBOARD_BIND` | `server.host` | `127.0.0.1` |
| Port | `--port` / `-p` | `DASHBOARD_PORT` | `server.port` | `8080` |
| Debounce interval | — | — | `refresh.intervalSeconds` | `30` |
| OpenClaw path (refresh script) | — | `OPENCLAW_HOME` | *(not read by runtime)* | `~/.openclaw` |
| AI chat enabled | — | — | `ai.enabled` | `true` |
| Gateway port | — | — | `ai.gatewayPort` | `18789` |
| Chat model | — | — | `ai.model` | `"kimi-coding/k2p5"` |
| Max history (server cap) | — | — | `ai.maxHistory` | `6` |
| Dotenv path for gateway token | — | — | `ai.dotenvPath` | `"~/.openclaw/.env"` |
| Bot name | — | — | `bot.name` | `OpenClaw Dashboard` |
| Bot emoji | — | — | `bot.emoji` | `⚡` |
| Daily cost high | — | — | `alerts.dailyCostHigh` | `50` |
| Daily cost warn | — | — | `alerts.dailyCostWarn` | `20` |
| Context % threshold | — | — | `alerts.contextPct` | `80` |
| Memory threshold | — | — | `alerts.memoryMb` | `640` |

**Implementation detail:** `server.py` applies `config.json` values, then env vars, then CLI args for bind/port. AI config is loaded from `config.json`, while `OPENCLAW_GATEWAY_TOKEN` is read from `ai.dotenvPath` via `read_dotenv()`. `refresh.sh` resolves OpenClaw path from `OPENCLAW_HOME` (or `~/.openclaw`) and does not read `config.openclawPath`.

---

## 8. data.json Schema

### Top-Level Fields

| Key | Type | Description |
|-----|------|-------------|
| `botName` | `string` | Display name from config (`"OpenClaw Dashboard"`) |
| `botEmoji` | `string` | Emoji from config (`"🦞"`) |
| `lastRefresh` | `string` | Human-readable timestamp (`"2026-02-16 13:45:00 Asia/Shanghai"`) |
| `lastRefreshMs` | `number` | Unix epoch milliseconds |

### Gateway

| Key | Type | Description |
|-----|------|-------------|
| `gateway.status` | `"online" \| "offline"` | Process detection result |
| `gateway.pid` | `number \| null` | Process ID |
| `gateway.uptime` | `string` | Elapsed time from `ps` (e.g., `"3-02:15:30"`) |
| `gateway.memory` | `string` | Formatted RSS (e.g., `"245 MB"`) |
| `gateway.rss` | `number` | Raw RSS in KB |

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

1. Check prerequisites (Python 3, OpenClaw directory)
2. Clone repo (or `git pull` if exists, or `curl` tarball if no git)
3. `chmod +x` scripts
4. Copy `examples/config.minimal.json` → `config.json` (if not exists)
5. Run initial `refresh.sh`
6. Create and load OS-specific service
7. Print URLs

### Uninstall Flow

1. Stop and remove service (LaunchAgent or systemd)
2. Kill any running `server.py` processes
3. `rm -rf` the install directory

---

## 10. Dependencies & Requirements

| Dependency | Required For | Notes |
|------------|-------------|-------|
| **Python 3.x** | `server.py`, inline Python in `refresh.sh` | stdlib only — `json`, `glob`, `os`, `subprocess`, `http.server`, `urllib`, `threading`, `collections`, `datetime` |
| **Bash** | `refresh.sh`, `install.sh`, `uninstall.sh` | POSIX-compatible |
| **Git** | Git log panel, installer | Optional (panel shows empty without it) |
| **OpenClaw** | Data source | Standard `~/.openclaw` directory structure |

**Zero external packages:** No npm, no pip, no CDN, no build tools.

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
| **Subprocess execution** | `server.py` executes `refresh.sh` via `subprocess.run` — ensure the script isn't writable by others |

---

## 12. Known Limitations

- **Timezone** — configurable via `config.json` `timezone` key (IANA names, default `Asia/Shanghai`); requires Python 3.9+ for `zoneinfo`, older Python falls back to fixed GMT+8
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
bash refresh.sh
cat data.json | python3 -m json.tool | head -50

# Start dev server
python3 server.py
# → http://127.0.0.1:8080

# LAN access
python3 server.py --bind 0.0.0.0 --port 9090
```

### Editing

- **Frontend:** Edit `index.html` directly. No build step. Refresh browser.
- **Data processing:** Edit the Python block inside `refresh.sh` (between `<< 'PYEOF'` and `PYEOF`).
- **Server:** Edit `server.py`. Restart to apply.

### Testing Checklist

```bash
python3 -m pytest tests/ -v
```

- [ ] `bash refresh.sh` produces valid JSON
- [ ] `data.json` contains expected keys
- [ ] Dashboard renders on desktop (1440px+)
- [ ] Dashboard renders on tablet (768–1024px)
- [ ] Dashboard renders on mobile (< 768px)
- [ ] Auto-refresh countdown works
- [ ] Tab switching (today/7d/30d/all-time) works for all tabbed panels
- [ ] Gateway offline state renders correctly
- [ ] Alerts display with correct severity styling

### PR Guidelines

1. **Zero-dependency constraint** — no npm, no pip, no CDN, no external fonts
2. **Single-file frontend** — CSS and JS stay embedded in `index.html`
3. **Python stdlib only** — no third-party imports in `server.py` or `refresh.sh`
4. **Test mobile + desktop** — check both responsive breakpoints
5. **Run automated tests** — use `pytest` before submitting changes

### Adding a New Dashboard Panel

1. Add HTML structure in `index.html` (follow existing `.glass .panel` pattern)
2. Add render logic in the `render()` function
3. If it needs new data, add extraction logic in the Python block of `refresh.sh`
4. Add the new key to `data.json` output dict
5. Optionally add a `panels.<name>` toggle in `config.json`

### Adding a New Alert Type

In `refresh.sh` Python block, append to the `alerts` list:
```python
alerts.append({
    'type': 'warning',      # warning | error | info
    'icon': '⚠️',
    'message': 'Description',
    'severity': 'medium'     # critical | high | medium | low
})
```

The frontend renders alerts automatically from the array. Severity maps to CSS classes: `.alert-critical`, `.alert-high`, `.alert-medium`, `.alert-low`.
