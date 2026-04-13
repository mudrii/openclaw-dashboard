# OpenClaw Dashboard Architecture

Backend: a single Go binary with an embedded frontend and no third-party Go runtime dependencies.

Frontend: a single static HTML application embedded from `web/index.html`.

Runtime defaults: checked into `assets/runtime/` and copied or read from the active dashboard runtime directory.

## Repo Layout

```text
cmd/openclaw-dashboard/      CLI entrypoint
internal/appconfig/          config loading and defaults
internal/appruntime/         runtime-dir and Homebrew seeding
internal/appchat/            chat prompt + gateway client
internal/apprefresh/         dashboard data collector
internal/appserver/          HTTP server, handlers, refresh orchestration
internal/appsystem/          system metrics and OpenClaw runtime probes
internal/appservice/         service lifecycle backend (launchd/systemd)
web/                         embedded frontend assets
assets/runtime/              runtime defaults (config, themes, refresh script)
testdata/                    reusable fixtures for tests
```

## Runtime Model

The binary resolves a dashboard runtime directory with this precedence:

1. `OPENCLAW_DASHBOARD_DIR`
2. source checkout / extracted release root
3. Homebrew-seeded `~/.openclaw/dashboard`

At runtime, the server serves:

- embedded UI from `web/index.html`
- generated dashboard data from `data.json`
- runtime theme/config assets from the resolved dashboard directory
- fallback default assets from `assets/runtime/` when running from a source checkout or release tree

## Data Flow

```text
Browser
  -> GET /api/refresh
  -> appserver refresh coordinator
  -> apprefresh collector
  -> data.json
  -> browser renders dashboard

Browser
  -> POST /api/chat
  -> appserver chat handler
  -> appchat prompt builder + gateway client
  -> OpenClaw gateway
```

## Package Boundaries

- `appconfig` owns config parsing and normalization.
- `appruntime` owns path resolution, version detection, and Homebrew runtime seeding.
- `apprefresh` owns data collection from OpenClaw sessions, crons, git history, and token logs.
- `appsystem` owns live host metrics and gateway/runtime probes.
- `appchat` owns prompt construction and OpenAI-compatible gateway requests.
- `appserver` owns HTTP routing, caching, rate limiting, refresh coordination, and log/error feed endpoints.
- `appservice` owns service lifecycle management: install, uninstall, start, stop, restart, and status via launchd (macOS) or systemd (Linux).

The root `dashboard` package now exists mainly as a compatibility layer for tests and the exported `Main()` entry used by `cmd/openclaw-dashboard`.
