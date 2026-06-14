# openclaw-dashboard — Fixes & Integration Plan

Comparison of openclaw-dashboard against the openclaw repo at
`/Users/mudrii/src/open_claw/openclaw` (latest tag `v2026.6.9-alpha.4`, stable
`v2026.6.6`). Investigation-only output: **3 fixes** + **5 new-feature
integrations**, each validated twice (dashboard feasibility + openclaw contract
accuracy + assembled-plan revalidation).

> Status: ✅ COMPLETE (2026-06-14). All 8 tasks shipped on `feature_fix` (INT-1,
> FIX-1, INT-2, FIX-2, FIX-3, INT-4, INT-3, INT-5). FIX-2 and FIX-3 were found
> already implemented pre-plan (FIX-3 gained its missing precedence test). Every
> tick was TDD + `make check`-green + atomic commit. Frontend panels and several
> live-data behaviors are **pending human visual check / runtime-verify** — see the
> human-gated remainder at the end of `LOOP_STATE.md`. Original analysis below is
> retained for traceability; line numbers reflect the branch at analysis time.

---

## Principles & constraints (apply to every task)

- TDD on implementation: characterization test first, behavior via public/seam
  interface, never pin known bugs.
- Zero third-party deps (stdlib only); no `require` block / no `go.sum`.
- Root package `dashboard` = thin facade re-exporting `internal/app*`. Add a root
  wrapper **only** when a new *exported* internal symbol/signature crosses the
  boundary.
- `CGO_ENABLED=0` across all build paths; loopback-only (127.0.0.1 / local CLI OK).
- Frontend (`web/index.html`) is `//go:embed`-compiled — UI changes need a rebuild.
- Every task keeps `make check` green: `vet`, `golangci-lint` (0 issues),
  `test -race`, `govulncheck`.

## Recommended sequence (dependency-ordered)

1. **INT-1** — channel health from `/readyz` (near-free; fixes channel status properly)
2. **FIX-1** — Linux journald logs (only real user-facing break)
3. **INT-2** — `status --json` rich blocks (biggest new surface)
4. **FIX-2**, **FIX-3** — cheap correctness
5. **INT-4** — live model catalog (kills maintenance debt)
6. **INT-3**, **INT-5** — polish (optional)

## Summary table

| ID | Type | Sev | Effort | Facade | Frontend rebuild |
|----|------|-----|--------|--------|------------------|
| INT-1 | feature | — | S–M | none | yes |
| FIX-1 | fix | MED | M | none | no |
| INT-2 | feature | — | M | none | yes |
| FIX-2 | fix | LOW | S | none | no |
| FIX-3 | fix | LOW | XS | none | no |
| INT-4 | feature | — | M | none | no |
| INT-3 | feature | — | S | none | maybe |
| INT-5 | feature | — | S | none | yes |

---

# FIXES

## FIX-1 — Linux logs via journald · Sev MED

**Problem.** On Linux/systemd the Logs panel and error-signature alerts are empty.
The dashboard tails `<OPENCLAW_HOME>/logs/gateway.log` with a macOS-only fallback
(`~/Library/Logs/openclaw`). openclaw's systemd unit emits no `StandardOutput=`
directive, so gateway output goes to **journald** — no file exists to tail.

**Evidence.**
- Dashboard `internal/apprefresh/logtail.go`: `ResolveLogPath` (L513-519),
  `candidateLogPaths` (L567-580), `ReadMergedLogs` (L67-109, merges every existing
  candidate keyed by path+ModTime), `defaultLogFallbackRoots` (L532-546, macOS path
  on all platforms), overridable via `SetLogFallbackRoots` (L551). **No exec seam in
  the log path** — pure file I/O. Empty state = `ReadMergedLogs` returns `nil,nil`.
- openclaw: gateway is systemd **--user** unit `openclaw-gateway.service`
  (`daemon/constants.ts:5,43-48`); `buildSystemdUnit` (`daemon/systemd-unit.ts:67-95`)
  emits no `StandardOutput=`; openclaw's own hint is
  `journalctl --user -u openclaw-gateway.service -n 200 --no-pager`
  (`daemon/runtime-hints.ts:26`). Override env `OPENCLAW_SYSTEMD_UNIT`;
  `OPENCLAW_PROFILE` appends a suffix.

**Design.** New platform-tagged collector. On Linux, when a source's file path is
absent, fetch `journalctl --user -u <unit>.service -o json --no-pager -n <N>` via a
**new injectable exec seam** (mirror `session_model_cache.go`'s `execCommandContext`
var), parse each JSON line (reuse `parseJSONLLine`/`classifySeverity`), synthesize
`LogRecord{Source,Raw,Timestamp}` (journald has no path/ModTime), and merge into
`ReadMergedLogs`'s `perSourceRecords` bypassing `os.Stat`.

**Source→unit mapping (resolve in task).** Add `Logs.SystemdUnit` config field
(default `openclaw-gateway`), honor `OPENCLAW_SYSTEMD_UNIT`.

**Files.** `internal/apprefresh/logtail_journald_linux.go` (new) +
`logtail_journald_other.go` (stub) · `logtail.go` (merge hook) ·
`internal/appconfig/config.go` (`LogsConfig` +`SystemdUnit`, additive — confirmed
`LogsConfig` at config.go:57-68) · `logtail_test.go` + new `_linux` test (stub the
runner; assert journald JSON `PRIORITY`→severity, `MESSAGE`, `__REALTIME_TIMESTAMP`).
**Facade:** none.

**Edge cases.** journald JSON differs from the gateway's keys — map `PRIORITY`
(int 0-7) → severity, `__REALTIME_TIMESTAMP` (µs) → time. No `journalctl` binary →
graceful empty. `OPENCLAW_PROFILE` shifts unit name.

**Acceptance.** Linux: logs panel populates from journald. macOS: unchanged. Both
build under `CGO_ENABLED=0`. Tests cover PRIORITY mapping + missing binary.

---

## FIX-2 — Per-agent models from `agents.list[]` · Sev LOW

**Problem.** Per-agent model overrides (`agents.list[].model.primary`) are never
surfaced. The function that resolves per-agent default models iterates an obsolete
config shape, so only the global default ever shows.

**Evidence (corrected in revalidation).** `loadAgentDefaultModels`
(`internal/apprefresh/refresh_sessions.go:92-129`) is **NOT dead** — it is called at
`:207` in `collectSessions` and feeds `getSessionModel` (`:131-142`, used at
`:316`/`:320`). It works today for `main/work/group` via the
`agents.defaults.model.primary` backfill (L123-127). The bug: its inner
`for name,v := range agents` loop (L109) iterates the obsolete top-level
`agents.<name>` map, which never matches openclaw's `{defaults, list[]}` shape
(`zod-schema.agents.ts:7-13`, `.strict()`), so per-agent overrides are silently
dropped. `parseAgents` (refresh_config.go:443-526) already parses `agents.list[]`
correctly for the agents panel — a separate path.

**Design.** **Enhance, don't delete.** Add an `agents.list[]` pass (keyed by
`entry.id`; `model` is `string | {primary,fallbacks,timeoutMs}` per
`zod-schema.agent-model.ts`); **keep** the main/work/group backfill (session-key
channel names ≠ agent ids — the bridge must stay); preserve the `map[string]string`
return shape `getSessionModel` depends on. Reuse `parseAgents` output rather than
re-parsing where practical.

**Files.** `internal/apprefresh/refresh_sessions.go` (rewrite
`loadAgentDefaultModels`) · tests (list[] string-model, object-model, `default:true`;
main/work/group still backfilled; default-path unchanged). **Facade:** none.

**Acceptance.** Custom `agents.list[].model.primary` appears per agent; default path
unchanged; `getSessionModel` behavior preserved.

---

## FIX-3 — Cron status precedence · Sev LOW (cosmetic today)

**Problem.** Dashboard prefers the **deprecated** `lastStatus` over the canonical
`lastRunStatus`. Dual-written today (values agree); breaks if openclaw drops the alias.

**Evidence.** `internal/apprefresh/refresh_crons.go:118-121` reads `lastStatus` first
(default `"none"`), with an **explicit comment** stating the legacy-continuity intent.
Emit key `"lastStatus"` (L153) is unaffected by read precedence. openclaw:
`lastRunStatus` canonical, `lastStatus` `@deprecated` alias, both dual-written
(`cron/types.ts:212-214`, `cron/service/timer.ts:678-679`); openclaw's own readers do
`lastRunStatus ?? lastStatus` (`cli/cron-cli/shared.ts:68`).

**Design.** Flip to `lastRunStatus` first, `lastStatus` fallback, keep `"none"`
default. **Reverses a deliberate choice** — document rationale (align with openclaw
canonical + its own readers; future-proof against alias removal). No present-day
behavior change.

**Files.** `internal/apprefresh/refresh_crons.go:118-121` · test (both keys →
resolves `lastRunStatus`; only `lastStatus` → still works). **Facade:** none.

---

# NEW FEATURES (integrations)

## INT-1 — Live channel-health panel (from `/readyz failing[]`) · highest value/effort

**What's new.** Real per-channel up/down driven by the gateway's own readiness,
replacing the session-activity heuristic. Idle-but-healthy channels stop looking
dead; actually-failing channels (bad token, API down) show red.

**Evidence (corrected).** `gw.Failing []string` is populated in **appsystem**
(`system_service.go:548`, via `CollectOpenclawRuntime` L438) — a different
collector pass/package than channel rows, which are built in **apprefresh**
(`parseChannels` refresh_config.go:247-312 → `channelStatus`; `backfillChannelConnectivity`
refresh_sessions.go:356, called refresh.go:249). `gw.Failing` is **not available**
where channels are built. apprefresh's `refresh_gateway.go` has only a **`/healthz`**
probe (`healthzProbe`), **not** `/readyz`; the only `/readyz` probe is in appsystem.
openclaw `/readyz` = `{ready, failing:string[], uptimeMs, eventLoop?}`
(`gateway/server/readiness.ts:12-17`); `failing` entries are channel plugin IDs
(`slack`,`telegram`) matching channel config keys, **but** can also contain
non-channel reasons (`"startup-sidecars"`, `"internal"`).

**Design.** Keep it in-package: **add a new `/readyz` probe in apprefresh**
(model it on `healthzProbe`, with an injectable client/func var for testability —
no seam exists today), extract `failing[]`, pass into `backfillChannelConnectivity`.
For each channel whose config key ∈ `failing[]` → `connected=false, health="unhealthy"`.
**Filter** non-channel tokens. On probe failure, fall back to the existing heuristic
(don't blank channels).

**channelStatus shape** (confirmed): `{enabled, configured, connected, health, error}`
(refresh_config.go:303-309).

**Files.** `internal/apprefresh/refresh_gateway.go` (new readyz probe + seam) ·
`refresh_sessions.go` (`backfillChannelConnectivity` takes `failing []string`) ·
`refresh.go:249` (wire) · `web/index.html` (channel row health badge, rebuild) ·
apprefresh tests (stub probe; channel in failing→unhealthy; `"startup-sidecars"`
ignored; probe failure→heuristic). **Facade:** none.

**Edge cases.** Extension channels (`.passthrough()` — nostr/matrix) may have
plugin-id ≠ config-key; match what you can, leave rest to heuristic. `/readyz` 503
still returns a body — parse anyway.

---

## INT-2 — Runtime-health + task-queue panel (from `status --json`) · biggest new surface

**What's new.** An ops view the dashboard lacks entirely: live task queue, event-loop
health, plugin-compat warnings, last heartbeat. The dashboard already shells
`status --json` for version only and discards the rest.

**Evidence (corrected).** Invoked `system_service.go:467`; parsed
`parseOpenclawStatusJSON` (L599-621); `SystemOpenclawStatus` (system_types.go:95-100)
keeps only version/latency/security — rich blocks dropped. Extension is additive.
openclaw exact shapes:
- `tasks` (**lean**, in `StatusSummary`) = `{total, active, terminal, failures,
  byStatus{queued,running,succeeded,failed,timed_out,cancelled,lost},
  byRuntime{subagent,acp,cli,cron}}` — counts nested under `byStatus`/`byRuntime`.
- `eventLoop` (**deep-only**, under `HealthSummary`) = `{degraded, reasons[],
  intervalMs, delayP99Ms, delayMaxMs, utilization(0-1), cpuCoreRatio}` —
  **not** `hz`/`lag`.
- `pluginCompatibility` = `{count, warnings:string[]}` (strings, not objects;
  omitted when empty).
- `lastHeartbeat` (**deep-only**) = `{ts, status, channel, …}`.
- `channelSummary` = `string[]` (pre-formatted lines; `[]` in lean status).

**Design.** Extend `SystemOpenclawStatus` with typed structs for known shapes
(`eventLoop`, `tasks` with `omitzero`/`omitempty`) and `map[string]any` for loose
blocks (`pluginCompatibility`, matching the existing `Security` precedent). Extend
`parseOpenclawStatusJSON` additively (back-compat: minimal JSON must still parse).
`eventLoop`/`lastHeartbeat` require the **deep** path → add a config toggle
(`System.DeepStatus`, default off) since deep status is slower. `eventLoop.degraded`
semantics are churny across releases → render the shape, never hard-gate logic on it.
Frontend: new SystemBar tiles (task counts, event-loop utilization gauge,
plugin-warning badge, last-heartbeat age).

**Files.** `internal/appsystem/system_types.go` (extend struct) ·
`internal/appsystem/system_service.go` (extend `parseOpenclawStatusJSON`; optional
deep flag) · `internal/appconfig/config.go` (optional `DeepStatus` toggle) ·
`web/index.html` (render + rebuild) · `system_service_test.go` (rich + minimal JSON
parse). **Facade:** none (struct fields propagate via existing alias).

---

## INT-3 — Robust gateway process metadata (lock file) · polish

**What's new.** Correct PID/uptime/RSS on every install type (homebrew, binary, bun,
source), not just npm. Liveness itself is already correct via `/healthz`; this fixes
the metadata gap only.

**Evidence.** Dashboard pgrep pattern `openclaw/dist/index.js gateway`
(refresh_gateway.go:23) matches only the npm/container layout (npm-global is actually
`dist/entry.js`). openclaw publishes an install-independent lock file
`os.tmpdir()/openclaw-<uid>/gateway.<sha256(configPath)[:8]>.lock` carrying `{pid,…}`
(`infra/gateway-lock.ts`).

**Design.** Read the lock file (+ PID-liveness check) for pid/uptime/RSS; keep
`/healthz` as the liveness signal and pgrep as a last-resort fallback.

**Files.** `internal/apprefresh/refresh_gateway.go` · tests. **Facade:** none.

---

## INT-4 — Live model catalog (dynamic names + context windows) · maintenance win

**What's new.** Model display names and context-window limits come from openclaw, not
a frozen hardcoded list — correct names for every current/future model, accurate
context-usage bars.

**Evidence.** `ModelName` (`internal/apprefresh/refresh.go:89-151`) is a hardcoded
substring switch pinning point versions; callers in refresh.go + token_usage_cache.go
+ root re-export (refresh.go:29). openclaw drifts model ids (now rewrites
`gemini-3-pro`→`gemini-3.1-pro-preview`). `openclaw models list --json` →
`{count, models:[{key,name,input,contextWindow,contextTokens,local,available,tags,
missing}]}` — id field is **`key`** (no separate provider/alias). `/v1/models`
returns bare ids only → unsuitable. `session_model_cache.go` is a clonable
TTL + injectable-runner pattern.

**Design.** New `model_catalog_cache.go` mirroring `session_model_cache.go`: TTL
cache fed by `openclaw models list --json`, injectable runner. `ModelName` consults
the cache (`key`→`name`) and **falls back to the hardcoded switch** on miss/error.
**Do not change `ModelName`'s signature** (pure, no-ctx, many callers + stable
facade) — add a package-level cache it reads, or a new resolver it calls.
`contextTokens`/`contextWindow` become a better source for `lookupModelLimits`.

**Files.** `internal/apprefresh/model_catalog_cache.go` (new) · `refresh.go`
(`ModelName` consults cache) · `model_catalog_cache_test.go` (stub runner, TTL,
fallback-on-error). **Facade:** none (signature stable).

---

## INT-5 — Cron delivery + flapping view (richer sidecar state) · polish

**What's new.** See whether scheduled jobs actually *delivered* output and which jobs
are unstable — today the panel shows only last status.

**Evidence.** `jobs-state.json` (`jobs[id].state`) also carries `lastDeliveryStatus`
(delivered/not-delivered/unknown/not-requested), `consecutiveErrors`,
`consecutiveSkipped`, `lastDiagnostics[]` (`cron/types.ts:207-235`) — all ignored by
the dashboard.

**Design.** Parse the extra state fields in `refresh_crons.go`; surface a delivery
badge + flapping indicator (consecutiveErrors ≥ N).

**Files.** `internal/apprefresh/refresh_crons.go` · `web/index.html` (badges,
rebuild) · tests. **Facade:** none.

---

# Cross-cutting notes

- **Build tags:** only FIX-1 needs platform-tagged files (`_linux.go` + stub);
  apprefresh has no existing platform split — new structure.
- **Facade:** none of the 8 tasks require facade edits (all unexported logic or
  additive struct fields under existing aliases). Re-verify if any new *exported*
  internal symbol is introduced.
- **Caching/exec seams:** FIX-1 and INT-4 reuse the proven `session_model_cache.go`
  injectable-runner + TTL pattern for deterministic tests without real shell-out.
  INT-1 must add a new injectable client for its `/readyz` probe (none exists).
- **Deep status:** INT-2 — `eventLoop`/`lastHeartbeat` are deep-only; gate behind a
  config toggle (default lean) to avoid slowing the status call.
- **Stability (openclaw surfaces):** `/readyz`, cron schema, agents schema,
  `models list --json` are safe to depend on. `eventLoop.degraded` shape is stable
  but its trigger semantics churn release-to-release → treat as best-effort/optional.

# Open decision

- INT-2 lean-vs-deep `status --json` tradeoff: recommend a `System.DeepStatus`
  config toggle defaulting to **lean** (tasks/plugin-compat free; event-loop/heartbeat
  opt-in).

# Investigated — no change needed (ruled out)

Documented so they aren't re-investigated. Each was a pass-1 candidate demoted by
revalidation (real on one side but already mitigated, or premise incorrect).

- **Channel connected/health read from `openclaw.json`** — those runtime keys never
  exist in config (intent only); but `backfillChannelConnectivity` already derives
  status from session activity. Not broken. Proper upgrade = INT-1.
- **`models.providers.*.models[].contextWindow/maxTokens` + `context1m`** — openclaw
  *does* define `models.providers.<p>.models[]` (`ModelsConfigSchema`); the dashboard's
  `lookupModelLimits` matches and returns nil only when the user leaves it unset
  (stock configs rely on the internal catalog) — an honest null already handled.
  `context1m` is a free-form Anthropic-1M-beta param, read as optional pass-through.
  (Low-confidence: passes disagreed on `providers` schema; a `grep ModelsConfigSchema`
  settles it. INT-4 supersedes this by sourcing context from the live catalog.)
- **Version paths in `status --json`** (`currentVersion`/`latestVersion` top-level) —
  hit wrong nested paths, but version is fully covered by `openclaw --version` + npm
  registry fallbacks (refresh.go:223-228). Only `connectLatencyMs` cosmetically reads
  0 (no fallback).
- **pgrep `dist/index.js gateway`** — misses non-npm installs, but `/healthz` is the
  authoritative liveness signal; pgrep supplies only pid/uptime/RSS metadata. INT-3
  upgrades the metadata path.
- **Telegram stream mode** — dashboard reads canonical
  `channels.telegram.streaming.mode` correctly; legacy `streamMode` fallback is
  harmless defensive code.

# Validation trail

1. Pass 1 — parallel contract mappers (openclaw surfaces + dashboard consumption).
2. Pass 2 — verification of each candidate fix against openclaw source (refuted/
   qualified ~6 over-claims).
3. Pass 3 — end-to-end cross-validation against dashboard fallbacks (demoted most
   "breakages" to non-issues; left 3 real fixes).
4. Plan round-1 validation — dashboard feasibility + openclaw contract accuracy
   (corrected ~6 specifics: cited lines, field shapes, schema trees).
5. Plan round-2 revalidation — corrected 3 more: INT-1 probe is `/healthz` not
   `/readyz` (must add one); INT-2 `eventLoop` is deep-only; FIX-2 function is live
   (enhance, don't delete).

All findings survived only if real on both sides AND not already mitigated by an
existing fallback.
