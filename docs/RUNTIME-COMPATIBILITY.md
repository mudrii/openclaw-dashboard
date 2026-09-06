# OpenClaw runtime compatibility

The dashboard targets OpenClaw's migrated runtime (2026.8.2 and 2026.9.1) without opening private SQLite tables. A Go 1.27.1 candidate passed read-only browser checks against Podman 2026.9.1, followed by a user-run Podman read contract test. Subsequent audit fixes require rebuilding and retesting the candidate; native/Docker end-to-end certification remains pending. See the dated [validation record](plans/2026-09-05-release-validation.md). It remains a zero-dependency Go server with one embedded SPA. Rebuild after frontend edits.

## Selecting the runtime

For a container-backed installation, add this to dashboard `config.json`:

```json
{"openclaw":{"mode":"container","container":"openclaw"}}
```

The same configuration works for Docker or Podman. The host OpenClaw CLI performs container discovery and execution; the dashboard does not access engine sockets directly or require a Go container SDK. In the inspected OpenClaw 2026.9.1 CLI, a running container with the selected name must be uniquely identifiable across the available Docker and Podman engines. If both engines have a running container with that name, use unique names. The service account needs a working host CLI, the relevant engine CLI on its PATH, and permission to access that engine. A stopped or inaccessible selected container is an unavailable source, not permission to use the native installation instead.

For the locally installed runtime, select it explicitly:

```json
{"openclaw":{"mode":"native"}}
```

This is explicit runtime selection, not automatic switching between unrelated installations. Run separate dashboard instances with separate ports and data directories when monitoring multiple targets concurrently.

Optional `profile` and `binary` select a profile or trusted local CLI executable. `mode: "native"` explicitly ignores inherited `OPENCLAW_CONTAINER`. Without an explicit mode, the existing environment selection is preserved. Service installation now persists an inherited container selection; explicit configuration is preferred. `OPENCLAW_STATE_DIR` overrides profile-derived state paths. Host CLI, selected runtime and gateway version are distinct facts. A container gateway has no asserted host PID.

Container selection, including inherited `OPENCLAW_CONTAINER`, uses runtime collectors even without local SQLite or legacy files. One-shot `--refresh` does not require a local state mount for a selected container. This does not install the host CLI or grant engine permissions. Running the dashboard image itself inside a container requires separately provisioning those runtime dependencies; the stock dashboard image does not contain them.

Service definitions also persist `OPENCLAW_DASHBOARD_DIR` from their configured working directory so a separately installed binary cannot silently load defaults from its own directory. Existing timezone configuration is preserved. Usage comparisons must use the same timezone, agent scope, range and cache state. The main dashboard is loopback-only; remote access still needs a separately secured boundary.

## Data sources and limits

| View | Source and behavior |
| --- | --- |
| Agent and model configuration | Native mode reads the selected local profile's configuration. Container mode reads the selected gateway's redacted `config.get` snapshot, including its reported configuration path for workspace defaults. A failed container read never falls back to host configuration. Same-target prior configuration can be retained as explicitly stale. Host workspace Git history and local session-store labels are not mixed into container snapshots. |
| Sessions | Gateway `sessions.list`, 100-row pages, at most 1,000 rows per refresh. Total count and collection completeness distinguish loaded rows from the full inventory. Exact active-run IDs, goals, placement and visibility are preserved when reported. |
| Usage | Gateway `sessions.usage` aggregates across all agents, separate today/7d/30d/all-time ranges. Totals do not come from the limited session page. Missing prices yield unknown cost, with known subtotal and missing-entry metadata retained. Warming caches are partial; prior complete data is displayed as stale. |
| Tasks | Gateway `tasks.list`, cursor pagination, at most 1,000 rows. Stable task IDs prevent title collisions; status, outcome, ownership, exact run identity, delivery and progress remain separate. |
| Automations | Gateway `cron.list` includes disabled jobs; bounded pages and on-demand `cron.runs` history. Command/script/prompt payload bodies and private delivery destinations are not exposed. |
| Memory | `doctor.memory.status` and non-deep CLI memory index status. Dirty indexes and unchecked embeddings are shown independently. No embedding/inference probes, memory snippets or automatic repair. |
| Models, plugins and skills | Per-agent model status, CLI plugin inventory and gateway skill eligibility. Allowed models, catalog presence, reported authentication kind and actual inference verification are distinct. |
| Channels | Gateway account-level status without active probes. Missing booleans remain unknown. |
| Gateway process | Authenticated selected-runtime `system.info` supplies gateway PID and uptime; `status.processMemory.rssBytes` supplies RSS. Only PID/uptime are retained from system info, not machine identity or environment data. Older or inaccessible gateways report unavailable/unsupported; same-target last-good values can remain explicitly stale. Container mode never substitutes host process metrics. |
| Work context | On-demand progress cards and safe widget metadata. Workboard board counts are read only when its plugin reports loaded. Interactive widgets remain in native OpenClaw; selected-session links preserve identity. |
| Logs and errors | Migrated runtimes use bounded, redacted gateway `logs.tail`, cached for five seconds. The error feed covers that bounded tail, not a proven complete historical window. Unsupported legacy source filters fail explicitly. |
| Backup and diagnostics | Only projected fields actually reported by status/config. Missing backup outcomes are not proof of success, failure, or restorability. |

Each snapshot collection reports source, state, attempt time, last-good time and completeness. Failed collectors retain same-target, same-timezone prior data as stale. A successful empty response replaces old rows. Unknown fields are not converted into healthy/idle/zero assertions. Legacy installations retain file-based readers; archived session lineages are deduplicated without deleting recovery files.

Runtime commands are allowlisted, shell-free, time bounded, and limited to 8 MiB stdout / 64 KiB stderr. Browser-facing errors contain canonical codes rather than raw command output. Display text is escaped; recognizable credential patterns in logs and diagnostics are redacted. Redaction is not a guarantee that arbitrary unlabeled sensitive text can be recognized: keep dashboard access restricted.

## On-demand endpoints

- `GET /api/automation/runs?id=<job>&offset=0`: 50 history entries, pagination metadata, separate execution/completion/delivery diagnostics.
- `GET /api/session/context?key=<session>`: bounded progress and widget metadata, no execution tickets or HTML.
- `GET /api/workboard`: capability-gated summary, up to 100 boards.
- `GET /api/chat/status`: endpoint/configuration/credential capability, never the credential. `inferenceVerified` is false: this endpoint performs no paid probe and does not claim that configured credentials have succeeded.
- `GET /api/operations/status`: whether optional operator actions are configured.
- `POST /api/operations`: opt-in exact-target actions, described in [configuration](CONFIGURATION.md#runtime-compatibility-and-optional-operations).

The existing AI drawer uses only a configured, enabled HTTP chat-completions endpoint. In container mode, endpoint readiness is read from the selected gateway configuration, not the host's `openclaw.json`. The HTTP transport is separate from CLI container routing: its configured host port must reach that same container's published gateway port, with matching credentials. A configured endpoint is not proof of HTTP reachability or successful inference. Gateway env SecretRefs can resolve server-side; unsupported secret providers fail closed. Monitoring can start even when chat credentials are absent. Gateway endpoint activation and device approval are not performed automatically.

CPU, RAM, swap and disk in the top bar describe the dashboard host. Container runtime health comes from the selected gateway; the dashboard does not invent a host PID for a container process. `stateDir` identifies the dashboard's local state-path context, not proof of a container bind mount.

The session summary separates active loaded sessions from the reported stored total. Token-table totals sum raw per-model components (cache column is cache-read; total includes the upstream total-token definition); usage bars represent token volume, not price. Complete pricing populates cost breakdowns. Incomplete pricing keeps total cost and monthly projection unknown, with the known subtotal and unpriced-model counts explained separately. Daily token volume remains chartable when prices are incomplete; cost/model-cost charts do not invent prices. Runtime skill inventory takes precedence over explicit legacy overrides, and channel configuration cards preserve separate runtime accounts. Absent cron duration remains null rather than zero. See the [field-fix validation record](plans/2026-09-05-field-fixes.md) for implementation versus live-verification status.

## Validation

```sh
make check
make frontend-test
OPENCLAW_DASHBOARD_LIVE=1 OPENCLAW_CONTAINER=openclaw go test ./internal/apprefresh -run '^TestRuntimeLiveReadContracts$' -count=1 -v
PLAYWRIGHT_MODULE=/absolute/path/to/playwright node scripts/runtime-browser-smoke.cjs
```

The normal Go suite uses fixtures and requires no running gateway. The live test is read-only and opt-in. The browser test requires an already installed Chrome and external Playwright package, adds no project dependency, defaults to `127.0.0.1:8081`, and expects the validation instance's AI transport and operations to be disabled. Override `DASHBOARD_TEST_URL` for a compatible local instance. Never point action tests at live jobs.

### Field-level data availability

A `ready` collection means its read completed, not that every field is populated. Session rows retain `tokenState` and `contextState`: absent, stale, invalid, or ready token counts, and a separately missing context limit. Stale counts are not used to calculate a current context percentage. Cost labels distinguish incomplete collection, missing pricing, and unreported cost. Valid zeros remain numeric zeros.

The pricing coverage disclosure matches exact provider/model IDs with unpriced all-history entry counts and the selected configuration's numeric base rates (USD per million tokens). No credentials or provider URLs are projected. These rates are diagnostic configuration, not verified invoices; tiered and plan-specific billing must be checked upstream. No fallback rates are assigned and historical records are not rewritten.

Backup runtime status and explicit backup/diagnostic/OpenTelemetry configuration are separate rows. Omitted configuration does not establish a default. Collected last/latest attempt/success timestamps are supported, but a scheduled backup is not evidence of success or restorability. Authentication diagnostics retain the CLI's reported kind and distinguish an absent in-use diagnostic list from an explicit empty list; neither means inference was tested. Tasks preserve authoritative owner/session/child-session identities and explain missing imported identities without guessing them. Channel and embedding probes remain opt-in upstream operations, not automatic background checks.

Node is required for `make frontend-test` and therefore `make check`; CI and release hooks require the same harness. Rebuild and restart the candidate after changes to the embedded frontend. See the [field-reason implementation record](plans/2026-09-06-field-reasons.md) for remaining upstream and live-validation gates.

Go 1.27 is now the coordinated pin: `go.mod` declares `go 1.27` with `toolchain go1.27.1`, the Dockerfile builder is `golang:1.27-alpine` at a pinned digest, `flake.nix` selects `pkgs.go_1_27`, and CI resolves the same toolchain through `go-version-file: go.mod`. The Makefile uses Staticcheck v0.8.1 for Go 1.27 compatibility. Network access is required when tool modules or vulnerability data are not cached, and the full race suite requires permission to bind local test listeners. Sandbox failures are not a passing gate.

The dependency-free frontend regression harness checks the embedded functions and CSS contracts; it is not a substitute for rendering the rebuilt candidate in a browser. See the [2026.9.1 release-validation record](plans/2026-09-05-release-validation.md) for the current evidence, fixes and release gates. Native and Docker end-to-end runs remain distinct from the live Podman UI observations.

The toolchain pin was advanced from 1.26.5 to 1.26.8 after the vulnerability scan found six reachable standard-library issues; the subsequent full scan reported no vulnerabilities. Go's [release history](https://go.dev/doc/devel/release) documents the intervening security and maintenance fixes. No OpenClaw upgrade is involved.

Unsupported or deliberately unverified: provider inference, embedding execution, backup restore, arbitrary widget execution, cloud/team administration, automatic upgrades, broad approvals and direct SQLite repair. These are not inferred from healthy read-only collection.
