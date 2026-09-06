# Candidate UI: missing-field audit

Date: 2026-09-05. Candidate: `http://localhost:8081/`, title `OpenClaw Candidate - Go 1.27.1`, selected container `openclaw`, gateway and host CLI 2026.9.1. Observations span approximately 19:28–19:43 Asia/Kuala_Lumpur. Live data changed during the audit; counts below are timestamped observations, not permanent expected values.

Status: **data-completeness gate failed; not release-certified**. This is the historical diagnosis and interactive test record. Subsequent source repairs and regression results are tracked in the [field-fix candidate record](2026-09-05-field-fixes.md); the rebuilt candidate still needs live replay. No credential change, model request, live job execution, or deployment was performed in this audit.

## Evidence and boundaries

- Computer Use exercised the actual candidate; missing PID, uptime, memory, totals and charts reproduced together. Opened all runtime inventory disclosures and inspected every visible `Unknown`, `missing`, dash and empty-field category, including table rows and detail views.
- Inspected the candidate's generated `dist/ui-candidate.fEasVv/data.json`, the dashboard collector/renderer source, and the installed OpenClaw 2026.9.1 source. Generated data is ignored runtime state, not a checked-in fixture.
- All 16 main collections reported `ready` with current successful collection timestamps. Usage caches were `fresh`, with no pending/stale files. This rules out a general collection outage, **not** field-specific omissions.
- The root binary and `dist/openclaw-dashboard-validation` had matching SHA-256 `11330714616c1c784fc217ec4be349a9fae452efb1ecf3d37e09fdfaf3b9d297` before the user started the candidate using the root binary and isolated configuration.
- Direct container CLI inspection from the agent sandbox failed to resolve the running container. This is not evidence the container stopped: the externally started dashboard continued collecting it. Direct browser navigation to `/api/system` was blocked by the browser; no alternate access was used to bypass that restriction. Its rendered fields and source were inspected instead.
- The user's latest complete Go 1.27.1 `make check` passed vet, lint, all ten race-tested packages, govulncheck, Staticcheck v0.8.1 and build. Passing tests did not detect the UI/data gaps below.

## Field-by-field disposition

| ID | Field / missing display | Evidence and cause | Required disposition |
| --- | --- | --- | --- |
| F01 | Health PID; Gateway Runtime PID | `gateway.pid=null`. Container collection returns before host process inspection; system status parsing also clears container/diagnostic-only service PIDs. Correctly avoids displaying the host/service PID, but never collects the real gateway PID. Installed 2026.9.1 `system.info` returns `process.pid`; the dashboard does not allowlist or call it. | Collect authenticated selected-runtime `system.info`, label PID scope, and retain unsupported/unavailable reasons. Verify live response before claiming repaired. |
| F02 | Health uptime; Gateway Runtime uptime | `gateway.uptime=""`. Container path skips host `ps`; UI falls back to unauthenticated `/readyz` uptime. Installed OpenClaw source omits readiness details for non-local, unauthenticated callers; port-forwarded container traffic may take that path. Exact live `/readyz` response was not independently captured. `system.info` supplies gateway `uptimeMs`. | Use selected-runtime uptime, not host process uptime. Treat probe-detail redaction as a supported condition, not a zero uptime. Container-forwarding explanation remains an inference pending raw response verification. |
| F03 | Health memory; Gateway Runtime memory | `gateway.memory=""`, but `runtimeHealth.processMemory.rssBytes` was 710,041,600 bytes in one snapshot and later 741,175,296 bytes. Runtime Health visibly displays it. Both summary cards read the old `gateway.memory`/`versions.gateway.memory`, not this value. | **Confirmed mapping defect:** format the selected runtime RSS in both summary cards and preserve freshness/source. Do not substitute host RAM or heap-used memory for process RSS. |
| F04 | Today's Cost | Fresh usage snapshot marks `costComplete=false`. At 11:30 UTC, 25 entries lacked prices: MiniMax M3 20; Mimo 2.5 Pro 5. Known subtotal was $0.01054293, not the total. | Explain incomplete pricing beside the card; display a clearly labelled known subtotal if desired. Resolve exact provider/model prices in the selected runtime, then recollect. Never present the subtotal as full spend. |
| F05 | All-Time Cost | At 11:30 UTC, 34 unpriced entries: MiniMax M3 28; Mimo 2.5 Pro 6. Known subtotal $0.04770753. | Same pricing repair as F04, covering historical entries and runtime cache recalculation. |
| F06 | Projected Monthly | Deliberately null unless today's cost is complete; otherwise a projection would extrapolate only the tiny priced subset. | Keep unavailable until a complete base exists, with an explicit dependency explanation. Do not extrapolate incomplete cost. |
| F07 | Cost Breakdown / donut legend | Two independent causes: incomplete prices **and** `applyRuntimeUsage` unconditionally assigns `costBreakdown=nil` for All and `costBreakdownToday=nil` for Today, even for complete pricing. | **Confirmed collector defect:** construct model breakdowns from runtime aggregates when complete; preserve explicit partial/unavailable semantics otherwise. Add a complete-price regression test. |
| F08 | Daily Cost Trend, 7d and 30d | Renderer refuses null daily totals. Collector nulls every daily total when the entire 30-day cost aggregate is incomplete, even if some individual days/models are priced. | Preserve the integrity warning. Add reliable per-day completeness if the upstream contract supports it; do not imply complete costs from partial aggregates. |
| F09 | Cost by Model chart | Unknown model costs correctly cannot become zero-sized priced slices. Current chart refuses the incomplete set entirely. | Show which models lack pricing; optionally separate known-cost subtotal from unpriced models, without representing partial shares as whole-spend percentages. |
| F10 | Token trends absent from Charts & Trends | All 30 daily buckets exist with token/message data; seven nonzero days were observed. The UI only implements cost charts, so incomplete prices blank the whole trends section despite usable token data. | Add a token-based trend view/fallback independent of cost completeness. This is a presentation capability gap, not missing token collection. |
| F11 | Model COST cells | MiniMax M3 and Mimo 2.5 Pro rows have `cost=null`, `missingCostEntries>0`. OpenClaw's usage resolver treats positive-token entries without known rates or recorded billed cost as missing, rather than free. | Pricing/configuration follow-up. The dashboard is preserving the upstream unknown correctly. Exact active rate configuration was not independently inspected inside the container; do not claim keys/rates are absent merely from this output. |
| F12 | Model USAGE bars empty | `renderTokenTbl` computes bar width from `u.cost`, substituting zero for unknown. Models with hundreds of thousands of tokens therefore receive empty bars. | **Confirmed misleading UI mapping:** calculate token usage bars from raw tokens, or explicitly label them as cost bars and show unpriced state. |
| F13 | TOTAL input/output/cache/token cells blank | `renderTokenTbl` deliberately emits an empty `colspan=4` cell. Raw per-model token components and gateway totalTokens exist. | **Confirmed renderer omission:** sum raw components and format once; verify against authoritative totals. Clarify whether cache includes read and write. |
| F14 | Cron MODEL dashes | Mapper reads only explicit `payload.model`; no override is supplied for the displayed jobs. Command/maintenance jobs do not necessarily perform inference; agentTurn jobs may inherit a model. | Distinguish `Not applicable`, `Inherited`, and `Not reported`. Resolve inheritance only with the job's correct agent/runtime policy; do not assign the main model to all job types. |
| F15 | Cron LAST RUN absent (backup, heartbeat) | Backup history dialog returned zero runs; heartbeat is disabled and has no last-run timestamp. | `Never run` or `Not reported`, not a generic dash. No need to execute a live job merely to populate this field. |
| F16 | Cron NEXT RUN absent (heartbeat) | Job is disabled and next-run timestamp absent. | `Disabled / not scheduled` is the appropriate explanation. |
| F17 | Cron DURATION misleading 0.0s for never-run jobs | Mapper type-asserts absent `lastDurationMs` to zero and always emits an integer. UI cannot distinguish missing duration from a real zero-duration run. | **Confirmed missing-to-zero defect:** preserve null/absence and render `Not run`/`Not reported`; retain genuine zero values. |
| F18 | Automation session CONTEXT % and TOKENS | Projected session contains null totals and context percentage. `mapRuntimeSession` intentionally withholds absent or `totalTokensFresh=false` counts; percentage also needs a positive context limit. Other two sessions have values. | Explain `not reported/stale token count`; obtain raw selected session freshness to distinguish the exact upstream branch. Do not populate current session context from aggregate historical usage or isolated child-run totals. |
| F19 | Agent hierarchy empty while Active Sessions says 3 | All three session rows are `active=false`, so the hierarchy's active-only filter yields none. Top card counts stored sessions, not active runs. | **Confirmed label/count mismatch:** show total sessions and active sessions separately. Empty hierarchy is valid for idle sessions. |
| F20 | Goal state and Tokens / Budget Unknown for all three sessions | No `goal` object exists; UI already says `No goal`. It passes absent goal fields to generic `runtimeValue`, producing Unknown. | Use `Not applicable — no goal`. Do not infer a budget or create a goal. |
| F21 | Progress cards / widget data empty | Main and automation session detail requests returned ready, no progress card and zero widget rows. Workboard plugin is disabled. | Explicit empty state is correct; identify no card/no widgets rather than suggest collection failure. Enabled-workboard actions need separate fixtures. |
| F22 | Memory Embedding Ready Unknown | Payload has `embedding.checked=false`, `ok=false`; renderer intentionally suppresses the misleading false when no embedding probe ran. | `Not checked`. Index files/chunks do not prove the current embedding endpoint works. A separately authorized runtime probe is needed for a readiness verdict. |
| F23 | Channel Probe OK Unknown | Collector explicitly requests `channels.status` with `probe:false`; projected `probe={}`. Connected and Running are both true. | `Not probed`; do not equate this with disconnected or unhealthy. |
| F24 | Config-panel channel Connected / Health Unknown, Error dash | `agentConfig.channelStatus={}` while live `channels[0]` reports Telegram connected/running true and `hasError=false`. Config card reads only the old config-derived map. | **Confirmed consistency defect:** use live per-account state in runtime fields, keep static configuration distinct, and say `No error reported`. Do not invent a health probe result from connectivity. |
| F25 | Plugin VERSION Unknown | `active-memory`, `device-pair`, `talk-voice` lack `version` in the collected inventory. Enabled/load states are separately available. | `Version not reported`. Do not substitute gateway version unless the plugin manifest guarantees it; raw manifest inspection remains a follow-up. |
| F26 | Model REPORTED AUTH KIND `missing` for all four custom providers | Dashboard faithfully projects CLI `auth.providers[].effective.kind`. `missingProviderInUse=false`; live MiniMax response logs show HTTP 200. Installed OpenClaw uses separate CLI credential-overview and runtime-route availability logic, so these fields need not agree. Earlier logs also include secret-resolution scope failures. | **Not proof the gateway lacks credentials.** Obtain redacted selected-runtime route evidence/SecretRef-resolution metadata to distinguish CLI environment, runtime credentials and permissions. Never copy secret values into dashboard output. Exact credential-resolution root cause remains unverified from this sandbox. |
| F27 | Backup enabled / last attempt / last success Unknown | Selected diagnostics `backup={}` and config `backupConfig={}`. A scheduled backup cron is not the same thing as the core backup status object, and has zero recorded runs. Collector projection may also omit future/schema-variant fields. | Label `Not reported`; inspect raw selected-runtime status schema before declaring backups disabled/nonexistent. Show cron backup status separately; no restore guarantee without a restore test. |
| F28 | Diagnostics enabled / OpenTelemetry enabled Unknown | Selected `diagnosticConfig.enabled=null`, `otel={}`. Older summary independently coerces missing config to false. | Distinguish unset/default from explicitly false. Resolve supported defaults from selected runtime config schema; do not conflate these with measured runtime health. |
| F29 | Agent Context 1M dash | Legacy column reads `models[model].params.context1m`, not general model context capacity. That optional flag is absent. | Label as an unset override or replace with actual context window and source. Do not infer one million tokens solely from the model name. |
| F30 | Skills: No skills configured | Legacy `skills=null` reads explicit `skills.entries`; runtime inventory contains 94 skills, 61 eligible and 17 model-visible. | **Confirmed misleading empty state:** render the inventory or relabel the old section `No explicit skill overrides`, with runtime inventory counts. |
| F31 | Git: No git history | Container mode explicitly skips host git collection to avoid mixing host/container state, leaving `gitLog=null`. | `Not collected for container target`, not a claim the target has no Git history. A container-aware Git source would be a separate capability. |
| F32 | Hooks: No hooks configured; Web Search: Not configured | Projected hooks are null; selected search provider is empty, with null result/cache settings. These are configuration-only panels; plugin-owned runtime functionality may not appear in these legacy config paths. | Say `No explicit configuration reported`. Do not conclude web search or hook functionality is globally unavailable without the appropriate runtime inventory. |
| F33 | Task agent / owner-session blanks | Two imported cron-runlog tasks lack agent; 15 tasks lack owner/session. Stable task and run IDs are present. Mapper preserves optional fields. | `Not reported`/`Imported legacy run`; do not guess ownership from display text. Check raw task detail if ownership is required. |
| F34 | Empty plugin TOOLS cells | `toolNames` empty/absent; many providers/channels register no agent-facing tools. | `No tools reported` is sufficient; absence of tools does not mean the plugin failed to load. |
| F35 | Subagent token/cost subsection absent | Modern path has explicit task-derived run activity but empty legacy `subagentUsage` arrays. Session-wide tokens cannot be attributed to individual runs without a supported lineage/usage contract. | Explain attribution unavailable; never fabricate per-run costs from parent/session totals. |
| F36 | Initial No logs found; unavailable legacy log sources | Actual bounded gateway log tail subsequently loaded 200 rows. Cron/session/subagent source buttons are disabled with explanations because separate legacy files are not available in migrated runtime. | Use an initial loading state rather than falsely declaring no logs. Keep source limitations and bounded/truncated marker visible. |
| F37 | `missing scope`, pairing and database-lock messages in logs | Earlier 11:25 UTC entries show missing operator scopes/device metadata mismatch. Later collections are ready. Repeated configuration-health SQLite lock errors continue into 11:36 UTC. | Separate runtime incident investigation from display mapping. Do not automatically approve devices, grant admin scope, delete locks or restart services. Historical failures do not prove current read access is broken. |
| F38 | Automation history SESSION Unknown / DIAGNOSTICS blank | Command daily-health runs and the skill-collection maintenance run omit a session identifier; agentTurn dreaming history includes isolated run session IDs. Successful runs may have no diagnostics. | Label session `Not reported` or `Not applicable` only when guaranteed by the payload kind; blank diagnostics should mean `None reported`. Do not join unrelated sessions to manufacture a value. |

### Pricing counts changed while testing

At generated snapshot `2026-09-05 11:37:43 UTC`, Today had 680,827 tokens, 33 unpriced entries (MiniMax 28, Mimo 5); All had 821,299 tokens, 42 unpriced entries (MiniMax 36, Mimo 6). Known subtotals were unchanged. These increasing counts track live activity, not an additional dashboard defect.

## Every reported missing skill requirement

The 33 ineligible skill rows expose requirement names, not credential values. These are runtime prerequisite failures in the selected container, not missing dashboard rendering. Installing host binaries does not automatically install them inside the container. macOS-only skills cannot be made Linux-compatible merely by supplying a binary.

| Skill | Reported unmet requirement |
| --- | --- |
| 1password | binary `op` |
| apple-notes | binary `memo`; OS `darwin` |
| apple-reminders | binary `remindctl`; OS `darwin` |
| bear-notes | binary `grizzly`; OS `darwin` |
| blogwatcher | binary `blogwatcher` |
| blucli | binary `blu` |
| camsnap | binary `camsnap` |
| coding-agent | one of `claude`, `codex`, `opencode`; config `skills.entries.coding-agent.enabled` |
| eightctl | binary `eightctl` |
| gemini | binary `gemini` |
| gifgrep | binary `gifgrep` |
| gog | binary `gog` |
| goplaces | binary `goplaces`; environment `GOOGLE_PLACES_API_KEY` |
| himalaya | binary `himalaya` |
| mcporter | binary `mcporter` |
| model-usage | binary `codexbar` |
| nano-pdf | binary `nano-pdf` |
| obsidian | binary `obsidian` |
| openai-whisper | binary `whisper` |
| openai-whisper-api | environment `OPENAI_API_KEY` |
| openhue | binary `openhue` |
| oracle | binary `oracle` |
| ordercli | binary `ordercli` |
| peekaboo | binary `peekaboo`; OS `darwin` |
| sag | binary `sag`; environment `ELEVENLABS_API_KEY` |
| sherpa-onnx-tts | environment `SHERPA_ONNX_RUNTIME_DIR`, `SHERPA_ONNX_MODEL_DIR` |
| songsee | binary `songsee` |
| sonoscli | binary `sonos` |
| spotify-player | one of `spogo`, `spotify_player` |
| summarize | binary `summarize` |
| things-mac | binary `things`; OS `darwin` |
| trello | binary `jq`; environment `TRELLO_API_KEY`, `TRELLO_TOKEN` |
| xurl | binary `xurl` |

## Interactive checks completed in this pass

| Component | Result / limitation |
| --- | --- |
| Global Expand All / Collapse All | All 12 section aria-expanded states changed correctly. |
| Individual section toggles | Each of the 12 collapsed and reopened independently. |
| All six themes | Distinct background/foreground applied; Catppuccin Latte survived reload; restored Midnight. Picker stays open after selection, so the test was adjusted to observed behavior. |
| Chart 7d/30d | Both clickable; cost content remains unavailable for the reasons above. This is not a successful complete-price chart test. |
| Token usage Today/7d/30d/All | All exercised; range datasets differed; Today restored. Unknown costs remained unknown. |
| Subagent Today/7d/30d/All | All exercised; Today one run, longer ranges five; Today restored. |
| Cron history | All five job buttons tested: Dreaming five runs, daily-health seven, scheduled backup zero, skill-collection maintenance one, disabled heartbeat zero. Close button and Escape dismissal passed. |
| Session pagination | With three rows, Next/Previous remained at Page 1. Multi-page fixture still required. |
| Task status | All nine statuses and All exercised; succeeded returned 21, other statuses returned zero for this dataset. |
| Task runtime | Subagent 5, cron 15, CLI 1, ACP 0; All 21. |
| Task search | Existing name returned 7; nonexistent text returned zero; keyboard clear restored 21. Browser automation's empty-string fill did not clear, but actual Select All/Backspace did; not classified as an app defect. |
| Task pagination | Single-page boundary stayed Page 1. Multi-page correctness remains a fixture gate. |
| Session work inspection | Main and automation selection changed native URLs; both detail views returned ready empty cards/widgets. No native session was modified. |
| Runtime disclosures | Freshness, plugins, skills, models and backup panels opened and were audited. |
| Logs | Pause/resume, fast/normal, All/Gateway, all four severity choices, no-match search and invalid-regex recovery exercised. Disabled legacy sources explained their state. |
| Error feed | Recent/Frequent exercised; View expanded inline signature/timestamps/occurrences; Hide collapsed it. Counts in current tail were tied, so ordering distinctions are not fully proven by this dataset. |
| Chat | Capability response disabled input/send and displayed native fallback link. No prompt sent; enabled transport not tested. |
| Responsive | At 390px and 768px no document-level horizontal overflow; phone table horizontal scroll revealed right-side cost/usage fields. Viewport reset. |
| Browser diagnostics | Checked console error/warning snapshots were empty. No claim of exhaustive accessibility or performance certification. |

## Source pointers and repair order

1. Gateway metrics: [container legacy collector](../../internal/apprefresh/refresh_gateway.go), [runtime health projection](../../internal/apprefresh/runtime_health.go), [system process/probe parsing](../../internal/appsystem/system_service.go), and [summary renderers](../../web/index.html). Add a minimal typed, read-only `system.info` projection and explicit process scope; reuse collected RSS.
2. Cost breakdown and token displays: [runtime usage](../../internal/apprefresh/runtime_usage.go) and [renderTokenTbl / chart rendering](../../web/index.html). Tests must cover complete pricing, incomplete pricing, genuine zero, fresh vs stale tokens, and exact raw-token summation.
3. Unify skills/channel runtime display and explicit N/A states in [frontend](../../web/index.html); distinguish legacy overrides from inventory. Test empty, disabled, unreported and no-goal states separately.
4. Preserve missing cron durations in [cron mapping](../../internal/apprefresh/refresh_crons.go). Test never-run versus true zero-duration runs.
5. Runtime follow-up: inspect redacted price configuration and credential route evidence through the selected container, investigate repeated database locks, then perform requested probes only with appropriate scope. No guessed prices or security broadening.
6. Rebuild, rerun unrestricted `make check`, and replay this field matrix against the new candidate. Close no field merely because it now displays a number; source, scope and completeness must match.

The collection-ready badge needs field-level explanations: it currently means a request/projection completed, not that prices, optional metadata, probe results, or UI bindings are complete.
