# Configuration Guide

## config.json

The dashboard is configured via `config.json` in the active dashboard runtime directory.
In a source checkout that is usually the repo root. In Homebrew installs it is
`~/.openclaw/dashboard/config.json`. Default runtime files in this repo live in
`assets/runtime/`.

In Homebrew installs, upgrades preserve existing `config.json` and runtime
`themes.json`. The package-managed `VERSION` file is refreshed automatically so
the binary reports the installed release version correctly after upgrades.

### Full Example

```json
{
  "bot": {
    "name": "My OpenClaw Bot",
    "emoji": "🤖"
  },
  "theme": {
    "preset": "midnight"
  },
  "timezone": "UTC",
  "refresh": {
    "intervalSeconds": 30
  },
  "server": {
    "port": 8080,
    "host": "127.0.0.1"
  },
  "alerts": {
    "dailyCostHigh": 50,
    "dailyCostWarn": 20,
    "contextPct": 80,
    "memoryMb": 640
  },
  "ai": {
    "enabled": true,
    "gatewayPort": 18789,
    "model": "",
    "maxHistory": 6,
    "dotenvPath": "~/.openclaw/.env"
  },
  "logs": {
    "enabled": true,
    "tailLines": 200,
    "fastRefreshMs": 3000,
    "errorWindowHours": 24,
    "maxErrorSignatures": 1000,
    "systemdUnit": "openclaw-gateway",
    "sources": [
      "logs/gateway.log",
      "logs/gateway.err.log"
    ]
  },
  "system": {
    "enabled": true,
    "pollSeconds": 10,
    "metricsTtlSeconds": 10,
    "versionsTtlSeconds": 300,
    "gatewayTimeoutMs": 5000,
    "coldPathTimeoutMs": 8000,
    "cpuTimeoutMs": 6000,
    "deepStatus": false,
    "gatewayPort": 18789,
    "diskPath": "/",
    "warnPercent": 70,
    "criticalPercent": 85,
    "cpu": { "warn": 80, "critical": 95 },
    "ram": { "warn": 80, "critical": 95 },
    "swap": { "warn": 80, "critical": 95 },
    "disk": { "warn": 80, "critical": 95 }
  }
}
```

### Bot Settings

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `bot.name` | string | `"OpenClaw Dashboard"` | Displayed in the header |
| `bot.emoji` | string | `"🦞"` | Avatar emoji in the header |

### Theme

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `theme.preset` | string | `"midnight"` | Default theme. Options: `midnight`, `nord`, `catppuccin-mocha`, `github-light`, `solarized-light`, `catppuccin-latte` |

Theme choice persists via `localStorage` (key: `ocDashTheme`). The `theme.preset` sets the initial default — once a user picks a theme via the 🎨 header button, their choice overrides the config.

#### Built-in Themes

| ID | Name | Type | Icon |
|----|------|------|------|
| `midnight` | Midnight | Dark | 🌙 |
| `nord` | Nord | Dark | 🏔️ |
| `catppuccin-mocha` | Catppuccin Mocha | Dark | 🌸 |
| `github-light` | GitHub Light | Light | ☀️ |
| `solarized-light` | Solarized Light | Light | 🌅 |
| `catppuccin-latte` | Catppuccin Latte | Light | 🌻 |

#### Custom Themes

Add custom themes by editing `themes.json` in the dashboard runtime directory.
The built-in defaults ship from `assets/runtime/themes.json`. Each theme requires
a `name`, `type` (`dark` or `light`), `icon`, and a `colors` object with all 19
CSS variables:

```json
{
  "my-theme": {
    "name": "My Theme",
    "type": "dark",
    "icon": "🎯",
    "colors": {
      "bg": "#1a1a2e",
      "surface": "rgba(255,255,255,0.03)",
      "surfaceHover": "rgba(255,255,255,0.045)",
      "border": "rgba(255,255,255,0.06)",
      "accent": "#e94560",
      "accent2": "#0f3460",
      "green": "#4ade80",
      "yellow": "#facc15",
      "red": "#f87171",
      "orange": "#fb923c",
      "purple": "#a78bfa",
      "text": "#e5e5e5",
      "textStrong": "#ffffff",
      "muted": "#737373",
      "dim": "#525252",
      "darker": "#404040",
      "tableBg": "rgba(255,255,255,0.025)",
      "tableHover": "rgba(255,255,255,0.05)",
      "scrollThumb": "rgba(255,255,255,0.1)"
    }
  }
}
```

All 19 color variables must be provided. The theme appears automatically in the theme picker menu, grouped by `type`.

### Timezone

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `timezone` | string | `"UTC"` | IANA timezone name for all time calculations and displayed timestamps |

Accepts any IANA timezone name, e.g. `"UTC"`, `"America/New_York"`, `"Europe/London"`. All "today" cost windows, cron timestamps, and chart bucket boundaries use this timezone. The Go binary uses `time.LoadLocation()` from the standard library.

### Panels

Panel visibility is not configurable — all panels are always displayed.

### Refresh

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `refresh.intervalSeconds` | number | `30` | Minimum seconds between data refreshes (debounce) |

### Server

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `server.port` | number | `8080` | HTTP server port |
| `server.host` | string | `"127.0.0.1"` | Bind address (`0.0.0.0` for LAN access) |

### Alerts

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `alerts.dailyCostHigh` | number | `50` | USD threshold for a high-cost alert |
| `alerts.dailyCostWarn` | number | `20` | USD threshold for a warning alert |
| `alerts.contextPct` | number | `80` | Context usage % above which an alert is shown |
| `alerts.memoryMb` | number | `640` | Gateway RSS memory (MB) above which an alert is shown |

### OpenClaw Path

To change the OpenClaw data directory, set the `OPENCLAW_HOME` environment variable — that is the runtime source of truth for both `refresh.sh` and the installer. The `openclawPath` key in `config.json` is not read by the current runtime.

### System Metrics

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `system.enabled` | boolean | `true` | Enable/disable the top metrics bar and `/api/system` endpoint |
| `system.pollSeconds` | number | `10` | How often the browser polls `/api/system` (2-60 seconds) |
| `system.metricsTtlSeconds` | number | `10` | Server-side metrics cache TTL (2-60 seconds) |
| `system.versionsTtlSeconds` | number | `300` | Version/gateway probe cache TTL (30-3600 seconds) |
| `system.gatewayTimeoutMs` | number | `5000` | Timeout for gateway liveness probe (200-15000 ms) |
| `system.coldPathTimeoutMs` | number | `8000` | Overall budget for a cold `/api/system` collection — bounds total wall time when no warm cache is available (200-30000 ms) |
| `system.cpuTimeoutMs` | number | `6000` | Timeout for CPU sampling on macOS and Linux (500-20000 ms) |
| `system.deepStatus` | boolean | `false` | Opt into `openclaw status --json --deep`. Lean status (default) returns the task queue, plugin-compatibility warnings, and channel summary; deep status additionally returns the event-loop health and last-heartbeat blocks at the cost of a slower status call. Surfaced in the Runtime Health panel. |
| `system.gatewayPort` | number | `18789` | Gateway port for health probes (defaults to `ai.gatewayPort`) |
| `system.diskPath` | string | `"/"` | Filesystem path to report disk usage for |
| `system.warnPercent` | number | `70` | Global warn threshold (% used) — overridden by per-metric values |
| `system.criticalPercent` | number | `85` | Global critical threshold (% used) — overridden by per-metric values |
| `system.cpu.warn` | number | `80` | CPU warn threshold (%) |
| `system.cpu.critical` | number | `95` | CPU critical threshold (%) |
| `system.ram.warn` | number | `80` | RAM warn threshold (%) |
| `system.ram.critical` | number | `95` | RAM critical threshold (%) |
| `system.swap.warn` | number | `80` | Swap warn threshold (%) |
| `system.swap.critical` | number | `95` | Swap critical threshold (%) |
| `system.disk.warn` | number | `80` | Disk warn threshold (%) |
| `system.disk.critical` | number | `95` | Disk critical threshold (%) |

### Logs

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `logs.enabled` | boolean | `true` | Enable/disable the Logs panel and error feed |
| `logs.tailLines` | number | `200` | Lines tailed per source |
| `logs.fastRefreshMs` | number | `3000` | Logs panel fast-refresh interval (ms) |
| `logs.errorWindowHours` | number | `24` | Window for the error-signature feed |
| `logs.maxErrorSignatures` | number | `1000` | Cap on distinct error signatures tracked |
| `logs.sources` | string[] | `["logs/gateway.log", "logs/gateway.err.log"]` | Log files to tail (relative to the OpenClaw home) |
| `logs.systemdUnit` | string | `"openclaw-gateway"` | Systemd `--user` unit name for the Linux journald fallback. When a log source has no file on disk (the systemd gateway logs to journald, not a file), the dashboard reads `journalctl --user -u <unit>.service -o json`. Overridable by the `OPENCLAW_SYSTEMD_UNIT` env var; `OPENCLAW_PROFILE` appends a `-<profile>` suffix. Linux only; ignored on macOS. |

### AI Chat

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `ai.enabled` | boolean | `true` | Enable/disable AI chat panel and `/api/chat` endpoint |
| `ai.gatewayPort` | number | `18789` | OpenClaw gateway port used for chat completions |
| `ai.model` | string | `""` | Gateway model ID for chat requests |
| `ai.maxHistory` | number | `6` | Server-side cap for previous chat messages included in context |
| `ai.dotenvPath` | string | `"~/.openclaw/.env"` | Path to dotenv file containing `OPENCLAW_GATEWAY_TOKEN` |

### AI Chat Setup

1. Enable OpenAI-compatible chat completions in your OpenClaw gateway config:

```json
"gateway": {
  "http": {
    "endpoints": {
      "chatCompletions": { "enabled": true }
    }
  }
}
```

2. Ensure `OPENCLAW_GATEWAY_TOKEN` exists in your dotenv file (default: `~/.openclaw/.env`).
3. Restart gateway and dashboard after changing gateway or dotenv config.

## Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENCLAW_HOME` | OpenClaw installation path (source of truth for `refresh.sh` and installer) |
| `OPENCLAW_GATEWAY_TOKEN` | Gateway bearer token loaded from `ai.dotenvPath` |
| `OPENCLAW_SYSTEMD_UNIT` | Overrides the systemd unit name used for the Linux journald log fallback (default `openclaw-gateway`). Takes precedence over `logs.systemdUnit`. |
| `OPENCLAW_PROFILE` | When set, appends a `-<profile>` suffix to the resolved systemd unit name (matches openclaw's per-profile unit naming). |
| `OPENCLAW_CONFIG_PATH` | Overrides the openclaw config path used to locate the gateway lock file (default `<OPENCLAW_HOME>/openclaw.json`). The lock supplies install-independent gateway PID/uptime/RSS. |
| `OPENCLAW_DASHBOARD_DIR` | Override the dashboard runtime directory |
| `OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK` | Set to the literal value `1` to permit non-loopback bind hosts (e.g., `0.0.0.0`). Required for containerized deployments where the bind has to be reachable from outside the container. Off by default; see Security below. |
| `DASHBOARD_PORT` | Override the HTTP listen port (takes precedence over `server.port` in config) |
| `DASHBOARD_BIND` | Override the HTTP bind address (takes precedence over `server.host` in config) |
| `DASHBOARD_AI_TOKEN_OPTIONAL` | When `ai.enabled=true` but `OPENCLAW_GATEWAY_TOKEN` is missing, set to `1` to downgrade the startup fatal to a warning (useful for dev gateways without auth). Default unset; only the literal value `1` enables the bypass. |

## Security

The dashboard is designed to run on a developer or operator's local machine.
A few hard rules are enforced at startup or per-request:

- **Loopback-only bind by default.** `--bind`/`DASHBOARD_BIND` accept only
  `127.0.0.1`, `localhost`, `::1`, or empty. Anything else aborts startup
  unless `OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK=1` is set. Rationale: the
  chat rate-limit map grows unbounded between cleanup cycles, so exposing the
  HTTP surface to a public network turns it into a DoS surface.
- **Container deployment.** Set `OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK=1` and
  bind to `0.0.0.0` so the published port works, or use
  `docker run --network=host` and keep the loopback bind (Linux only).
- **HTML response headers.** The `/` handler sets
  `Content-Security-Policy: default-src 'self'; …; connect-src 'self'; frame-ancestors 'none'; …`,
  plus `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, and
  `Referrer-Policy: no-referrer`. Inline scripts still need `'unsafe-inline'`
  (the SPA inlines its bundles), but cross-origin exfiltration and
  clickjacking are blocked.
- **Gateway token redaction.** `appchat.CallGateway` strips the bearer token
  from any 5xx response body before surfacing the error to the browser.

## Data Flow

1. Browser opens the embedded frontend from `web/index.html` (`//go:embed`)
2. JavaScript calls `GET /api/refresh`
3. Go server runs data collection (debounced) via `RunRefreshCollector()` in `internal/apprefresh`
4. Collector reads OpenClaw data → writes `data.json` (atomic via tmp + rename)
5. Server returns `data.json` content (stale-while-revalidate)
6. Dashboard renders all panels (including AI chat UI if enabled)
7. AI chat uses `POST /api/chat` with `{question, history}` and receives `{answer}` or `{error}`
8. Auto-refresh repeats every 60 seconds
