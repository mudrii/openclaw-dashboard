# OpenClaw 2026.8.2 capability and integration research

Date: 2026-09-05. Scope: official release evidence and the integration contracts relevant to OpenClaw Dashboard. This is research and an implementation proposal, not an authorization to upgrade, repair, migrate, or modify runtime state.

## Version and evidence boundaries

| Layer | Verified value | Evidence |
| --- | --- | --- |
| Requested target | 2026.8.2 | User request |
| Routed CLI and running Gateway | 2026.8.2, commit `0965053`; Gateway build `2026.8.2-0965053fe6b9-2026-09-01T17-11-58.000Z` | Runtime inspection in the accompanying analysis: `OPENCLAW_CONTAINER=openclaw` forwards execution into the container |
| Host npm package behind asdf | 2026.9.1 | `$HOME/.asdf/installs/nodejs/24.18.0/lib/node_modules/openclaw/package.json`; this is not proof of the container version |
| Public stable package at research time | 2026.9.1 | Live [npm latest metadata](https://registry.npmjs.org/openclaw/latest) |
| 2026.8.1 publication | 2026-08-31 03:30:51 UTC | [Official release API](https://api.github.com/repos/openclaw/openclaw/releases/tags/v2026.8.1) |
| 2026.8.2 publication | 2026-09-01 16:00:56 UTC | [Official release API](https://api.github.com/repos/openclaw/openclaw/releases/tags/v2026.8.2) |
| 2026.9.1 publication | 2026-09-03 18:31:33 UTC | [Official release API](https://api.github.com/repos/openclaw/openclaw/releases/tags/v2026.9.1) |

2026.8.2 is a real stable release, but is no longer the latest stable release at the time of this analysis. Its release page's statement that npm `latest` is 2026.8.2 records publication-time verification; the live registry now supersedes that fact. The host/container distinction should become an explicit dashboard health field.

Context7 was consulted using `library openclaw` and then `docs /openclaw/openclaw` with the full research question. It identified the official project and the 2026.8.1 breaking migrations, but its listed version-specific snapshots stopped at April. Detailed integration claims below therefore use **tag-pinned 2026.8.2 source documentation**, avoiding accidental adoption of newer live-documentation behavior.

## What belongs to which release

### The major platform expansion is 2026.8.1, marketed as OpenClaw 2.0

The August baseline added considerably more than the incremental 2026.8.2 UI changes. The important dashboard-facing capabilities are:

| Capability | Operational value for this dashboard |
| --- | --- |
| Sessions placed on local hosts, paired devices, or cloud workers | Show actual execution location, preparation state, device availability, recovery, and resource pressure |
| Goals, task ledger, Workboard, durable progress cards | Track work and blocking conditions beyond recent transcript timestamps |
| Interactive widgets, pinned session dashboards, MCP app views | Link into the native work surface; full rendering is a separate product scope |
| Conversation-bound automations, loops, exact-operation recurring approval grants | Show owners, delivery destinations, scheduling, permissions, run outcomes, and failures |
| Active Memory, grounded dreaming, Dream Diary, source ownership and forget controls | Show memory health, maintenance activity, provenance, and explicit control states |
| Runtime model catalogs, per-agent accounts, explicit `modelPolicy.allow` | Separate configured models, allowed models, catalog availability, credential readiness, and execution readiness |
| Shared credentials and environment entries in SQLite | Show sanitized credential status without interpreting a missing old JSON file as missing authentication |
| Team roles, creator/owner attribution, identity profiles | Preserve agent and operator access boundaries if sharing is enabled |
| Backup snapshots, recorded backup outcomes, Git-backed backup histories | Expose freshness, last success, failures, and restore guidance |
| Node-hosted MCP/plugin tools, scoped MCP OAuth | Distinguish Gateway connectors from tools hosted by paired nodes |
| External-supervisor mode | Recognize container/supervisor ownership when presenting service and update actions |

These capabilities and their originating changes are listed in the [2026.8.1 release](https://github.com/openclaw/openclaw/releases/tag/v2026.8.1) and [tag-pinned release documentation](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/releases/2026.8.1.md). Their existence does not imply that every feature is enabled locally.

### The incremental 2026.8.2 changes

2026.8.2 adds a docked Home conversation with inspectable work context, New Session background starts with retained placement, four optional themes, cross-session message attribution, improved session organization, readable Beam transcript links, and standalone Chrome-relay wake-up in supported extension/native-host builds. It also improves update recovery, migration safety, real heartbeat completion outcomes, cron JSON output, MCP response-size bounds, plugin capability review, and completion of tool-driven turns. The standalone browser feature has explicit build prerequisites and is not proof that the local browser supports it. [Official 2026.8.2 release](https://github.com/openclaw/openclaw/releases/tag/v2026.8.2)

The most relevant changed default is unsandboxed session visibility: other sessions belonging to the same agent become visible by default. Shared-agent operators can choose `tools.sessions.visibility: "tree"` or `"self"`; sandbox and cross-agent restrictions still apply. A dashboard should display effective visibility and avoid treating all sessions as equally visible to every operator. [2026.8.2 changes](https://github.com/openclaw/openclaw/releases/tag/v2026.8.2)

### Migrations and limitations to preserve

- The 2026.8.1 route migration moves shipped `codex/*` and `openai-codex/*` references to `openai/*` while preserving runtime intent. Provider-name string matching alone cannot identify the execution backend.
- OpenProse's bundled plugin and `/prose` command were removed in 2026.8.1; retained `.prose` sources follow a separate upstream skill migration.
- Deprecated Plugin SDK subpaths remain present in **2026.8.2**, even though the release records a September 1 removal target. Do not report those paths as already removed just because that date has passed.
- The 2026.8.2 release reports a packaging limitation for standalone `@openclaw/memory-lancedb`: an optional dependency chain may still resolve an older Sharp image decoder. This is relevant only if that plugin/dependency is installed; it is not evidence of a dashboard defect.
- Migration-original cleanup is explicit and destructive. `update cleanup --dry-run` can preview retained originals, but removing them gives up rollback to those originals and requires the selected Gateway to be stopped.

Sources: [2026.8.1 release](https://github.com/openclaw/openclaw/releases/tag/v2026.8.1), [2026.8.2 release, known issues and deprecations](https://github.com/openclaw/openclaw/releases/tag/v2026.8.2).

## The main compatibility change: authoritative state moved to SQLite

The global database is `~/.openclaw/state/openclaw.sqlite`; the default agent database is `~/.openclaw/agents/<agentId>/agent/openclaw-agent.sqlite`. Global state includes control-plane registries, approvals, plugin and runtime state. Agent databases contain sessions, transcripts, memory indexes, authentication and conversation state. Task/trajectory features can have dedicated stores. Configuration and customized agent directories still affect resolved ownership. [2026.8.2 database contract](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/reference/database-schemas.md)

Automation definitions, pending state, and execution history now live in the global SQLite store. `jobs.json`, state sidecars, and `runs/*.jsonl` are imported once and renamed with `.migrated`; editing those originals is not a supported schedule update. Canonical sessions and transcripts live in the per-agent database. A collector that reads only old JSON/JSONL can report empty or stale data while OpenClaw is operating normally. [Automation storage](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/cli/cron.md), [session storage](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/concepts/session.md)

Recommendation: read runtime-owned projections using bounded CLI JSON or Gateway RPC. Do not add a private-schema SQLite reader to the dashboard. This also preserves the repository's zero-third-party-dependency constraint and avoids splitting host/container state ownership. Database schema numbers alone are insufficient compatibility evidence because compatible additions can occur without a version bump. [Database versioning contract](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/reference/database-schemas.md)

## Available 2026.8.2 integration contracts

| Dashboard concern | Supported source | Important behavior |
| --- | --- | --- |
| Sessions | `openclaw sessions --all-agents --json`; RPC `sessions.list` | CLI lists configured stores; Gateway discovery can be broader. Results are bounded; distinguish pagination from global totals |
| Transcript/history | RPC `chat.history`, `chat.message.get` | Display-normalized history suppresses internal directives and silent-token rows; use bounded pages |
| Usage and costs | `usage.cost`, `sessions.usage`, `usage.status`; CLI `gateway usage-cost` | Pass one `agentId` or `agentScope: "all"`; use timezone-aware aggregation and preserve unknown values |
| Models and accounts | `models.list`, `openclaw models status --agent <id> --json` | Runtime allowance, catalog metadata, account readiness and proven execution are different facts |
| Automation inventory | `openclaw automations list --all --json`; `cron.list`, `cron.get`, `cron.status` | Default list omits disabled jobs. Canonical status includes `disabled`, `running`, `ok`, `error`, `skipped`, `idle` |
| Automation history | `openclaw automations runs --id <job-id> --limit 50 --json`; `cron.runs` | **8.2 uses `--id` in the documented runs syntax**. Execution and delivery are separate; deliberate suppression is not a delivery failure |
| Tasks | `tasks.list`, `tasks.get` | Require `operator.read`; cancellation requires `operator.write` |
| Memory | `doctor.memory.status`, `doctor.memory.dreamDiary`; CLI `memory status --json` | Normal status can use cached readiness; explicit deep probes may invoke the embedding provider |
| Channels | `channels.status` | Distinguish configured, running, reachable and authenticated; one broken account should not erase the rest |
| Skills/tools | `skills.status`, `tools.effective` | Effective tools depend on agent/session policy; read-only MCP inventory must not force new runtime startup |
| Configuration | `config.get`, `config.schema.lookup` | Saved root-file hash, resolved revision and applied runtime hash differ |
| Updates | `update.status`, CLI `update status --json` | Some reads are admin-scoped; match the selected runtime and supervisor |
| Gateway health | `/healthz`, `/startupz`, `/readyz`; `gateway probe --json` | Liveness, started runtime, channel readiness and authenticated RPC capability are distinct |
| Backup health | `openclaw status --json` | Includes the latest backup attempt and latest successful run |

Sources: tag-pinned [Gateway protocol](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/gateway/protocol.md), [Gateway CLI](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/cli/gateway.md), [sessions CLI](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/cli/sessions.md), [automations CLI](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/cli/cron.md), [models CLI](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/cli/models.md), [backup CLI](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/cli/backup.md).

The Gateway wire protocol is **4** in 2026.8.2. Its version is separate from CLI and published SDK package versions. `hello-ok.features.methods` is a conservative feature list, not a complete callable-method inventory: for example, `sessions.usage` can be callable despite exclusion. Pair feature discovery with a maintained known-method capability table and graceful unsupported/scope errors. [Pinned protocol](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/gateway/protocol.md)

Session activity must use `hasActiveRun`, not simply an update timestamp. Optional `activeRunIds` contains the complete exact set when available: empty means idle; omission means exact identities are unavailable. Placement exposes lifecycle states and device availability. `sessions.subscribe` supplies an initial list with nonempty list parameters and then change events; subscriptions end at disconnect. History delta cursors can request a reset, which means refetching a normal tail. [Pinned session protocol](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/gateway/protocol.md)

For future writes, `config.patch` and `config.apply` require the observed `baseHash` once config exists. Destructive array replacement requires declared `replacePaths`. A transport success is not proof that the runtime applied a saved change: compare the resolved saved and applied revisions. Agent model changes also need correct scope: `models set` writes global defaults and does **not** accept `--agent`, whereas status/list/auth are agent-aware. [Pinned configuration RPC](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/gateway/configuration.md), [pinned models CLI](https://github.com/openclaw/openclaw/blob/v2026.8.2/docs/cli/models.md)

## Recommended sequencing

### Concrete read contracts verified against 8.2 source

The following are candidate requests for the runtime adapter, not evidence that every local call succeeds:

```json
{
  "method": "sessions.usage",
  "params": {
    "agentScope": "all",
    "range": "all",
    "groupBy": "family",
    "mode": "specific",
    "timeZone": "Asia/Kuala_Lumpur",
    "limit": 50,
    "includeContextWeight": false
  }
}
```

`sessions.usage` supports ranges `7d`, `30d`, `90d`, `1y`, and `all`, or explicit `startDate`/`endDate`. `groupBy` accepts `instance` or `family`. All-agent scope cannot combine with `agentId` or `key`. The handler accumulates **every matching visible row before applying the returned-session limit**, so `totals` and `aggregates` are not limited to the first 50 returned sessions. Use these authoritative totals instead of summing the visible page. `usage.cost` supports selected/all-agent scope and date interpretation, but aggregate access can be denied when operator policy hides sessions. [Usage schema](https://github.com/openclaw/openclaw/blob/v2026.8.2/packages/gateway-protocol/src/schema/sessions.ts), [usage handler](https://github.com/openclaw/openclaw/blob/v2026.8.2/src/gateway/server-methods/usage.ts)

| Read operation | Parameters and result |
| --- | --- |
| `tasks.list` | Optional `status` string/string array, `agentId`, `sessionKey`, `limit` from 1–500, `cursor`; returns `tasks` and optional `nextCursor` |
| `tasks.get` | `{ "taskId": "..." }`; returns `task` |
| Goal projection | Read `goal` on the canonical session row; objective, status, tokens used/budget and continuation-turn count are typed. There is no invented `goals.list` requirement |
| `progressCard.get` | `{ "sessionKey": "..." }`; returns `card` or `null` |
| `board.get` | `{ "sessionKey": "...", "agentId": "..." }`; `agentId` optional; native session dashboard/widgets, distinct from Workboard |
| `workboard.boards.list` | `{}`; requires the Workboard plugin to be enabled |
| `workboard.cards.list` | Optional `boardId`; plugin-owned card collection |
| `workboard.cards.stats` | Optional `boardId`; plugin-owned statistics |
| `workboard.cards.runs` | `{ "id": "..." }`; card run detail |

These read methods require `operator.read`. Workboard methods are registered by its plugin, not guaranteed core methods. Its CLI `workboard list --json` includes archived cards for compatibility, unlike default human output. Widget rendering requires a separate bounded-grant/view-ticket model; plain inventory and native navigation are the smaller first implementation. [Task schema](https://github.com/openclaw/openclaw/blob/v2026.8.2/packages/gateway-protocol/src/schema/tasks.ts), [session goal projection](https://github.com/openclaw/openclaw/blob/v2026.8.2/src/gateway/session-utils.types.ts), [goal schema](https://github.com/openclaw/openclaw/blob/v2026.8.2/packages/gateway-protocol/src/schema/sessions-goal.ts), [progress-card schema](https://github.com/openclaw/openclaw/blob/v2026.8.2/packages/gateway-protocol/src/schema/progress-card.ts), [board schema](https://github.com/openclaw/openclaw/blob/v2026.8.2/packages/gateway-protocol/src/schema/board.ts), [Workboard registration](https://github.com/openclaw/openclaw/blob/v2026.8.2/extensions/workboard/src/gateway.ts), [core method scopes](https://github.com/openclaw/openclaw/blob/v2026.8.2/src/gateway/methods/core-descriptors.ts).

The accompanying local runtime inspection reported failures in some 8.2 direct CLI task/database reads with a read-only SQLite stabilization error, while status/cron RPC reads succeeded. Therefore, CLI availability cannot be treated as collector readiness. Prefer proven Gateway projections where available, preserve errors and last-good snapshots, and verify task RPC independently before committing that panel to the first phase. A working 9.1 host collector does not validate the 8.2 container command.

### Delivery phases

1. Restore authoritative collection first: sessions, usage, automations, models, agent roster, and source identity. Fixture tests should cover migrated SQLite-backed installations, legacy files, empty inventories, timeouts, inaccessible agents, malformed output and mixed versions.
2. Add operational visibility: task states, placement, channel/MCP health, memory readiness, backup freshness, saved-versus-applied configuration, and version/supervisor identity. Preserve last-good data with freshness and explicit error status.
3. Add carefully scoped actions only after the read model is reliable: automation run/enable/disable, exact-session cancellation, and role-aware links into native setup/approval surfaces. Reuse Gateway validation, revision checks, operator scopes and operation IDs.
4. Evaluate optional product expansion separately: native-session chat parity, Home dock, Workboard editing, widgets/MCP apps, cloud provisioning and team administration. Deep links into the native Control UI provide immediate value without reproducing the entire upstream product.

This sequence is a research recommendation. The accompanying repository audit determines exact affected files and confirms which missing capabilities matter to the local deployment.

## Keep 2026.9.1-only features out of the 8.2 compatibility baseline

2026.9.1 adds Mermaid rendering, personal skill libraries, more durable Codex approvals, personal GitHub accounts, model/usage changes, configured working directories, `cron.skipMissedJobs`, `memory reset`, and CLI config compare-and-swap/dry-run flags. It also improves failed-update rollback and migration recovery. These are a separate next-version backlog, not capabilities proven for the running 8.2 container. [Official 2026.9.1 release](https://github.com/openclaw/openclaw/releases/tag/v2026.9.1)

## Confidence and limits

High confidence: release identity, publication dates, the live stable registry value, SQLite migration, and the tag-pinned documented contracts. Local enablement, runtime permissions, connector readiness and actual field coverage require the accompanying live and repository inspection. No upgrade, repair, migration, destructive cleanup, provider invocation, or configuration change was performed by this research task. Public docs and releases can evolve; implementation should retain captured fixture contracts and verify the exact selected runtime.
