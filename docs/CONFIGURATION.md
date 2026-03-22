# Configuration Guide

## config.json

The dashboard is configured via `config.json` in the active dashboard runtime directory.
In a source checkout that is usually the repo root. In Homebrew installs it is
`~/.openclaw/dashboard/config.json`. Default runtime files in this repo live in
`assets/runtime/`.

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
  "system": {
    "enabled": true,
    "pollSeconds": 10,
    "metricsTtlSeconds": 10,
    "versionsTtlSeconds": 300,
    "gatewayTimeoutMs": 5000,
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
| `OPENCLAW_DASHBOARD_DIR` | Override the dashboard runtime directory |
| `DASHBOARD_PORT` | Override the HTTP listen port (takes precedence over `server.port` in config) |
| `DASHBOARD_BIND` | Override the HTTP bind address (takes precedence over `server.host` in config) |

## Data Flow

1. Browser opens the embedded frontend from `web/index.html` (`//go:embed`)
2. JavaScript calls `GET /api/refresh`
3. Go server runs data collection (debounced) via `RunRefreshCollector()` in `internal/apprefresh`
4. Collector reads OpenClaw data → writes `data.json` (atomic via tmp + rename)
5. Server returns `data.json` content (stale-while-revalidate)
6. Dashboard renders all panels (including AI chat UI if enabled)
7. AI chat uses `POST /api/chat` with `{question, history}` and receives `{answer}` or `{error}`
8. Auto-refresh repeats every 60 seconds
