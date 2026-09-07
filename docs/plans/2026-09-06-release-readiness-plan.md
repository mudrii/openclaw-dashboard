# Release readiness implementation plan — 2026-09-06

Branch: `release-readiness` (from `c593d0d`). Seven workstreams run in parallel in the same working tree. Each workstream owns a disjoint set of files.

## Global constraints (binding for every task)

- Zero third-party dependencies. `go.mod` has no `require` block. Stdlib only. Node scripts use only `node:` built-ins.
- **Do not run `git add`, `git commit`, `git stash`, `git checkout`, `git worktree`, or anything that mutates git state.** The controller commits. Other agents are editing other files in this tree concurrently.
- **Edit only the files listed under your task's "Files owned". If you believe you must touch another file, stop and report NEEDS_CONTEXT with the reason.** Creating a new test file inside your owned package is allowed.
- Run only your own package tests while iterating (`go test ./internal/<pkg>/...`), then `go vet ./...` and `golangci-lint run ./...` once before reporting. Do not run `make check` (other agents are running in parallel; the controller runs the full gate).
- Follow CLAUDE.md: `%w` wrapping, lowercase error strings, context first parameter, early returns, table-driven tests with `t.Run`, `t.Context()`, no panics for expected failures, doc comments on every exported name starting with the name.
- Invariant: **container monitoring must never silently substitute host data when container access fails.** Payloads must say `unavailable`, `partial`, or `stale` with an error code rather than a plausible value.
- Never log or serialize secrets. Use `appopenclaw.Redact` for any CLI output that reaches a log line.
- Every behavior change gets a test that fails before and passes after (TDD). Put RED/GREEN evidence in your report.

Go toolchain note: the host has Go 1.27.1. Task 1 changes `go.mod` to `go 1.27`. Other tasks may see the directive change mid-run; that is expected and harmless.

---

### Task 1: Go 1.27.1 coordinated pin, CI Node setup, docs scrub

Files owned: `go.mod`, `Dockerfile`, `ops_contract_test.go`, `.github/workflows/tests.yml`, `.github/workflows/release.yml`, `flake.nix`, `docs/INFRA-CHECKLIST.md`, `CONTRIBUTING.md`, `CLAUDE.md`, `.claude/rules/go-idioms.md`, `.claude/skills/go-rig/SKILL.md`, `AGENTS.md`, `docs/RUNTIME-COMPATIBILITY.md`, everything under `docs/plans/` except this plan file.

Requirements:

1. `go.mod`: `go 1.27` and `toolchain go1.27.1`.
2. `Dockerfile` line 24: `FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS builder` (verified Go 1.27.1, multi-arch index digest).
3. `ops_contract_test.go`: update the golangci-lint install string to `@v2.13.2`, the tag list to `golang:1.27-alpine`, and the pinned digest string to match the Dockerfile exactly. Run `go test -run 'TestPackagingDocs|TestReleaseWorkflow|TestCIUses|TestGoReleaser|TestWorkflowActions' .` and make it pass.
4. `.github/workflows/tests.yml` line 31 and `release.yml` line 49: golangci-lint `v2.13.2` (first release with Go 1.27 support is v2.13.0).
5. Add an `actions/setup-node` step to the `test` job in `tests.yml` and to the `release` job in `release.yml`, before the first step that runs `make`. Pin to a full commit SHA with a `# vN` comment, like the other actions (the contract test `TestWorkflowActionsAreImmutablePinned` requires a 40-hex SHA). Use Node 22 LTS: `with: node-version: "22"`. Find the current SHA for `actions/setup-node` v5 (or latest major) with `gh api repos/actions/setup-node/git/ref/tags/<tag>`; resolve annotated tags to the commit object.
6. `flake.nix`: `go = pkgs.go_1_27 or pkgs.go;` with comment updated to "Go 1.27+", and add `pkgs.nodejs` to `devShells.default.buildInputs` (Node is required by `make frontend-test`). Do not touch `vendorHash`.
7. `docs/INFRA-CHECKLIST.md`: replace all four `golang:1.26-alpine` mentions with `golang:1.27-alpine`.
8. `CONTRIBUTING.md` line 70: `CI currently uses v2.13.2`. Also mention that CI installs Node via `actions/setup-node`.
9. `CLAUDE.md`: line 25 becomes `Go 1.27 (toolchain \`go1.27.1\` per \`go.mod\`), ...`; line 54 heading `## Go 1.27 Idioms`; add a row to the Internal Packages table: `| \`appopenclaw\` | Bounded access to the selected OpenClaw runtime: CLI target selection, gateway RPC reads, allow-listed operations, secret redaction |`.
10. `.claude/rules/go-idioms.md`: bump headings to 1.27; under Language add generic methods and struct-literal field-selector keys (one line each); in the modernizer list remove `fmtappendf`, rename `waitgroup` to `waitgroupgo`, add `atomictypes` (`int32`+`atomic.AddInt32` to `atomic.Int32`), `embedlit`, `slicesbackward`, `unsafefuncs`.
11. `.claude/skills/go-rig/SKILL.md` line 20 and `AGENTS.md` line 99: 1.26 to 1.27.
12. `docs/RUNTIME-COMPATIBILITY.md` around line 86: rewrite the paragraph to state the coordinated pin is now Go 1.27 / toolchain go1.27.1 across go.mod, Dockerfile, Nix, and CI.
13. Replace every `/Users/mudrii/...` path under `docs/plans/*.md` (nine occurrences; find with `grep -rn '/Users/mudrii' docs/plans`) with `$HOME/...` or `<repo>/...` equivalents. Do not change meaning.
14. Verify: `GOTOOLCHAIN=go1.27.1 go vet ./... && go test -count=1 . -run 'Contract|Contracts|Pinned|Canonical|Parity|Checklist|Packaging|Nix|Installer|ReleaseRequires'` passes, and `go mod tidy` leaves go.mod unchanged apart from your edit.

---

### Task 2: Container gateway status must not use host probes

Files owned: `internal/appsystem/system_service.go`, `internal/appsystem/system_runtime_test.go`, `internal/appsystem/gateway_compat_test.go`, `internal/apprefresh/refresh_gateway.go`, `internal/apprefresh/runtime_gateway_test.go`, `internal/apprefresh/refresh_gateway_test.go`.

Defect A, `internal/appsystem/system_service.go` around lines 440-449 (function that collects versions and gateway status): `gwOut, _ := runOpenclawWithTimeout(...)` drops the error, and when the parsed status is `unknown` it unconditionally calls `DetectGatewayFallback(ctx, gatewayPort, timeoutMs)`, which does an HTTP probe of `127.0.0.1:<port>`. In container mode (`appopenclaw.TargetFromContext(ctx).IsContainer()`) that probe reports the host, not the container.

Required behavior:
- When the target is a container, never call `DetectGatewayFallback`. Leave `Status: "unknown"` and set a new field on `SystemGateway` (owned file `system_types.go` is NOT yours; check whether `SystemGateway` already has a usable field such as `ProcessScope` or an error/reason string. If it has none and you need one, report NEEDS_CONTEXT naming the field you want; the controller will decide.) If a reason field exists, populate it with the error code from `appopenclaw.ErrorCode(err)` when the CLI call failed.
- Keep native behavior unchanged.
- Test in `system_runtime_test.go`: container target on ctx, fake CLI binary emitting empty stdout, HTTP client swapped for a transport whose RoundTrip calls `t.Errorf("host probe must not run in container mode")`; assert `Status == "unknown"`. Also add `t.Setenv("OPENCLAW_CONTAINER", "")` to `TestCollectVersionsLocal_FallbackHTTP` so it stays native regardless of the developer's environment.

Defect B, `internal/apprefresh/refresh_gateway.go` around lines 139-156: in the container branch, `status` is derived from `healthzProbe(127.0.0.1:port)`. An unpublished container port yields `offline` for a healthy container.

Required behavior:
- In container mode do not call `healthzProbe` at all. Set `gw["status"] = "unknown"` and `gw["processScope"] = "container"`, and add `gw["statusReason"] = "host_probe_not_applicable"` (string). Do not set `pid`, `uptime`, or `memory` from host data (already the case; keep it).
- Rewrite `TestRuntimeGatewayNeverUsesHostPIDForContainer` in `runtime_gateway_test.go`: the `healthzProbe` stub must call `t.Errorf` if invoked; assert `status == "unknown"`, `processScope == "container"`, `statusReason == "host_probe_not_applicable"`, and `pid == nil`.
- Check `refresh_gateway_test.go` and `gateway_compat_test.go` for tests that encode the old behavior and update them.

Note: the frontend (Task 6) will render `statusReason`. Runtime health from `collections.runtimeHealth` remains the authoritative liveness signal in container mode; you do not need to touch it.

---

### Task 3: Server hardening — chat gate, log cache, errors endpoint, operations pruning, tests

Files owned: `main.go`, `main_cli_test.go`, `main_shutdown_test.go`, `internal/appserver/server_chat_handler.go`, `internal/appserver/server_chat_capability_test.go`, `internal/appserver/server_chat_handler_test.go`, `internal/appserver/server_runtime_logs.go`, `internal/appserver/server_runtime_logs_test.go`, `internal/appserver/server_logs.go`, `internal/appserver/server_logs_test.go` (create if absent), `internal/appserver/server_operations.go`, `internal/appserver/server_operations_test.go`, `internal/appserver/server_runtime.go`, `internal/appserver/server_runtime_test.go`, `internal/appserver/server_core.go`, `internal/appserver/main_test.go`.

3.1 Chat credential gate. `internal/appserver/server_chat_handler.go:64` wraps the capability check in `if s.cfg.Openclaw != (appopenclaw.Target{}) || s.cfg.Openclaw.IsContainer()`. Remove the condition so `s.chatCapability(r.Context())` runs for every install. Confirm `chatCapability` handles the native, no-target case (it reads the selected config; in native mode that is the host `openclaw.json`). If `chatCapability` returns `Available=false` with `State` `credentials_missing` when `s.gatewayToken == ""`, good; otherwise add that check before the gateway call. Test in `server_chat_capability_test.go`: native target (env `OPENCLAW_CONTAINER` cleared), `AI.Enabled=true`, gateway token empty, `openclaw.json` with chat completions enabled; POST `/api/chat` must return 503 with `errorCode` `credentials_missing` and must not dial the gateway (point `GatewayPort` at a listener whose accept loop calls `t.Errorf`). Second case: token present, endpoint disabled, expect 503 `endpoint_disabled`.

Also in `main.go` around line 183: keep monitoring alive without a token (that is intentional), but when `cfg.AI.Enabled` is true and the token is empty, log at `slog.Warn` with a message that names the consequence: chat requests will be refused with `credentials_missing`. Remove the now-dead `DASHBOARD_AI_TOKEN_OPTIONAL=1` from `main_cli_test.go:119` (and anywhere else in owned files).

3.2 Runtime log cache, `internal/appserver/server_runtime_logs.go`. Two problems: the cache timestamp is set even when the read failed because the caller's context was cancelled, so all pollers get a 502 for 5 s; and the CLI exec runs while `mu` is held. Required: (a) do not cache a result whose error is a context cancellation or deadline of the *caller* (`errors.Is(err, context.Canceled)` or `context.DeadlineExceeded` originating from `ctx`); (b) run `apprefresh.ReadRuntimeLogs` with a detached, bounded context (`context.WithTimeout(context.WithoutCancel(ctx), <existing per-call bound>)`) so one client's disconnect does not poison others, using single-flight semantics (only one exec in flight; concurrent callers wait for it) without holding the mutex during exec. Test in `server_runtime_logs_test.go`: first request with a pre-cancelled context, second immediate request with a healthy context must invoke the runner again and return 200.

3.3 `/api/errors`, `internal/appserver/server_logs.go` around line 193: on a modern runtime the handler returns 500 `failed to read logs`. Map the error through the same `runtimeReadError` helper `handleLogs` uses (permission_denied 403, unsupported 501, timeout/other 502, with `errorCode` in the body). Add a test.

3.4 Operations audit directory, `internal/appserver/server_operations.go` around lines 103-117: `<dir>/operations/<uuid>.jsonl` files are never pruned. Add retention: after writing, if the directory holds more than 500 files, delete the oldest by modification time down to 500. Make the limit a named constant `maxOperationAuditFiles`. Log a `slog.Warn` if pruning fails; do not fail the request. Test it.

3.5 Tests for uncovered server paths: `runtimeReadError` mapping (403/501/502) in `server_runtime_test.go`; `/api/workboard` handler happy path and error; operations outcomes: write RPC failure yields `outcome: unknown` and 502, `result.ran == false` yields `not_applied`, disabled job yields 409, wrong content type 415, rate limit 429, unknown JSON field 400. Reuse the existing fake runner patterns in `server_operations_test.go`.

---

### Task 4: Collector honesty — cron fallback, agent list, per-agent errors, stale cap, task row limit

Files owned: `internal/apprefresh/refresh.go`, `internal/apprefresh/runtime_health.go`, `internal/apprefresh/runtime_health_test.go`, `internal/apprefresh/collection_state.go`, `internal/apprefresh/collection_state_test.go`, `internal/apprefresh/runtime_snapshot.go`, `internal/apprefresh/runtime_snapshot_test.go`, `internal/apprefresh/cron_fallback_test.go`, `internal/apprefresh/refresh_collector_test.go`, `internal/apprefresh/field_reasons_test.go`.

Do not change any exported function signature in `apprefresh`; `internal/appserver` and the root package depend on them. Do not edit `refresh_gateway.go` (Task 2 owns it) or `appopenclaw` (Task 5 owns it).

4.1 Legacy cron fallback, `refresh.go` around lines 419-421: when `cronErr != nil && !modern && crons != nil`, the collection is published as `collectionStatus("legacy.cron.files", nil, true)`. Change to `collectionStatus("legacy.cron.files", cronErr, false)` and make sure the resulting status has `state: "partial"` (check `collectionStatus` / `CollectionStatus` semantics in `collection_state.go`; if a non-nil error always yields `unavailable`, add an explicit partial constructor). The rows stay. Extend `TestCollectDashboardData_CronCLIFailureFallsBackToFile` in `cron_fallback_test.go` to assert `collections["crons"]` is not `ready`, has `source == "legacy.cron.files"`, and carries the error code.

4.2 Guessed agent list, `refresh.go` around lines 383-385: when `readSelectedConfig` failed, `agentIDs` defaults to `["main"]` and memory, skill, and model collections report `ready`. Required: if `configErr != nil`, still collect for `["main"]` (keeps the dashboard useful) but publish `memory`, `skillInventory`, `modelReadiness` as `partial` with error code `configuration_unavailable` and `complete:false`. Add a test next to `TestDashboardRuntimeCostAlerts` in `runtime_snapshot_test.go` (or `refresh_collector_test.go`): `config.get` returns `{}`, every other RPC valid; assert `collections["configuration"].State == "unavailable"` and the three dependent collections are `partial`.

4.3 Per-agent errors, `runtime_health.go` lines 21-61: the loop overwrites `memoryErr`/`skillErr` each iteration and publishes rows from agents that succeeded under an `unavailable` status. Required: track failures per agent; if some agents succeed and some fail, publish state `partial`, `complete:false`, error code of the first failure, and add `failedAgents []string` to the status payload (extend `CollectionStatus` in `collection_state.go` with `FailedAgents []string \`json:"failedAgents,omitempty"\``). If all fail, `unavailable`. Test with agents `[a,b,c]` where `b` fails `permission_denied` and `c` fails `timeout`.

4.4 Stale retention cap, `collection_state.go` lines 45-85: `retainLastGoodCollections` chains indefinitely. Add `const maxStaleAge = 24 * time.Hour`; if the previous snapshot's `collectedAt` for that collection is older than the cap (or unparsable), do not retain: leave the collection `unavailable` and do not copy values. Test both sides of the boundary.

4.5 Task row limit, `runtime_snapshot.go` around line 227: `collectRuntimeTasks` returns `rows, fmt.Errorf("tasks collection reached row limit")`, which becomes `unavailable` and discards the rows. Make it match the sessions collector at line ~100: return the rows with `Complete=false` and nil error so the collection is `partial`. Test: fixture returning 100 unique ids per page with fresh cursors for more pages than the limit; assert rows are kept, error nil, and through `collectDashboardData` the tasks collection is `partial`.

---

### Task 5: appopenclaw — server-side diagnostics logging, error wrapping, doc comments, Redact tests

Files owned: everything under `internal/appopenclaw/`.

Do not change any exported signature (Task 4 and the server depend on them). Adding new exported helpers is fine.

5.1 Logging. `readError.Error()` returns only the code, and nothing in the runtime path logs. Required: in `Client.Read` (and the operations write path) when a CLI call fails, emit `slog.Warn("[openclaw] collection failed", "method", method, "code", code, "target", target.Effective(), "stderr", redactedTail)` where `redactedTail` is the last 512 bytes of combined output passed through `Redact`, with newlines collapsed. Keep `readError.Error()` unchanged (browser payloads must stay code-only). Make sure `Redact` is applied before any byte reaches the logger. Test: capture `slog` output via a `slog.NewTextHandler` writing to a buffer set as the default logger inside the test (`slog.SetDefault` with `t.Cleanup` restore); assert the method and code appear and a planted secret (`token=abc123secret`) does not.

5.2 `operations.go` around line 33: wrap the marshal error: `fmt.Errorf("encode %s params: %w", method, err)`.

5.3 Doc comments on every exported identifier in the package (`MaxOutputBytes`, `Target`, `Validate`, `WithTarget`, `TargetFromContext`, `IsContainer`, `CommandContext`, `ErrorCode`, `CommandError`, `DecodeJSON`, `Client`, `Read`, `ReadCommand`, `ReadAgentModels`, `Redact`, and any others `grep -nE '^(func|type|var|const) [A-Z]'` reveals). Comments start with the identifier name. Fix comments that exist but do not start with the name.

5.4 Tests: `Redact` table test covering `sk-...` keys, `"apiKey":"..."` JSON, `password=`, `Authorization: Bearer ...`, case-insensitivity, and false-positive guards (`totalTokens=100`, `tokens: 12`, `secretOwners: 3` must survive unchanged). `ReadAgentModels` rejects `../x`, `--flag`, and a 129-character id without executing (runner that calls `t.Errorf` if invoked). `ReadCommand` unknown operation path. `Output` classification when `unauthorized` is on stderr with exit 1 yields `permission_denied`.

---

### Task 6: Frontend — sticky error banner, stale indicators, error-feed keys, log poll restart, a11y, harness coverage

Files owned: `web/index.html`, `scripts/frontend-regression.cjs`, `web_runtime_test.go`, `web_contract_test.go`, `docs/plans/2026-09-06-frontend-fixes.md` (new, optional notes).

Frontend is vanilla JS/CSS embedded via `go:embed`; no build step; no dependencies. The regression harness `scripts/frontend-regression.cjs` loads slices of `web/index.html` into a `node:vm` context and must keep using only `node:` built-ins. Run it with `node scripts/frontend-regression.cjs` and the Go side with `go test -count=1 -run 'Frontend|Web' .`.

6.1 Sticky error banner. `App.refresh()` catch (around line 2662) writes `Failed to load data.json` into `#alertsSection`, but that section only re-renders when `flags.alerts` is true, which a failed fetch never triggers. Required: render fetch failures into a dedicated element (add `<div id="fetchError" role="alert" hidden></div>` near `#alertsSection`), show it on failure, hide and clear it on the next successful refresh. Do not write into `#alertsSection` from the catch. Harness test: simulate failure then success and assert the element toggles.

6.2 Stale/unavailable indicators. Backend `collections[name].state` is one of `ready|partial|stale|unavailable` with `errorCode`, `source`, `collectedAt`. Panels that currently read retained fields with no state check: Cron Jobs table and `#cronCount` (around 1645-1666, collection `crons`), cost cards and donut (1618-1642, `usageToday`/`usageAll`), charts (2107-2123, `usage30d`), health row `#hSess`/`#hComp` (1608, 1615, `sessions`), Available Models grid (1728-1733, `configuration`), agent cards/routing/bindings/agent table (1736-1987, `configuration`). Required: reuse the existing `collectionLabel()` helper to render a small inline status line (same style as the sessions/usage labels) in each of those panels when the state is not `ready`. In `stale` state the label must include the collection's `collectedAt` formatted with the existing dashboard timezone formatter. Task 2 adds `gateway.statusReason` (`host_probe_not_applicable`) in container mode; when present, render the gateway status as `Unknown (container: host probe not applicable)` instead of `Offline`. Harness tests for cron stale, cost stale, and gateway statusReason.

6.3 Error feed expand state keyed by array index (around 1371-1403). Key `_expanded` by `item.signature` (fall back to the message text if signature is absent). Harness test: expand row for signature X, reorder the list, assert X is still the expanded one.

6.4 Log polling restart (around 1195-1213, 1032-1039, 2651-2655). `applyRuntimeConfig` calls `_startPoll()` on every 60 s refresh, so any `logRefreshIntervalMs` above 60000 never fires. Required: only call `_startPoll()` when the effective interval actually changed (or on first configuration). Harness test with fake timers (`vm` context with stubbed `setInterval`/`clearInterval` counters).

6.5 Verify and fix if real (reviewer nits, unverified): (a) `esc(sample).slice(0,120)` at ~1376 truncates entities; slice before escaping. (b) `role="status"` regions `#sessionSource`, `#usageSource`, `#runtimeSource`, `#taskSource` rewritten on every render; only assign `textContent` when changed. (c) `<dialog id="runtimeDetail">` lacks `aria-labelledby`; add it pointing at its title element. (d) `renderGatewayDegraded` (~2314-2345) leaves `#hGw` saying Online; set it to Unknown when the system endpoint fails. (e) `/api/operations/status` fetch at ~1523 swallows failure; on failure show `#operationPanel` hidden and log via `console.warn` once. (f) `${dur}` at ~1711 is the only unescaped interpolation; coerce with `String()` and `esc()`.

6.6 Harness coverage. `scripts/frontend-regression.cjs` currently stubs `RuntimePanels.renderHealth` and `linkSession` and never loads `SystemBar`, `ErrorFeed`, `Sections`, or `App`. Required: extend the loaded source slices so `SystemBar`, `ErrorFeed`, and `App.refresh` are evaluated for real (stub only `fetch` and timers), stop stubbing `renderHealth`, and add tests for `#runtimeHealthPanelInner` rendering ready/unavailable, `SystemBar` gateway-degraded transition including the readiness alert node lifecycle, and `ErrorFeed` render. Keep the harness dependency-free. The real `esc()` must be loaded, not a substitute.

6.7 `web_contract_test.go` and `web_runtime_test.go`: update any source-text assertions your changes invalidate; add assertions for the new `#fetchError` element and `statusReason` rendering.

---

### Task 7: Root-package facade wrappers for new internal APIs

Files owned: `config.go`, `refresh.go`, `server.go`, `doc.go` at the repository root, plus a new `openclaw.go` and `openclaw_test.go` at the root.

CLAUDE.md requires: logic in `internal/`, thin re-exports at the root, tests at both levels. Commit `c593d0d` added `internal/appopenclaw` and several `apprefresh`/`appchat`/`appconfig` exports with no root wrappers. `dashboard.Config` (root alias of `appconfig.Config`) now has an `Openclaw appopenclaw.Target` field whose type is unreachable from the root package.

Required, in a new root file `openclaw.go` (package `dashboard`):
- `type OpenclawTarget = appopenclaw.Target`
- `type OpenclawClient = appopenclaw.Client`
- `type CollectionStatus = apprefresh.CollectionStatus`
- `type RuntimeLogs = apprefresh.RuntimeLogs`
- `type ChatCapability = appchat.Capability`
- `func RedactOpenclawOutput(s string) string { return appopenclaw.Redact(s) }`
- `func OpenclawErrorCode(err error) string { return appopenclaw.ErrorCode(err) }`
- `func ResolveGatewayToken(dotenvPath, statePath string) string` delegating to `appconfig.ResolveGatewayToken` (check the real signature and mirror it exactly).
- `func ReadRuntimeLogs(ctx context.Context, client OpenclawClient, limit int) (RuntimeLogs, error)` delegating to `apprefresh.ReadRuntimeLogs` (mirror the real signature).
Each with a doc comment. Do not add wrappers for unexported or clearly internal helpers. If a name conflicts with an existing root identifier, report NEEDS_CONTEXT.

Tests in `openclaw_test.go`: aliases are the same types (`reflect.TypeFor[OpenclawTarget]() == reflect.TypeFor[appopenclaw.Target]()` style), `RedactOpenclawOutput` redacts a planted secret, `OpenclawErrorCode(nil) == "ok"`, and a compile-time check that `Config{}.Openclaw` is assignable to `OpenclawTarget`.

Run `go test -count=1 -run 'Openclaw|Facade' .` and `go vet ./...`.
