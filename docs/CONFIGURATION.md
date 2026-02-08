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
    "preset": "dark",
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
| `theme.preset` | string | `"dark"` | Theme preset (currently only `dark`) |
| `theme.accent` | string | `"#6366f1"` | Primary accent color (hex) |
| `theme.accentSecondary` | string | `"#9333ea"` | Secondary accent color (hex) |

### Panels

Toggle individual panels on/off. All default to `true`.

| Key | Description |
|-----|-------------|
| `panels.kanban` | Kanban task board |
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
| `refresh.autoRefresh` | boolean | `true` | Enable auto-refresh on the frontend |

### Server

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `server.port` | number | `8080` | HTTP server port |
| `server.host` | string | `"127.0.0.1"` | Bind address (`0.0.0.0` for network access) |

### OpenClaw Path

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `openclawPath` | string | `"~/.openclaw"` | Path to OpenClaw installation. Can also be set via `OPENCLAW_HOME` env var. |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENCLAW_HOME` | Override OpenClaw installation path (takes precedence over config) |

## Data Flow

1. Browser opens `index.html`
2. JavaScript calls `GET /api/refresh`
3. `server.py` runs `refresh.sh` (debounced)
4. `refresh.sh` reads OpenClaw data → writes `data.json`
5. `server.py` returns `data.json` content
6. Dashboard renders all panels
7. Auto-refresh repeats every 60 seconds
