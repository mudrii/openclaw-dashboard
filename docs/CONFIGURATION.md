# Configuration Guide

## config.json

The dashboard is configured via `config.json` in the dashboard directory.

Note: some legacy compatibility keys still appear in examples (`refresh.autoRefresh`, `openclawPath`, `panels.kanban`) but are currently not read by runtime code.

### Full Example

```json
{
  "bot": {
    "name": "My OpenClaw Bot",
    "emoji": "🤖"
  },
  "theme": {
    "preset": "midnight",
    "accent": "#6366f1",
    "accentSecondary": "#9333ea"
  },
  "panels": {
    "kanban": true,
    "sessions": true,
    "crons": true,
    "skills": true,
    "tokenUsage": true,
    "subagentUsage": true,
    "models": true
  },
  "refresh": {
    "intervalSeconds": 30,
    "autoRefresh": true
  },
  "server": {
    "port": 8080,
    "host": "127.0.0.1"
  },
  "openclawPath": "~/.openclaw"
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
| `theme.preset` | string | `"midnight"` | Default theme preset. Options: `midnight`, `nord`, `catppuccin-mocha`, `github-light`, `solarized-light`, `catppuccin-latte` |

Theme choice persists across page reloads via `localStorage` (key: `ocDashTheme`). The `theme.preset` in config.json only sets the initial default — once a user picks a theme via the 🎨 header button, their choice overrides the config.

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

All 19 color variables must be provided. The theme will appear automatically in the theme picker menu, grouped by `type`.

### Panels

Toggle individual panels on/off. All default to `true`.

| Key | Description |
|-----|-------------|
| `panels.kanban` | Legacy key (kanban UI removed; currently no-op) |
| `panels.sessions` | Active sessions table |
| `panels.crons` | Cron jobs table |
| `panels.skills` | Skills grid |
| `panels.tokenUsage` | Token usage & cost table |
| `panels.subagentUsage` | Sub-agent activity tables |
| `panels.models` | Available models grid |

### Refresh

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `refresh.intervalSeconds` | number | `30` | Minimum seconds between data refreshes (debounce) |
| `refresh.autoRefresh` | boolean | `true` | Legacy key (frontend currently always auto-refreshes every 60s) |

### Server

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `server.port` | number | `8080` | HTTP server port |
| `server.host` | string | `"127.0.0.1"` | Bind address (`0.0.0.0` for network access) |

### OpenClaw Path

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `openclawPath` | string | `"~/.openclaw"` | Legacy key (current runtime uses `OPENCLAW_HOME` env var instead). |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENCLAW_HOME` | Set OpenClaw installation path for `refresh.sh` and installer (runtime source of truth) |

## Data Flow

1. Browser opens `index.html`
2. JavaScript calls `GET /api/refresh`
3. `server.py` runs `refresh.sh` (debounced)
4. `refresh.sh` reads OpenClaw data → writes `data.json`
5. `server.py` returns `data.json` content
6. Dashboard renders all panels
7. Auto-refresh repeats every 60 seconds
