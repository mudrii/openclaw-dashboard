# OpenClaw 2026.9.1 pre-release validation

Status: **not yet release-certified**. No deployment, release tag or publication was performed in this validation pass.

Latest source follow-up: [2026-09-06 field availability repairs](2026-09-06-field-reasons.md) records token freshness reasons, pricing coverage diagnostics, backup/authentication explanations, required frontend CI gates, and the new candidate artifact. Historical passes below do not certify that artifact; upstream pricing and unrestricted/live verification remain pending.

Current checkpoint: the user reran the complete Go 1.27.1 `make check` after the five post-restart fixes: vet, lint, all ten race-tested packages, vulnerability scan, Staticcheck v0.8.1 and build passed. The user then started the rebuilt root binary with the isolated port-8081 configuration. New component-by-component Computer Use testing found additional data-completeness and presentation defects; see the [field-by-field audit](2026-09-05-ui-data-field-audit.md). The UI/data gate is not passed. Native/Docker end-to-end validation, disposable action/multi-page browser fixtures, and coordinated Go build/CI pin alignment remain pending. The agent's sandbox restrictions persist.

## Scope and environment

- User requested detailed Computer Use testing, defect fixes, targeted optimization and documentation before release, with native, Docker and Podman runtime support.
- Initial browser target: existing `http://localhost:8080/`, reporting selected container `openclaw`, gateway 2026.9.1 and host CLI 2026.9.1.
- Initial observations below describe the older running binary. The later candidate section records the rebuilt Go 1.27.1 binary on port 8081. Both display dashboard version v2026.7.12; the candidate title and isolated directory distinguish them.
- Existing uncommitted implementation changes were preserved.
- No inference, embedding execution, live automation execution/disable, session abort, configuration repair, service restart or security-permission changes were performed.

## Subsequent missing-field repair candidate

Follow-up: the [live desktop field-fix replay](2026-09-05-field-fixes-ui-retest.md) now verifies the core repairs against the running Podman candidate. Remaining heading, pagination and startup-freshness findings, plus unverified release gates, are listed there. This is not a full-release pass.

The [field-fix candidate record](2026-09-05-field-fixes.md) tracks the later PID/uptime/RSS, cost breakdown, token totals/chart, channel, skill and missing-value repairs. It supersedes earlier source-status statements for those fields, but does not supersede the failed live UI gate. Rebuild and replay are required; the user's prior full-check result predates these changes.

## Defects fixed in source

| Issue | Correction | Regression evidence |
| --- | --- | --- |
| Unknown model cost rendered as a zero-dollar chart | Both cost charts require finite totals; unknown pricing renders an explicit unavailable message | Actual embedded chart functions tested with unknown and genuine zero values |
| Two visible charts reserved three columns | Two-column chart grid | Candidate: two equal 608px cards at a 1280px viewport; stacked phone cards |
| Absolute times used browser timezone | Shared formatter uses dashboard timezone, with UTC fallback | UTC, Kuala Lumpur, invalid zone and invalid date cases |
| Cron identity/time changes could miss repaint | Compare the full cron collection rather than a partial field signature | Changed next-run time and stable identity trigger updates; unchanged input skips |
| Invalid log filter did not immediately show its error | Re-render filter controls on regex change | Invalid-to-valid recovery test |
| Old log-fetch error remained after successful data | Successful payload clears obsolete error state | Failure-to-success regression |
| Log highlighting matched HTML-escaped text | Match raw text, then escape the resulting segments once | Special-character and entity cases |
| Task search omitted exact run identity | Include `runId` in search | Exact run ID regression |
| Container configuration mixed with host configuration | Read selected gateway `config.get`; no host fallback; exclude local Git and session-store labels | Native/container, invalid/missing config, source and target retention isolation |
| Container chat readiness read host configuration | Use selected gateway configuration for status and request preflight | Enabled/disabled/unavailable/null configuration cases; no token leakage |
| Runtime-health cards overflowed mobile page | Stack diagnostic cards below 768px and contain wide content | CSS contract plus candidate 390px/768px browser checks; no page overflow |

No new third-party dependency was introduced. Changes retain the embedded SPA and existing CLI adapter. Performance changes are bounded: correct dirty checks avoid redundant cron repaint, and no new polling loop or model probe was added.

## Computer Use observations

| Area | Result on running instance |
| --- | --- |
| Runtime identity and collection visibility | 2026.9.1 selected container and host CLI displayed; freshness/source metadata inspectable |
| Inventory | Two sessions and 20 loaded tasks observed; cron count changed from four to five as live data changed; disabled heartbeat retained |
| Themes | All six applied distinct foreground/background colors; restored Midnight |
| Sections | Expand/collapse all and keyboard Enter toggle checked; restored expanded state |
| Usage and sub-agent ranges | Today, 7-day, 30-day and all-time controls exercised; active states and differing all-time content checked |
| Charts | 7-day/30-day actions exercised; unknown-cost discrepancy reproduced and fixed in source |
| Task filtering | Empty search result, status filter, runtime filter, reset checked; run-ID gap covered by new regression |
| Automation history | Populated daily-health-check history and empty disabled-heartbeat history inspected; no execution |
| Session context | Progress/widget dialog and selected-session native link inspected; empty/unsupported content was explicit |
| Chat | Disabled endpoint presented an unavailable state and disabled input/send; no inference submitted |
| Logs | Pause/resume and fast/normal controls, disabled legacy sources, bounded-tail metadata inspected; stale fetch-error defect reproduced |
| Error feed | Frequent/recent controls and View/Hide detail expansion checked |
| Diagnostics | Plugin, skill, model and backup sections inspected; memory dirty/embedding unchecked and Telegram connectivity remained separate |
| Responsive layout | 768px page did not overflow; 390px page overflow reproduced and traced to runtime-health cards |
| Browser errors | No JavaScript error/warning entries reported in checked browser log snapshots |

UI actions were read-only with respect to OpenClaw. Browser viewport was reset. Live inventories and warnings are point-in-time observations, not fixed expected production counts.

## Automated checks

Passed after the fixes:

- `node scripts/frontend-regression.cjs`: eight regression groups.
- Root Go frontend regression/fixture tests.
- `go test ./... -run '^$'`: all packages compile (not a full test run).
- Race-enabled full tests for `internal/appopenclaw`, `internal/appconfig`, `internal/appruntime`.
- Race-enabled selected configuration/retention/target tests in `internal/apprefresh`.
- Race-enabled selected chat-capability and operation-authorization tests in `internal/appserver`; capability tests in `internal/appchat`.
- `make vet`, `make lint` (zero issues) and `make build` with writable temporary cache directories.

Initially blocked in the agent environment (later user-run checks and candidate UI evidence supersede the corresponding gaps):

- Full race suite / `make check`: HTTP listener creation fails with `bind: operation not permitted`. Tests were not weakened or removed.
- `make staticcheck` and `make govulncheck`: unavailable Go proxy DNS/network access prevents fetching their pinned tools.
- Updated opt-in live contract test: selected container configuration unavailable from this sandbox. Direct read-only Podman inspection confirms `connect: operation not permitted` to its localhost engine socket.
- Serving and visually validating the rebuilt candidate: cannot bind a candidate HTTP listener in this session.

## Runtime compatibility boundary

The dashboard uses OpenClaw's host `--container` routing. Inspected 2026.9.1 CLI source detects running Docker/Podman containers and rejects ambiguous same-name matches. Native mode explicitly clears inherited container selection. Engine-specific end-to-end verification cannot be inferred from shared argument/fixture tests. Container monitoring must never silently substitute host data when container access fails.

## Required release gates

1. **Passed for the current binary:** the user's latest unrestricted `GOTOOLCHAIN=go1.27.1 make check` completed vet/lint, all ten race-tested packages, govulncheck, Staticcheck v0.8.1 and build. Rerun after further implementation changes.
2. **Not passed:** the rebuilt candidate was retested on port 8081. The [missing-field audit](2026-09-05-ui-data-field-audit.md) records newly confirmed collector/renderer gaps and remaining runtime investigations. Repair and replay that matrix. Cron next-run mutation and transport failure/recovery remain regression-harness coverage, not deliberately induced live failures.
3. Podman read contract gate completed in the user's Terminal, as recorded below. Validate equivalent native and Docker targets next. Use distinct state/data directories and uniquely named containers; verify selected-runtime configuration, version, sessions, usage, tasks and logs, including inaccessible-target behavior.
4. Complete multi-page UI fixture testing and action confirmation/error paths on disposable fixtures only. Real inference, aborts, automation execution and restore tests require separately scoped approval and fixtures.
5. After those gates pass, obtain release/deployment approval and preserve the existing rollback path. This record is not authorization to restart or publish.

## User-run full-check follow-up

The user ran `GOTOOLCHAIN=go1.26.8 make check` outside the sandbox. Vet and lint passed, and all race-test packages except `internal/apprefresh` passed. The failing package reported two assertions: missing legacy sub-agent usage and an unexpected compaction mode. Scans and build were not reached because Make stopped at the test failure.

Both failures were reproduced with the inherited `OPENCLAW_CONTAINER=openclaw` environment and disappeared when that selection was cleared. Legacy fixture tests now isolate `OPENCLAW_CONTAINER` and `OPENCLAW_STATE_DIR`; explicitly selecting native mode alone is not appropriate for those pre-migration fixtures because explicit targets opt into migrated-runtime collection. The atomic-write test instead explicitly selects native mode, sets a deliberately conflicting container environment as a regression guard, and uses an unavailable fixture CLI path so it cannot contact the user's installation. No production collector behavior or assertions were weakened. The affected collector tests passed three consecutive race-enabled runs on Go 1.26.8.

The cache-corruption, cache-version and rename-failure log messages in the submitted output are deliberate negative test cases, not evidence of damaged production data.

### Go upgrade assessment

The [official release history](https://go.dev/doc/devel/release) identifies Go 1.27.1 (September 1, 2026) as the latest stable release. It is installed on this host. All packages compile with `GOTOOLCHAIN=go1.27.1`; that is not a full runtime test pass. The [Go 1.27 release notes](https://go.dev/doc/go1.27) highlight JSON implementation and timer changes that make a full regression run important for this stdlib-heavy dashboard.

Go 1.27.1 also passed three consecutive race-enabled runs of the affected collector tests, vet, lint (zero issues) and build. Full `make check` on that toolchain remains pending outside the sandbox.

The project pin remains Go 1.26.8 pending upgrade validation. A coordinated upgrade should cover `go.mod`, Docker's digest-pinned builder, Nix tooling, and the CI/release linter pin (currently v2.12.2; this host has Go-1.27-built v2.13.2). Keep historical validation results labeled with the version actually tested; do not reinterpret them as Go 1.27 release certification.

### Full Go 1.27.1 run and Staticcheck compatibility

The user's subsequent `GOTOOLCHAIN=go1.27.1 make check` passed vet, lint and all ten race-test packages. Govulncheck reported no vulnerabilities. Standalone Staticcheck v0.7.0 then failed to decode Go 1.27 export data (`version 4 is greater than maximum supported version 2`), so that run did not reach the build target.

The shared Makefile pin is now Staticcheck v0.8.1 (2026.2.1), the stable patch release of the [Go-1.27-compatible 2026.2 series](https://staticcheck.dev/changes/2026.2/). CI calls the same Make target, so it inherits the fix without duplicate version declarations. No checks were disabled and no application dependency was added. Agent-side execution still cannot fetch the tool because Go proxy DNS access is restricted. Next verification is `GOTOOLCHAIN=go1.27.1 make staticcheck build` in the user's Terminal; a clean result is required before calling the automated checks complete.

The user subsequently supplied a successful `GOTOOLCHAIN=go1.27.1 make staticcheck build` run. This closes the automated gate above. The recorded binary reports Go 1.27.1, darwin/arm64, CGO disabled.

### Prepared isolated UI candidate

- Directory: `/Users/mudrii/src/open_claw/openclaw-dashboard/dist/ui-candidate.fEasVv` (ignored build artifacts).
- Binary SHA-256: `781399647ddc0753fab75e45195e1173abe91561076ce7cd49067bebbfe6c9f6`; copied candidate matches the user's built repository binary.
- Title: `OpenClaw Candidate - Go 1.27.1`; UTC; 60-second refresh; selected container `openclaw` through the verified host CLI.
- Bind: `127.0.0.1:8081`; chat and operator actions disabled; separate generated data and cache; themes copied from repository assets.
- Configuration loaded successfully with `--version`; server not started by the agent because localhost binding remains restricted.
- Existing 8080 dashboard, service configuration and OpenClaw configuration were not changed.

Start in Terminal:

```sh
cd /Users/mudrii/src/open_claw/openclaw-dashboard/dist/ui-candidate.fEasVv
OPENCLAW_DASHBOARD_DIR="$PWD" ./openclaw-dashboard --bind 127.0.0.1 --port 8081
```

Leave that Terminal running during Computer Use validation. Stop only this foreground candidate with Ctrl-C afterward. Starting it does not install a service or deploy a release. If the port is occupied, report the error instead of stopping another process.

### Rebuilt candidate Computer Use results

The user started the isolated candidate above. Browser validation ran against `http://localhost:8081/` around 18:27–18:35 Asia/Kuala_Lumpur on 2026-09-05, with gateway and host CLI both reporting 2026.9.1. This is direct evidence for the current Podman-backed candidate, not a claim that all deployment modes or write operations passed.

| Check | Observed result |
| --- | --- |
| Runtime isolation | All 16 collection rows ready; configuration source `gateway.config.get`; compaction `safeguard`; selected default model Mimo 2.5 Pro |
| Cost accuracy/layout | Unknown totals remain Unknown; both charts show pricing/collection unavailable; two equal 608px cards at 1280px; phone cards stack |
| Responsive layout | At 390px, document scroll width 386px and diagnostic cards 346px wide in one column; at 768px, scroll width 764px and one diagnostic column. Viewport restored afterward |
| Task search/filter | Exact observed run ID returns one CLI task; unmatched text and CLI+failed combination return zero; resets restore 21 tasks |
| Time ranges | Usage/sub-agent Today, 7 Days, 30 Days and All Time controls exercised; all-time showed 44 calls and five sub-agent runs versus today's 26 calls and one run; chart 7/30-day selection works |
| Logs | Invalid `[` shows an immediate visible error; valid `gateway ready` filters to the matching line and hides the error. Pause/resume, fast/normal, All/Gateway and all four severity choices work; unavailable legacy sources are disabled; defaults restored |
| Cron history | daily-health-check returns populated execution/completion/delivery history; scheduled backup returns zero recorded runs; no job executed |
| Timezone | Cron history, error details and memory-maintenance timestamps explicitly use UTC despite the host's Kuala Lumpur timezone |
| Session context | Empty progress/widget dialog is explicit and closes correctly; choosing the Telegram session changes the native link to its matching session path; selection restored |
| Inventories | 61 plugin rows, 94 skill rows and four model-readiness rows; backup fields explicitly Unknown rather than falsely healthy |
| Sections/themes | All 12 sections collapse/expand; Enter and Space toggle Health; all six themes apply distinct backgrounds; Midnight restored |
| Error feed | Frequent/Recent and View/Hide exercised; historical gateway warnings remain inspectable with occurrence timestamps |
| Pagination boundary | Next/Previous with one available page retain Page 1 and all rows; buttons remain enabled and perform a bounded no-op at the edge. Multi-page behavior still requires a larger fixture |
| Read-only safety | Operator panel hidden; chat input/send disabled with an explicit unavailable message; mobile chat stays inside the viewport; quick prompts were not submitted; no inference or runtime mutation submitted |
| Refresh/browser health | Manual refresh advances collection timestamps; automatic refresh remains active; no warning/error entries in the checked browser console snapshots |

No additional source fix was required by these candidate checks. UI themes, ranges, log polling controls and session selection were restored to defaults; no production process or OpenClaw configuration was changed. Live counts are point-in-time evidence, not fixed assertions.

Remaining release work: validate separate native and Docker targets including inaccessible-target isolation; test multi-page and operation confirmation/error paths with disposable fixtures; align the validated Go 1.27.1 toolchain across build/CI packaging. Unknown model prices and unreported backup health are runtime-data limitations, not evidence of a completed pricing or restore check. Final deployment remains unapproved.

### User-run Podman read contract result

The user supplied a successful uncached run:

```sh
GOTOOLCHAIN=go1.27.1 OPENCLAW_DASHBOARD_LIVE=1 OPENCLAW_CONTAINER=openclaw \
  go test ./internal/apprefresh -run '^TestRuntimeLiveReadContracts$' -count=1 -v
```

`TestRuntimeLiveReadContracts` passed in 9.74 seconds (package 10.391 seconds). It validated selected-container configuration and collected three sessions, 21 tasks and 510,303 tokens; usage cache state was `fresh`. The 34 entries without pricing remain explicitly incomplete cost data, not a failed collection or verified zero cost. This test does not establish native/Docker compatibility, inference, writes, backup restoration or inaccessible-target behavior.

## Whole-working-tree audit after restart

Scope included tracked and untracked implementation files, selected-runtime routing, CLI output limits, collection state/retention, usage aggregation, pagination, operation authorization/replay records, chat capability checks, log routing, embedded frontend regressions, build declarations and current user/developer documentation. This is not a proof that every possible execution path or deployment environment is correct.

### Additional reproduced defects and corrections

| Defect | Correction and regression |
| --- | --- |
| One-shot `--refresh` refused a selected container when its state was not mounted on the host | Restrict the local-path existence check to non-container targets. Configured and inherited container cases now reach the fixture collector without a mount; native missing-state behavior remains tested |
| Inherited `OPENCLAW_CONTAINER` did not select migrated collectors without local SQLite markers | Runtime detection now considers effective container selection. A no-files fixture fails before the fix and passes afterward |
| Modern daily-cost alerts used empty legacy token buckets | Alert evaluation uses the complete current gateway Today total. An expensive complete day alerts; missing prices and warming totals do not assert a complete-cost alert |
| Stale configuration retained agent details but lost the compaction summary | Retain `compactionMode` with the same-target, same-source configuration snapshot; cross-target/source isolation assertions remain intact |
| Environment-selected container chat skipped POST capability preflight | POST checks the selected container endpoint before loading data or reaching HTTP transport. A disabled-endpoint fixture verifies one config read and a 503 capability response, without inference |

Broader tests also exposed legacy cron/log/chat fixtures inheriting live container/state environment values. Those fixtures now explicitly isolate their environment; the appserver test process clears ambient target/state selection and individual selection tests opt in using configuration or `t.Setenv`. No production fallback or existing assertion was weakened. The unrestricted live test remains opt-in and still honors `OPENCLAW_CONTAINER`.

### Fresh verification

- `make vet` and `make lint` on Go 1.27.1: passed, zero lint issues.
- All ten packages compile via `go test ./... -run '^$'`; this is compile evidence, not a full test pass.
- Full race suites passed for `appopenclaw`, `appconfig`, `appruntime` and `cmd/openclaw-dashboard`.
- `apprefresh` race suite passed with only `TestReadyzProbe_ParsesFailingFromHTTP503` excluded because it binds a local listener. Focused runtime, snapshot, retention, configuration, legacy, cache and collector tests also passed three consecutive race-enabled runs.
- `appserver` race suite passed excluding five listener tests: `TestHandleChat_SuccessHitsGateway`, `TestHandleChat_GatewayError502`, `TestHandleChat_GatewayErrorDoesNotExposeUpstreamBodyOrToken`, `TestHandleChat_SanitizesHistoryBeforeGatewayRequest`, and `TestHandleChat_BodyCapBoundary`. Runtime/authorization/capability cases passed three consecutive race-enabled runs.
- Root CLI-refresh, chat-validation and embedded-frontend cases passed under the race detector; appchat prompt/capability cases passed.
- A broad root race run passed in 33.919 seconds with explicit exclusions for listener/process-dependent tests: `TestCallGateway*`, `TestShutdownSequence`, `TestCollectOpenclawRuntime*`, `TestDetectGatewayFallback*`, `TestFetchJSONMap*`, `TestCollectVersions_GatewayNoStdoutFallsBackToHTTP_I2`, `TestHandleSystem_DegradedReturns200`, and `TestGetProcessInfo_CurrentProcess`. This subset is not the full root suite.
- Eight Node frontend regression groups and both script syntax checks passed. No frontend source change was made in this audit.
- Production-style builds passed for darwin/arm64, darwin/amd64, linux/amd64 and linux/arm64 using Go 1.27.1, CGO disabled. Outputs are isolated under `dist/openclaw-dashboard-validation*`; no service or running candidate binary was replaced. Go emitted non-fatal denied module-stat-cache writes while producing these artifacts.
- JSON configuration examples parse; local links in README, CONTRIBUTING, CONFIGURATION and RUNTIME-COMPATIBILITY resolve; Go formatting and `git diff --check` pass. Shell syntax checks pass for all three shipping scripts.

The updated darwin/arm64 artifact is `dist/openclaw-dashboard-validation`, SHA-256 `11330714616c1c784fc217ec4be349a9fae452efb1ecf3d37e09fdfaf3b9d297`. The earlier port-8081 candidate does **not** contain these additional fixes.

### Not passed / still required

- Fresh `make check` was attempted and stopped on `httptest` listener denial (`bind: operation not permitted`). The earlier user-run full suite predates the five fixes, so it cannot certify current source.
- Staticcheck and govulncheck were attempted separately; Go proxy DNS access remains unavailable. Earlier scan results are retained as historical evidence only for the source then tested.
- Shellcheck is not installed in this session; shell syntax checks are not a substitute for it.
- A broad root-package run excluding listener-dependent tests still failed two environment-sensitive checks: `TestHandleSystem_DegradedReturns200` returned 503 and `TestGetProcessInfo_CurrentProcess` returned no process metadata. Direct `ps` invocation is denied; the system endpoint returns 503 when all core collectors fail. These tests require an unrestricted rerun, not relaxed assertions.
- Native/Docker live targets, packaging execution in those environments, multi-page browser fixtures and action/paid-inference end-to-end paths remain unverified. Stock dashboard Docker images do not include the OpenClaw CLI/engine tooling required for migrated collection.
- The coordinated Go 1.27.1 Docker/Nix/CI pin upgrade remains pending; do not substitute an unverified Docker digest or claim the current Go 1.26.8 project pin has changed.

Documentation now distinguishes migrated versus legacy collection, exposes the new endpoint surface and opt-in operations, describes runtime prerequisites/container selection, includes new configuration keys in the full example, and accurately describes frontend test coverage and the Staticcheck pin. Historical screenshots/release evidence are labeled instead of being presented as current certification. No release, deployment, service restart or live OpenClaw mutation was performed.
