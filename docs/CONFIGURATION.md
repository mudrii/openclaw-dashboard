# Configuration Guide

## config.json

The dashboard is configured via `config.json` in the dashboard directory.

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
  "timezone": "Asia/Shanghai",
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
    "model": "kimi-coding/k2p5",
    "maxHistory": 6,
    "dotenvPath": "~/.openclaw/.env"
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

Add custom themes by editing `themes.json` in the dashboard directory. Each theme requires a `name`, `type` (`dark` or `light`), `icon`, and a `colors` object with all 19 CSS variables:

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
| `timezone` | string | `"Asia/Shanghai"` | IANA timezone name for all time calculations and displayed timestamps |

Accepts any IANA timezone name, e.g. `"UTC"`, `"America/New_York"`, `"Europe/London"`. All "today" cost windows, cron timestamps, and chart bucket boundaries use this timezone. Requires Python 3.9+ (`zoneinfo` stdlib); older Python falls back to GMT+8.

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

### AI Chat

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `ai.enabled` | boolean | `true` | Enable/disable AI chat panel and `/api/chat` endpoint |
| `ai.gatewayPort` | number | `18789` | OpenClaw gateway port used for chat completions |
| `ai.model` | string | `"kimi-coding/k2p5"` | Gateway model ID for chat requests |
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
| `OPENCLAW_GATEWAY_TOKEN` | Gateway bearer token consumed by `server.py` via `ai.dotenvPath` |

## Data Flow

1. Browser opens `index.html`
2. JavaScript calls `GET /api/refresh`
3. `server.py` runs `refresh.sh` (debounced)
4. `refresh.sh` reads OpenClaw data → writes `data.json`
5. `server.py` returns `data.json` content
6. Dashboard renders all panels (including AI chat UI if enabled)
7. AI chat uses `POST /api/chat` with `{question, history}` and receives `{answer}` or `{error}`
8. Auto-refresh repeats every 60 seconds
