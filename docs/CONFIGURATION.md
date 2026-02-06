# Configuration Reference

All configuration is done via `config.json` in the dashboard directory.

## Full Configuration Example

```json
{
  "bot": {
    "name": "My Bot",
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
  "openclawPath": "~/.openclaw",
  "timezoneOffset": 0
}
```

## Options

### bot

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `name` | string | "OpenClaw Bot" | Bot name displayed in header |
| `emoji` | string | "🦞" | Emoji shown in avatar |

### theme

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `preset` | string | "dark" | Theme preset: `dark`, `light` |
| `accent` | string | "#6366f1" | Primary accent color (hex) |
| `accentSecondary` | string | "#9333ea" | Secondary accent color (hex) |

### panels

Toggle visibility of dashboard sections:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `kanban` | boolean | true | Show kanban task board |
| `sessions` | boolean | true | Show live sessions |
| `crons` | boolean | true | Show cron jobs |
| `skills` | boolean | true | Show skills grid |
| `tokenUsage` | boolean | true | Show token usage table |
| `subagentUsage` | boolean | true | Show sub-agent usage |
| `models` | boolean | true | Show available models |

### refresh

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `intervalSeconds` | number | 30 | Auto-refresh interval |
| `autoRefresh` | boolean | true | Enable auto-refresh |

### server

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `port` | number | 8080 | HTTP server port |
| `host` | string | "127.0.0.1" | Bind address |

### Paths

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `openclawPath` | string | "~/.openclaw" | Path to OpenClaw installation |
| `timezoneOffset` | number | 0 | Timezone offset in hours (0 = system local) |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENCLAW_HOME` | Override OpenClaw path |

## Minimal Configuration

For most users, this is enough:

```json
{
  "bot": {
    "name": "My Bot",
    "emoji": "🤖"
  }
}
```

Everything else uses sensible defaults.
