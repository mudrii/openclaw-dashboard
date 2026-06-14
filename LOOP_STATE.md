# LOOP_STATE — openclaw-dashboard autonomous dev loop

Dated cycle log. Source of truth for task design = PLAN.md.
Sequence: INT-1, FIX-1, INT-2, FIX-2, FIX-3, INT-4, INT-3, INT-5.

---

## 2026-06-14 — INT-1 (channel health from `/readyz failing[]`) — DONE (backend), UI needs human visual check

**Task.** INT-1 — live per-channel up/down driven by the gateway's `/readyz`
`failing[]`, replacing the session-activity heuristic for actually-failing channels.

**TDD slices (RED→GREEN, all real behavior).**
1. `backfillChannelConnectivity` gains `failing []string`; a failing channel is
   forced `connected=false, health="unhealthy"`, overriding the activity
   heuristic. + non-channel token (`startup-sidecars`) creates no spurious entry.
2. `parseReadyzFailing([]byte) []string` — pure body parser (extract `failing[]`,
   tolerate missing key / empty array / malformed JSON → nil). 4 sub-cases.
3. Wiring: `collectDashboardData` calls new `readyzProbe(ctx, GatewayPort)` seam,
   feeds `failing` into backfill. White-box integration test stubs the probe and
   asserts `data.json` channelStatus.telegram → unhealthy end-to-end.

**Files.**
- `internal/apprefresh/refresh_gateway.go` — new `readyzProbe` func-var seam
  (mirrors `healthzProbe`; 503 body parsed; 1MB LimitReader; port<=0 → ok=false)
  + `parseReadyzFailing` typed-struct parser.
- `internal/apprefresh/refresh_sessions.go` — `backfillChannelConnectivity` takes
  `failing`; authoritative failing→unhealthy override before heuristic.
- `internal/apprefresh/refresh.go` — wire probe at the backfill call site; probe
  failure → nil failing → heuristic fallback (no channel blanked).
- `web/index.html` — Health value colored by state (unhealthy→red, healthy/active→green).

**Seam/fallback.** Injectable `readyzProbe` func-var (no real network in tests).
Probe failure or port 0 falls back to the existing session-activity heuristic.
Non-channel `failing` tokens ignored implicitly (only `channelStatus` keys mutated).

**Facade.** None (all unexported logic; no exported internal symbol crossed).

**Tests delta.** +4 test funcs (TestParseReadyzFailing [4 subtests],
TestBackfillChannelConnectivity_FailingMarksUnhealthy,
TestBackfillChannelConnectivity_NonChannelFailingTokenIgnored,
TestRunRefreshCollector_ReadyzFailingMarksChannelUnhealthy). Existing
TestBackfillChannelConnectivity_MarksActive updated to 3-arg signature (nil failing).

**Review.** Self-adversarial pass (correctness/PLAN/quality/tests/repo). No
Critical/Important findings. Minor: `readyzProbe` port<=0 branch + real-HTTP paths
not unit-tested (thin glue; parser + fallback covered) — accepted.

**Gate.** `make check` green (vet, golangci-lint 0 issues, `test -race` all pkgs,
govulncheck clean). `gofmt -l` empty. `make build` OK (frontend re-embedded).
No platform-tagged code → no GOOS=linux pass needed.

**NEEDS HUMAN VISUAL CHECK.** `web/index.html` channel-card Health coloring — backend
data verified by tests + binary rebuilt, but the rendered badge color is not
visually validated.

**Remaining.** FIX-1 (next), INT-2, FIX-2, FIX-3, INT-4, INT-3, INT-5.
graphify NOT updated (known false-deletion gotcha — graph left stale intentionally).

---

## 2026-06-14 — FIX-1 (Linux journald logs) — PARTIAL (1/2): journald line parser

**Task.** FIX-1 — on Linux/systemd the gateway logs to journald (no file to tail),
so the Logs panel + error alerts are empty. Fix = synthesize log records from
`journalctl --user -u <unit> -o json`.

**This slice (RED→GREEN).** Pure, platform-independent journald JSON line parser —
the highest-risk logic, isolated and fully testable on darwin:
- `parseJournaldLine(line) (LogRecord, bool)` — `MESSAGE`→message; `PRIORITY`
  (syslog 0-7 string) → severity via `journaldPrioritySeverity` (0-3 error, 4
  warn, 5-6 info, 7 debug), falling back to `classifySeverity(message)` when
  PRIORITY absent/out-of-range; `__REALTIME_TIMESTAMP` (µs string) → time +
  TimestampMs + SeenAt. ok=false on malformed JSON / missing MESSAGE / empty.

**Files.** `internal/apprefresh/logtail_journald.go` (new, non-tagged pure parser)
· `logtail_journald_test.go` (new). Reuses existing `classifySeverity`/`LogRecord`.

**Tests delta.** +5 test funcs (PriorityMapping [8 subtests], FieldsExtracted,
MissingPriorityFallsBackToMessage, MissingTimestampStillParses, Degenerate).

**Facade.** None.

**Planned deviation (next slice, justified).** PLAN calls for `_linux.go` + non-linux
stub build-tag split. Intend to SKIP build tags: `journalctl` is invoked through
`exec.CommandContext` (a runtime string command) which compiles on every GOOS and
fails gracefully (missing binary → empty) on non-Linux; the only platform concern
is *whether to attempt the probe*, handled by a single `runtime.GOOS=="linux"`
guard at the merge site. A tag split would add a file + stub for zero compile-level
benefit (no platform-specific imports/syscalls) — simpler per CLAUDE.md. Acceptance
(macOS unchanged via GOOS guard; Linux populates) still met. Re-confirm at wiring.

**Next slice (2/2).** exec seam `journaldRunner` (func-var, stubbed in tests) +
`collectJournaldRecords` + merge hook in `ReadMergedLogs` (gated on GOOS=linux,
only when a source has no file records) + additive `Logs.SystemdUnit` config field
(honor `OPENCLAW_SYSTEMD_UNIT`, `OPENCLAW_PROFILE` suffix; default openclaw-gateway).
ReadMergedLogs signature stays stable (exported/faceted). Then flag runtime-verify
on live Linux/systemd.

**Gate.** `make check` green (vet, lint 0, `test -race` all pkgs, govulncheck clean),
`gofmt -l` empty. No exec/embed change → no `make build` needed this slice.

**RUNTIME-VERIFY (deferred).** End-to-end journald population needs live openclaw on
Linux/systemd — not loop-observable; will ship code + stubbed tests and flag.

---

## 2026-06-14 — FIX-1 (Linux journald logs) — DONE (code), runtime-verify on live Linux

**Slice 2/2.** Wired the journald fallback end to end behind injectable seams.

- `journaldRunner` (func-var exec seam) — `journalctl --user -u <unit>.service -o
  json --no-pager -n <N>`; stubbed in tests, graceful error→nil (missing binary).
- `journaldEnabled` (func-var GOOS gate, default `runtime.GOOS=="linux"`) — tests
  force-enable to exercise the Linux path on darwin.
- `collectJournaldRecords(ctx, unit, source, limit)` — runner → split → parseJournaldLine
  → Source/Raw set; skips blank/unparseable lines.
- `ResolveSystemdUnit(configUnit)` — precedence OPENCLAW_SYSTEMD_UNIT (verbatim) >
  config > default `openclaw-gateway`; OPENCLAW_PROFILE appends `-<profile>` to the
  non-override forms.
- Merge hook in new `ReadMergedLogsWithUnit(...)` (ReadMergedLogs delegates with
  env/default unit — back-compat, signature stable): when a source has no file on
  disk AND journald enabled, synthesize records from journalctl (5s ctx timeout).
- `Logs.SystemdUnit` config field (additive, omitempty); appserver `readMergedLogs`
  resolves + passes the unit via `ReadMergedLogsWithUnit`.

**Files.** `logtail_journald.go` (+seam/collector/resolver) · `logtail.go`
(ReadMergedLogs→WithUnit split + merge hook, +context import) · `appconfig/config.go`
(+SystemdUnit) · `appserver/server_logs.go` (wire) · `logtail_journald_test.go` (+tests).

**Tests delta.** +5 funcs this slice (CollectJournaldRecords_ParsesLines,
_RunnerErrorYieldsNil, ReadMergedLogsWithUnit_JournaldFallback,
_DisabledSkipsJournald, ResolveSystemdUnit_Precedence [4 subtests]).

**Deviation taken (as planned).** No `_linux.go`/stub build-tag split — journald
invoked via `exec.CommandContext` (compiles every GOOS), platform choice handled by
the `journaldEnabled` GOOS gate. `GOOS=linux go build ./...` clean. Simpler, acceptance
met (macOS: gate skips shell-out, verified by _DisabledSkipsJournald; Linux: populates).

**Facade.** None — `ReadMergedLogsWithUnit`/`ResolveSystemdUnit` exported from
apprefresh but not root-faceted (neither is `ReadMergedLogs`).

**Gate.** `make check` green (vet, lint 0, `test -race` all pkgs, govulncheck clean),
`gofmt -l` empty, `GOOS=linux go build ./...` ok. No embed change → no `make build`.

**RUNTIME-VERIFY (flagged).** Real journald population on live Linux/systemd not
loop-observable. Code + stubbed tests shipped; needs human/runtime check on a Linux
host with openclaw-gateway.service running.

**Remaining.** INT-2 (next), FIX-2, FIX-3, INT-4, INT-3, INT-5.

---

## 2026-06-14 — INT-2 (runtime-health + task-queue panel) — PARTIAL (1/3): backend types + parse

**Task.** Surface the rich blocks the dashboard already discards from
`openclaw status --json`: task queue, event-loop health, plugin-compat warnings,
last heartbeat, channel summary. Decision SETTLED: lean default + System.DeepStatus
toggle (this slice does the parse; toggle is next slice).

**Slice 1/3 (RED→GREEN).** Extend `SystemOpenclawStatus` + additive parse:
- New typed `SystemOpenclawTasks` {total,active,terminal,failures,
  byStatus/byRuntime map[string]int} and `SystemOpenclawEventLoop`
  {degraded,reasons[],intervalMs,delayP99Ms,delayMaxMs,utilization,cpuCoreRatio}.
- `SystemOpenclawStatus` gains `*Tasks`, `*EventLoop` (pointer+omitempty → absent
  blocks omitted), `PluginCompatibility`/`LastHeartbeat` map[string]any (loose,
  matches Security precedent), `ChannelSummary []string`.
- `parseOpenclawStatusJSON` extended additively via generic `decodeStatusField[T]`
  (re-marshal sub-object → typed struct; malformed block → nil, never fails the
  whole parse). Loose maps pass through; channelSummary via stringSliceFromAny.

**Value now.** tasks/pluginCompatibility/channelSummary are lean-status blocks →
already flow to /api/system through SystemResponse.Openclaw.Status. eventLoop /
lastHeartbeat are deep-only → need the toggle (next slice) to appear.

**Files.** `appsystem/system_types.go` (+2 types, +5 fields) ·
`appsystem/system_service.go` (additive parse + decodeStatusField) ·
`system_helpers_test.go` (+2 subtests: rich blocks parsed, minimal→nil back-compat).

**Tests delta.** +2 subtests under TestParseOpenclawStatusJSON.

**Facade.** None — new types reached through the already-aliased SystemOpenclawStatus.

**Gate.** `make check` green (vet, lint 0, `test -race`, govulncheck), `gofmt -l` empty.

**Next slices.** (2/3) `System.DeepStatus` config toggle gating a deep flag on the
`openclaw status --json` invocation (exact deep CLI flag = runtime-verify). (3/3)
web/index.html SystemBar tiles: task counts, event-loop utilization gauge,
plugin-warning badge, last-heartbeat age → make build + human visual check.

---

## 2026-06-14 — INT-2 — PARTIAL (2/3): System.DeepStatus toggle

**Slice 2/3 (RED→GREEN).** Opt-in deep status invocation.
- Confirmed via openclaw source (`cli/profile.test.ts`, `cli/channels-cli.ts`):
  the flag is `openclaw status --deep` (adds event-loop + last-heartbeat).
- `statusArgs(deep bool)` helper — `[status --json]` / `[status --json --deep]`
  (pure, unit-tested).
- `CollectOpenclawRuntime` gains a trailing `deepStatus bool`; invocation uses
  `statusArgs(deepStatus)`. Threaded from `s.cfg.DeepStatus` (s.cfg IS SystemConfig).
- Root facade wrapper `collectOpenclawRuntime` signature updated to match
  (deliberate facade change — CollectOpenclawRuntime is root-re-exported).
- `SystemConfig.DeepStatus` config field (additive, omitempty, default false = lean).

**Files.** `appsystem/system_service.go` (statusArgs + param + wire) ·
`appconfig/config.go` (+DeepStatus) · `system_service.go` (root wrapper) ·
`system_helpers_test.go` (+TestStatusArgs) · `system_test.go` (4 existing call
sites get trailing `false` — mechanical compile fix).

**Tests delta.** +1 func (TestStatusArgs). 4 existing root tests updated to new arity.

**Facade.** Root wrapper signature updated (CollectOpenclawRuntime re-exported).

**Gate.** `make check` green (vet, lint 0, `test -race`, govulncheck), `gofmt -l` empty.

**RUNTIME-VERIFY (flagged).** `--deep` flag name confirmed in openclaw source but the
deep payload shape (eventLoop/lastHeartbeat presence) needs a live `openclaw status
--json --deep` run to confirm end to end.

**Next slice (3/3).** web/index.html SystemBar tiles (task counts, event-loop
utilization gauge, plugin-warning badge, last-heartbeat age) → make build + human
visual check.

---

## 2026-06-14 — INT-2 — DONE (code), frontend needs human visual check

**Slice 3/3.** Runtime Health card in web/index.html.
- New full-width "📊 Runtime Health" panel (#runtimeHealthPanel) above Agent Bindings.
- SystemBar.render populates it from `d.openclaw.status.{tasks,eventLoop,
  pluginCompatibility,lastHeartbeat,channelSummary}`: task active/total + failures +
  byStatus breakdown; event-loop degraded/healthy + utilization% (color-graded) +
  delay p99 + reasons; plugin-warning count; last-heartbeat age; channel summary.
- Each block optional → renders only when present; lean status shows a hint to
  enable System.DeepStatus for event-loop/heartbeat.
- Built with `replaceChildren` + per-row `textContent` (NOT innerHTML) so all
  server-derived strings are inert — satisfies the repo XSS hook and is genuinely
  injection-safe (diverges from the file's `_kv`/innerHTML convention deliberately).

**Files.** `web/index.html` (card markup + SystemBar render).

**Facade/tests.** None (pure presentation; backend data + parse already tested in 1/3).

**Gate.** `make build` OK (frontend re-embedded), `make check` green.

**NEEDS HUMAN VISUAL CHECK.** Runtime Health card layout/colors not visually validated.
**RUNTIME-VERIFY.** Live data population needs a running openclaw (lean for tasks/
plugin/channels; `--deep` for event-loop/heartbeat).

**INT-2 COMPLETE** (3/3 slices). **Remaining.** FIX-2 (next), FIX-3, INT-4, INT-3, INT-5.

---

## 2026-06-14 — FIX-2 (per-agent models from agents.list[]) — ALREADY DONE (PLAN stale)

**Finding.** FIX-2's production fix already exists: `loadAgentDefaultModels`
(refresh_sessions.go:123-137) already has the `agents.list[]` pass keyed by
`entry.id`, `agentModelPrimary` already handles both `string` and `{primary}`
model shapes (146-155), and the main/work/group backfill (138-142) is preserved.
Implemented in earlier audit work (pre-loop), so PLAN.md (which assumed the
list[] loop was missing) is stale here.

**Coverage.** Already fully tested — `agent_default_models_test.go:144`
("agents list string and object models populate per-agent defaults") covers
PLAN's exact design: list[] string-model, object-model, `default:true`, plus
main/work/group backfill. Default path covered by sibling subtests.

**Action.** None (no production change; verified all 8 subtests pass). Marked DONE.

---

## 2026-06-14 — FIX-3 (cron status precedence) — DONE (code pre-existed; added precedence test)

**Finding.** Production fix already in place: `refresh_crons.go:116-121` reads the
canonical `lastRunStatus` first, falls back to deprecated `lastStatus`, defaults
to `"none"` — exactly FIX-3's design. Pre-existing from earlier audit work.

**Gap closed.** The distinguishing precedence branch was UNtested — existing cron
tests only seed the legacy `lastStatus` input key, never `lastRunStatus`, so the
canonical-wins behavior had no coverage. Added
`TestCollectCrons_LastRunStatusPrecedence` (3 subtests): canonical lastRunStatus
wins over lastStatus, fallback to lastStatus when canonical absent, `"none"` default
when neither present. Meaningful (distinguishes old vs new precedence).

**Files.** `cron_state_test.go` (+1 test func, 3 subtests). No production change.

**Gate.** `make check` green (vet, lint 0, `test -race`, govulncheck), `gofmt -l` empty.

**Remaining.** INT-4 (next), INT-3, INT-5.

---

## 2026-06-14 — INT-4 (live model catalog) — PARTIAL (1/2): display-name catalog

**Task.** Source model display names from openclaw (`models list --json`) instead of
a frozen hardcoded switch, so current/future model ids get real names. Contract
confirmed against openclaw source (`commands/models/list.model-row.ts`): rows are
`{key, name, contextWindow?, contextTokens?, …}`, id field is `key`.

**Slice 1/2 (RED→GREEN).** Display-name catalog.
- `parseModelCatalog(out)` — pure parser; key→name; accepts `{count,models:[…]}`
  and bare `[…]`; skips rows without a name; malformed → empty.
- `modelCatalogCache` — TTL + singleflight cache mirroring `liveSessionModelCache`,
  injectable `runner`/`resolveOpenclaw`; `models list --json`, 10s timeout, 5m TTL.
- `modelCatalogNames atomic.Pointer[map]` snapshot + `setModelCatalogNames` /
  `catalogDisplayName` — lock-free read so ctx-free pure `ModelName` can consult it.
- `refreshModelCatalog(ctx,now,ttl)` wired into `collectDashboardData` (before
  sessions/token usage use ModelName); publishes only when non-empty (failed fetch
  keeps prior snapshot).

**KEY DESIGN DEVIATION (justified, vs PLAN's "cache-first").** ModelName consults
the catalog in its **default branch only** — curated switch names win, catalog fills
the unknown-model gap. Reason: caught by a real test failure — openclaw's `name` for
many models is the bare id (e.g. `gpt-5.3-codex`), so cache-first REGRESSED curated
names ("GPT-5.3 Codex" → "gpt-5.3-codex"). Curated-first preserves quality AND adds
catalog names for ids the switch doesn't know — the actual INT-4 intent.

**Files.** `model_catalog_cache.go` (new) · `refresh.go` (ModelName default-branch
consult + refresh wiring) · `model_catalog_cache_test.go` (new).

**Tests delta.** +4 funcs (ParseModelCatalog [4 subtests], ModelName_ConsultsCatalog,
ModelCatalogCache_FetchWithStubRunner, _RunnerErrorYieldsEmpty).

**Facade.** None — `ModelName` signature unchanged (pure, no ctx); catalog symbols
unexported.

**Note (test global).** Collector tests shell out to real `openclaw` and may publish
a catalog snapshot; benign because ModelName is curated-first (no unit test asserts a
default-case raw-id passthrough for a live key). `-race` suite green.

**Gate.** `make check` green (vet, lint 0, `test -race`, govulncheck), `gofmt -l` empty.

**RUNTIME-VERIFY.** Live name resolution needs a running openclaw with `models list
--json`. **Next slice (2/2).** Feed `contextWindow`/`contextTokens` from the catalog
into `lookupModelLimits` for accurate context-usage bars.
