# Changelog

## v2026.5.13 — 2026-05-13

Code-review fixes (waves 1–4) and security hardening pass. No new features.

### Security

- **Static file handler rejects symlink escapes** — resolved paths that point outside the served root via symlinks now return 404 instead of serving the target file.
- **Service files written with mode 0o600 via atomic temp+sync+rename** — generated `launchd` plist and `systemd` unit files no longer leak bind address, port, or env values to other local users; the temp+fsync+rename sequence also closes the previous TOCTOU window.
- **Gateway error messages redact the gateway auth token** — added a `redactToken` helper applied to all error paths in `appchat`; raw upstream response bodies are no longer surfaced to clients.
- **Service install rejects non-absolute paths** — `OPENCLAW_HOME` must be absolute, and `PATH` entries that are not absolute are filtered out before being written into the unit/plist.
- **Response body caps unified at 64KB** — `appsystem.FetchJSONMap`, `fetchJSONMapAllowStatus`, and `FetchLatestNpmVersion` now share the same 64KiB `io.LimitReader` ceiling (was inconsistently 1MB in some paths).

### Fixed

- **`GetDataCached` data race** — concurrent map read/write fatal eliminated by returning a top-level `maps.Clone` snapshot under the cache lock.
- **Linux meminfo/swap underflow** — clamps when the kernel reports `MemAvailable > MemTotal` (observed on certain anomalous kernels) so used-bytes never wraps.
- **`parseTopCPU` bounds guard** — checks regex submatch length before indexing to avoid a panic on malformed `top -l 2` output.
- **`GetProcessInfo` rejects non-positive PIDs** before invoking `ps`/`proc` reads.
- **`GetJSON` stale-cache flag** — derived under a single mutex scope so the `(value, stale)` pair can no longer be torn across writers.
- **Shutdown sequence** — single `defer serverCancel()`, and the shutdown context is now parented to `signal.NotifyContext` so a second `SIGINT` accelerates exit instead of being swallowed.
- **`appruntime.CopyIfMissing` is atomic** via `O_CREATE|O_EXCL`; `appruntime.CopyFile` is atomic via temp file + `fsync` + `rename`.
- **`apprefresh.ModelName` segment-anchored O1/O3 match** — match now requires `/`, `-`, `:`, `.`, or `_` separators around the `o1`/`o3` token, eliminating `foo1bar`-style false positives.
- **`apprefresh` skips empty group ids** in session parsing to avoid an empty-key entry in the aggregation map.
- **Config strict-decode warnings** — unknown JSON keys emit a `slog.Warn` instead of being silently ignored; dotenv `export ` prefix is now correctly stripped before the `=` split.

### Changed

- **`/api/chat` empty gateway response** — an empty `choices[]` array now returns HTTP 502 with `{"error":"gateway unavailable"}` instead of HTTP 200 with `"(empty response)"`. An empty `content:""` string from the gateway still passes through unchanged.
- **`/api/chat` error bodies** — generic redacted messages only; raw upstream response bodies are no longer surfaced.
- **`/api/errors` response** — additive `dropped_signatures` (int) field reports the count of error signatures dropped after the in-memory dedup map saturated.
- **`cmd/openclaw-dashboard`** — `main()` now `os.Exit(run())`; the `run()` seam lets in-process tests exercise the binary entrypoint.

### Coverage

- `cmd/openclaw-dashboard`: 0% → 50% (in-process tests via the `run()` seam).
- `internal/appruntime`: 57.5% → 66.7% (failure-injection tests).
- `internal/appsystem`: 59.9% → 67.1%.
- `internal/appserver`: 66.9% → 73.1%.
- `internal/appservice`: 74.9% → 80.0%.
- Total: 60.7% → ~65%.

---

## v2026.4.29 — 2026-04-29

### Fixed

- **Cron table empty after OpenClaw v2026.4.20+ ([#25](https://github.com/mudrii/openclaw-dashboard/issues/25))** — OpenClaw v2026.4.20 split runtime cron state into a separate `~/.openclaw/cron/jobs-state.json` sidecar; the dashboard previously read only `jobs.json`, so the cron table rendered with blank `Last run` / `Next run` / `Last status` / `Last duration` columns. The refresh collector now merges the sidecar by `job.id`, with sidecar values winning wholesale and inline state preserved as the legacy fallback when the sidecar is absent. Dashboards now work against both pre- and post-v2026.4.20 OpenClaw installs.
- **`/api/system` cold-start latency and Gateway Runtime stuck on "Loading…" ([#26](https://github.com/mudrii/openclaw-dashboard/issues/26))** — cold collections (no warm cache) could run 10–12s when the gateway was slow because version probes ran serially before the parallel host-metrics group; the frontend Gateway Runtime card had no fetch timeout and never repainted on `r.ok===false` or thrown errors, so it stayed on `Loading…` indefinitely.
  - **Backend**: introduced `system.coldPathTimeoutMs` (default 4000, validated [200, 15000]); `SystemService.refresh` now wraps the entire collection in `context.WithTimeout(ColdPathTimeoutMs)` and runs versions in parallel with runtime/host-metrics goroutines. Partial cold-path results return `degraded:true` rather than blocking on the slowest probe; the version cache is only updated on full success so a deadline-cancelled collection can never poison the cached version pair.
  - **Frontend**: `Sys.fetch()` now uses `AbortController` with a 6000ms ceiling (4000ms cold-path budget + jitter); on `r.ok===false` or thrown exception the new `renderGatewayDegraded(reason)` helper repaints the card with `State=Unavailable` and an explicit reason instead of leaving the placeholder text in place.
  - **Skills empty state**: `web/index.html` now falls back to a `No skills configured` empty-state element when `data.skills` is `null` or `[]`, matching the existing Git Log fallback pattern.
- **`system.gatewayPort` default masked `ai.gatewayPort` inheritance** — `appconfig.Default()` pre-filled `SystemConfig.GatewayPort` with `18789`, which defeated the `Load()` fallback that was supposed to inherit from `ai.gatewayPort` when `system.gatewayPort` was omitted. The default is now zero so the inheritance path activates as documented; user-supplied values (either side) still win.
- **systemd unit missing `Environment=`** — Linux `service install` generated a unit file with no `OPENCLAW_HOME` or `PATH`, so the daemonized binary could not locate the openclaw CLI or OpenClaw runtime on fresh machines. The unit template now emits both `Environment=` directives, computed from the install-time `OPENCLAW_HOME` env override (falling back to `~/.openclaw`) and a deduplicated `PATH` with system bins (`/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin`) appended.
- **systemd `service install` did not pick up changed flags on reinstall** — the install path called `systemctl --user start`, which is a no-op when the unit is already running. Switched to `systemctl --user restart` so reinstalls with changed `--bind` / `--port` / `Environment` actually apply; `restart` also starts a stopped unit so first-installs still work.
- **Latest-version fetcher races with test cleanup** — the `getLatestVersionCached` background goroutine read a package-level `fetchLatestVersion` var that tests overrode during cleanup, occasionally producing data races under `-race`. Replaced with a per-instance `SystemService.fetchLatest` field set in the constructor; tests now isolate fully without touching shared state.
- **Version banner double-`v` prefix** — when `BuildVersion` was injected via `-ldflags` (the `make build` path) and the `VERSION` file already started with `v`, the startup banner printed `vv2026.4.x`. Both `Main()` assignment sites now normalize via `strings.TrimPrefix(version, "v")` so the banner and `--version` flag agree on the rendered value.

### Added

- **`system.coldPathTimeoutMs` configuration** — overall budget for a cold `/api/system` collection; defaults to 4000ms, validated [200, 15000]ms. Documented in `README.md` and `docs/CONFIGURATION.md`.
- **Frontend `renderGatewayDegraded(reason)` helper** — paints the Gateway Runtime card with an explicit `State=Unavailable` plus reason on fetch timeout, network error, or `r.ok===false`, so the card always reaches a terminal state.
- **Skills empty-state fallback** in `web/index.html`, matching the Git Log pattern.
- **Cron sidecar regression coverage** — new `internal/apprefresh/cron_state_test.go` plus split-store and legacy-inline fixtures exercise sidecar-only, legacy-only, sidecar-missing-job-id, malformed-sidecar, both-present, and `lastRunStatus` fallback paths.
- **Cold-path regression coverage** — new `internal/appsystem/cold_path_test.go` asserts the deadline is honoured, `degraded:true` is set on partial collection, host metrics still ship when gateway probes hang, and a deadline-cancelled collection cannot poison the version cache.

### Changed

- **`/api/system` cold path is now fully parallel** — versions, gateway runtime, and host metrics goroutines all run inside the same bounded `context.WithTimeout`. Previously versions ran serially before the parallel block.
- **CORS loopback-reflection invariants are now documented in code** — `internal/appserver/server_routes.go` carries an inline doc above `setCORSHeaders` enumerating why arbitrary `localhost:*` / `127.0.0.1:*` / `[::1]:*` origins are reflected (loopback bind by default, no `Allow-Credentials`, server-side gateway token, rate-limited `/api/chat`).
- **Planning doc preserved** — the issue #25/#26 fix plan moved to `docs/plans/2026-04-29-issue-25-26-fix-plan.md` alongside other historical planning docs.

### Security

- **GitHub external link hardened** — added `rel="noopener noreferrer"` to the `target="_blank"` link in the dashboard header so the linked tab cannot reach back to `window.opener`. Browsers default this for `noopener` since 2021, but the explicit attribute satisfies static auditors and older browsers.

### Documentation

- **`README.md`** and **`docs/CONFIGURATION.md`** — added rows for `system.coldPathTimeoutMs` and `system.gatewayPort` (including the inheritance behaviour). `examples/config.full.json` now includes the full `system` block which was previously missing.

---

## v2026.4.13 — 2026-04-13

### Added

- **Diagnostics delivery for issue [#14](https://github.com/mudrii/openclaw-dashboard/issues/14)** — implemented the diagnostics and log visibility work requested by [@agrr971](https://github.com/agrr971), including the API surface and release hardening needed to ship it safely.

### Fixed

- **Release hardening pass** — closed the remaining pre-release correctness, lifecycle, and packaging issues identified during final audit and validation.
- **Linux service backend compile fix** — fixed the `systemd` service path so Linux builds and status log collection use the backend lifecycle context correctly.
- **Shutdown and refresh lifecycle cleanup** — refresh-side subprocesses and refresh orchestration now follow cancellation more consistently.
- **Gateway probe timeout enforcement** — runtime gateway endpoint probes now consistently honor configured timeouts.
- **CORS and local development compatibility** — reflected CORS responses now emit `Vary: Origin`, and IPv6 loopback origins are accepted for local browser testing.
- **Sub-agent accounting consistency** — zero-cost subagent runs are still counted, and token usage aggregation keeps display-name normalization consistent.
- **Log feed correctness** — merged log selection now preserves the globally newest entries across skewed sources and uses a bounded merge strategy.

### Changed

- **Structured logging cleanup** — production logging paths in config, refresh, system, and startup flows were aligned further toward `slog`.
- **Logs config precedence tightened** — canonical `logs.*` fields now win, while legacy compatibility fields only backfill when canonical values are unset.
- **System probe transport reuse** — system runtime/version HTTP calls now reuse a shared client instead of creating short-lived clients repeatedly.
- **Main process lifecycle simplified** — the CLI now uses one signal-driven lifecycle path for startup and graceful shutdown.

### Documentation

- **Release docs refreshed** — README, TECHNICAL.md, CONTRIBUTING.md, and installer messaging now match the current refresh model, command surface, and published endpoints.

### Merged pull requests

- [#19](https://github.com/mudrii/openclaw-dashboard/pull/19) by [@SweetSophia](https://github.com/SweetSophia) — consolidate escaping helpers for consistent HTML escaping
- [#20](https://github.com/mudrii/openclaw-dashboard/pull/20) by [@SweetSophia](https://github.com/SweetSophia) — use `structuredClone` for state snapshots with safe fallback
- [#21](https://github.com/mudrii/openclaw-dashboard/pull/21) by [@SweetSophia](https://github.com/SweetSophia) — reduce lock churn in latest-version cache refresh
- [#22](https://github.com/mudrii/openclaw-dashboard/pull/22) by [@SweetSophia](https://github.com/SweetSophia) — preserve refresh shutdown semantics safely

### Thanks

- Thanks [@SweetSophia](https://github.com/SweetSophia) for the merged improvements in [#19](https://github.com/mudrii/openclaw-dashboard/pull/19), [#20](https://github.com/mudrii/openclaw-dashboard/pull/20), [#21](https://github.com/mudrii/openclaw-dashboard/pull/21), and [#22](https://github.com/mudrii/openclaw-dashboard/pull/22).
- Thanks [@agrr971](https://github.com/agrr971) for opening [#14](https://github.com/mudrii/openclaw-dashboard/issues/14) and documenting the diagnostics use case clearly enough to drive implementation.

---

## v2026.4.8 — 2026-04-08

### Added

- **Built-in service management** — `openclaw-dashboard` now ships six service lifecycle subcommands: `install`, `uninstall`, `start`, `stop`, `restart`, and `status`. All commands are available directly and via the `service` alias (`openclaw-dashboard service install`, etc.).
  - **macOS**: manages a LaunchAgent plist at `~/Library/LaunchAgents/com.openclaw.dashboard.plist` via `launchctl`
  - **Linux**: manages a systemd user service at `~/.config/systemd/user/openclaw-dashboard.service` via `systemctl --user`
  - `install` bakes `--bind` and `--port` (from flags, env vars, or `config.json`) into the generated service file
  - `status` shows version, running state, PID, uptime, port, auto-start, and last 20 log lines
  - Uninstall preserves all config and data — only the service registration is removed
  - New package: `internal/appservice/` with `Backend` interface, launchd/systemd/unsupported implementations, shared HTTP liveness probe

### Fixed

- **Homebrew upgrade version drift** — Homebrew runtime seeding now refreshes `~/.openclaw/dashboard/VERSION` on startup so `openclaw-dashboard --version` stays aligned with the installed package after upgrades. Existing user-owned runtime files such as `config.json` remain untouched.

### Documentation

- **Release/install docs refreshed** — README, TECHNICAL.md, configuration docs, and Nix package metadata now reflect the 2026.4.8 release and the current Homebrew/download/install behavior.

---

## v2026.4.4 — 2026-04-04

### Fixed

- **Release packaging broken** — GoReleaser v2 renamed `archives.format` (string) to `archives.formats` (array); the old key was silently ignored, causing GoReleaser to publish raw binaries instead of `.tar.gz` archives. `brew install` and the README tarball install command both 404'd as a result. Fixed by updating `.goreleaser.yml` to use `formats: [tar.gz]`.

---

## v2026.4.3 — 2026-04-03

### Fixed

- **M1: Exact path match for `/api/refresh`** — `strings.HasPrefix` replaced with `==`; previously any path beginning with `/api/refresh` (e.g. `/api/refreshXXX`) would incorrectly match the refresh handler
- **M2: `serverCtx` renamed to `shutdownCtx`** in `SystemService` — clarifies that this context is the server lifecycle context and must not be used for per-request operations
- **M3: Linux CPU dual-sample sleep is context-aware** — `time.Sleep(200ms)` replaced with `select { case <-time.After: case <-ctx.Done(): }` so graceful shutdown is not blocked by the inter-sample gap
- **M4: 404 responses include CORS headers** — all `http.NotFound` call sites in `ServeHTTP` and `HandleStaticFile` now go through a new `notFound` helper that sets CORS headers first; browser clients no longer receive a misleading CORS error instead of a clean 404
- **L3: npm registry response cap tightened** — `io.LimitReader` in `FetchLatestNpmVersion` reduced from 1 MiB to 64 KiB; sufficient for any npm metadata response

### Added

- **v2026.3.7 and v2026.3.6 CHANGELOG entries** — two releases that were missing from the changelog

---

## v2026.3.23 — Comprehensive Audit & Hardening

### Error Handling

- **15 silent failure fixes** — All `os.UserHomeDir()` calls (4 sites) now log warnings instead of silently producing invalid paths. `json.Marshal` errors in `sendJSON` and system metrics now return proper error responses instead of empty bodies.
- **Gateway timeout classification** — `CallGateway` now correctly returns HTTP 504 (Gateway Timeout) instead of 502 when the HTTP client timeout fires, using `errors.Is(err, context.DeadlineExceeded)`.
- **Config loading feedback** — `Load()` now logs when falling back to defaults (both config paths failed). Users are no longer silently served default configuration.
- **Dotenv scanner error** — `ReadDotenv` now checks `scanner.Err()` after the parsing loop to detect truncated reads.
- **Session/token cache logging** — `loadSessionStores`, `saveTokenUsageCache`, and `fetchLiveSessionModelsCLI` now log when skipping files or failing operations instead of silently returning partial data.
- **Sort comparator panic guard** — Subagent run sorting now uses comma-ok type assertions to prevent panics on nil/non-string timestamp values.
- **PID parse safety** — Gateway PID parsing now skips invalid entries instead of displaying "PID: 0".
- **`ExpandHome` logging** — Logs warning when `UserHomeDir` fails, making tilde expansion failures visible.
- **`DASHBOARD_PORT` validation** — Invalid (non-numeric) port environment variables now produce a warning instead of being silently ignored.

### Linting & Code Quality

- **Zero lint issues** — All `errcheck`, `ineffassign`, `staticcheck`, `govet`, `gocritic`, and `unused` checks pass clean.
- **Added `.golangci.yml`** — Project-wide linter configuration with `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `gocritic` enabled. Test files excluded from errcheck.
- **Added `Makefile`** — Build automation with `make build`, `make test`, `make lint`, `make vet`, `make cover`, and `make check` targets.
- **CI linting** — Added `golangci-lint` support to the repo command surface and CI workflow.
- **Dead code removed** — Removed stale unused aliases, legacy helpers, and unreachable branches left behind by the Go-only migration.
- **Unreachable code removed** — `strings.Contains(clean, "..")` after `filepath.Clean` (which never leaves `..`).
- **Ineffectual assignments fixed** — Removed dead `todayStr`, `compactionMode`, `agentConfig` assignments in refresh pipeline.
- **Errcheck compliance** — All `defer f.Close()` / `defer resp.Body.Close()` patterns wrapped properly. `os.Remove` cleanup errors explicitly discarded with `_ =`.
- **If-else chains refactored** — Converted to switch statements in `clampThreshold` and model resolution.

### Testing

- **37 new tests** across 4 previously untested internal packages:
  - `internal/appconfig` (13 tests, 74.2% coverage) — Default values, Load with valid/partial/invalid JSON, threshold clamping, ReadDotenv edge cases, ExpandHome
  - `internal/appserver` (10 tests, 23.8% coverage) — HandleStaticFile allowlist/traversal, ServeHTTP routing/CORS/405, sendJSON marshal error, rate limiter
  - `internal/appchat` (7 tests, 75.9% coverage) — BuildSystemPrompt, CallGateway success/error/timeout, GatewayError
  - `internal/appruntime` (7 tests, 43.5% coverage) — DetectVersion, ResolveDashboardDir env override, ResolveRepoRoot, CopyIfMissing
- **Extended `internal/appsystem`** (7 new tests, 12.5% coverage) — FormatBytes, BoolFromAny, VersionishGreater, DecodeJSONObjectFromOutput, ParseGatewayStatusJSON
- **All 121 tests pass** with `-race` flag.

### Documentation

- **TECHNICAL.md** — Updated version header to 2026.3.22; frontend data flow section updated from legacy `loadData()/global D/render()` to current 7-module architecture (`DataLayer.fetch()`, `State`, `DirtyChecker.diff()`, `Renderer`); theme engine and tab state sections updated to reference module methods.
- **docs/CONFIGURATION.md** — Added full `system` configuration section with 18 fields documented; added missing environment variables (`OPENCLAW_DASHBOARD_DIR`, `DASHBOARD_PORT`, `DASHBOARD_BIND`).
- **README.md** — Clarified bash is optional (only for `refresh.sh` wrapper); updated `/api/system` description to reference Go refresh collector.
- **Dockerfile** — Added `COPY VERSION ./` to builder and runtime stages for proper version detection; removed unused `curl` and `jq` packages from runtime image.
- **`.goreleaser.yml`** — Fixed caveats port from 9090 to 8080.
- **Bug report template** — Updated component list to reference `internal/` packages.
- **Package doc comments** — Added to all 6 internal packages.
- **`flake.nix`** — Updated version to 2026.3.22; removed `curl` and `jq` from runtime and dev dependencies.

### Cleanup

- **`.gitignore` updated** — Added `.claude/`, `coverage.out`, `coverage.html`.
- **Removed tracked `.claude/settings.local.json`** from git.
- **Deleted stale branches** — `master`, `remove-python-dependency`, `exp/openclaw-runtime-observability`.

---

## v2026.3.22 — Go-Only Codebase

### Breaking Changes

- **Python removed entirely** — `server.py`, `system_metrics.py`, and all Python test files have been removed. The Go binary is now the only server implementation.
- **`refresh.sh` rewritten** — No longer contains embedded Python. Now calls the Go binary with `--refresh` flag. Data collection logic has been ported to Go (`refresh.go`).
- **Dockerfile simplified** — Removed the `--target python` build stage. Only the Go binary stage remains.
- **`install.sh` rewritten** — Now downloads/builds the Go binary instead of running `server.py`. System services use the Go binary.
- **`uninstall.sh` updated** — Kills `openclaw-dashboard` process instead of `server.py`.
- **Python 3 no longer required** — The dashboard has zero runtime dependencies beyond the single Go binary and `bash`.

### Added

- **`--refresh` flag** — `openclaw-dashboard --refresh` generates `data.json` and exits. Used by `refresh.sh` and available for cron/automation.
- **`refresh.go`** — Full Go port of the Python data collection logic (~860 lines of Python → Go). Handles: OpenClaw config parsing, session collection, JSONL token usage scanning, cron job parsing, gateway health, git log, daily chart generation, alert building, frozen data merging.

### Removed

- `server.py` — Python HTTP server (replaced by Go server since v2026.3.3)
- `system_metrics.py` — Python system metrics collector (replaced by Go `system_service.go`)
- `requirements.txt` — No Python dependencies needed
- `tests/` directory — 13 Python test files (Go test suite provides coverage)
- `.pytest_cache/` — Pytest artifacts

---

## v2026.3.8 — Runtime Observability

### Features

- **Runtime observability MVP** — `/api/system` now includes an `openclaw` block with live gateway runtime state sourced from three endpoints: `/healthz` (liveness + uptime), `/readyz` (readiness + failing deps), and `openclaw status --json` (version, latency, security). Both Go (`collectOpenclawRuntime`) and Python (`_collect_openclaw_runtime`) backends collect this data in parallel alongside existing metrics.
- **Gateway Runtime + Config card split** — System Settings now shows two separate cards: *Gateway Runtime* (live `/api/system` data — state, uptime, version, source indicator) and *Gateway Config* (static config snapshot from `data.json` — port, mode, bind, auth). The old combined `gatewayPanel`/`gatewayPanelInner` has been removed.
- **Gateway readiness alerts** — Alert banner synthesizes a `🟡 Gateway not ready: discord` (or any failing dep) alert from the `openclaw.gateway.failing[]` array. Auto-clears when readiness recovers or gateway goes fully offline. Distinct from the "Gateway is offline" alert — both states are mutually exclusive by design.

### Improvements

- **Health pill simplified** — `hGw` now shows `● Online` or `● Offline` (green/red) for the fully-healthy case, or `● Live` (green, no `/ Not Ready`) when live but not ready. Readiness detail is surfaced exclusively through the Alerts banner — no more misleading compound state label in the health row.
- **`_gatewayState()` helper (JS)** — New `SystemBar._gatewayState(d)` function encapsulates the runtime-vs-versions fallback decision. Returns `{source, ok, live, ready}`. Runtime data is trusted when `healthEndpointOk`, `readyEndpointOk`, `uptimeMs>0`, or `failing.length>0`.
- **`_versionsBehind()` robustness** — Strips beta/dev/build suffixes (e.g., `-beta-runtime-observability`, `-dev.1`) before comparing `YYYY.M.D` version triplets. Avoids false "behind" warnings on pre-release installs.
- **`fmtDurationMs()` added to SystemBar** — Converts uptime in ms to human-readable `Ns / Nm / Nh / Nd` string; used by both the Gateway Runtime card and the health row uptime display.
- **`localStorage` key bumped** — `ocDash-v1` → `ocDash-v2` to reset collapse defaults and prevent stale UI state after upgrade.
- **Stale-while-revalidate on `/api/refresh`** (Go) — Response uses JSON round-trip to safely inject `"stale":true` instead of fragile byte-level string replacement that would silently fail on whitespace/ordering differences (B2 fix).
- **Versions collected before parallel phase** (Go) — `getVersionsCached()` now runs before the parallel goroutine group so `collectOpenclawRuntime` always receives real version data instead of an empty struct (B1 fix).

### Bug Fixes

- **Go: `bytes` import removed** — Stale `"bytes"` import from the old byte-level stale injection was cleaned up.
- **Python: `_parse_json_array_fragment` removed** — Dead helper added during development but never called; removed to keep the codebase clean.
- **Gateway status: parse stdout on non-zero exit** — Both Go and Python now attempt to parse `openclaw gateway status --json` stdout even when the command exits non-zero. Many CLIs emit valid JSON to stdout while exiting 1 (e.g., gateway offline but status successfully queried). Falls back to HTTP probe only when stdout has no usable JSON (I2 fix).
- **Thundering herd prevention** (Python) — `_refreshing` flag in `_VersionsState` prevents multiple concurrent calls from spawning redundant collection goroutines when the cache is cold. Returns stale data to waiters while one refresh is in flight.
- **CPU sampling interval** — Increased from 50 ms to 200 ms (Linux dual-`/proc/stat` sample) to reduce noise and give a more representative utilisation window.
- **`fetchJSONMapAllowStatus` for readyz 503** — New helper accepts a set of allowed HTTP status codes so `/readyz` 503 responses (partial readiness) are parsed as valid JSON rather than discarded as errors.

### Breaking Changes

- **`gatewayPanel` / `gatewayPanelInner` removed** — Any external scripts or browser extensions targeting these DOM IDs will break. Use `gatewayRuntimePanelInner` (live data) or `gatewayConfigPanelInner` (config) instead.
- **Top-bar GW pill removed** — `sysGateway` span is no longer present in the HTML. Gateway state is still shown in the System Health row (`hGw`) and the Gateway Runtime card.
- **Install commands changed** — Release assets are raw binaries, not tarballs. The correct download format is `curl -L <url> -o openclaw-dashboard` (no `| tar xz`). See Quick Start.

### Internal

- **`system_types.go`** — Added `SystemOpenclaw`, `SystemOpenclawGateway`, `SystemOpenclawStatus`, `SystemOpenclawFreshness` structs; `SystemResponse.Openclaw` field added.
- **`system_service.go`** — Added `collectOpenclawRuntime()`, `probeOpenclawGatewayEndpoints()`, `parseOpenclawStatusJSON()`, `fetchJSONMapAllowStatus()`, `_versionTriplet()` helpers; added `regexp` import; removed stale `bytes` import.
- **`system_metrics.py`** — Added `_collect_openclaw_runtime()`, `_fetch_json_url_allow_status()`, `_parse_json_object_fragment()`; removed dead `_parse_json_array_fragment()`.
- **`version.go`** — Removed unused `resolveRepoRoot()` helper (dead after embed approach solidified).
- **`version_test.go`** — Removed `TestResolveRepoRoot_Direct` and `TestResolveRepoRoot_DistSubdir` tests (function deleted).
- **`main.go`** — Simplified to `dir := filepath.Dir(exe)` (no longer calls `resolveRepoRoot`).
- **Go dependency bumps** in `go.mod`.
- **56 new frontend tests** in `tests/test_frontend.py` — `TestGatewayPillRemoved`, `TestNoRawPlaceholderTokens`, `TestGatewayRuntimeConfigSplit`, `TestGatewayReadinessAlert`, `_versionsBehind` and `_gatewayState` behavioral tests.
- **37 new Python tests** in `tests/test_system_metrics.py` — `TestOpenclawRuntime`, `TestStaleInjectionSafe`, `TestVersionCollectionI2`, `TestVersionsCacheThunderingHerd`.
- **384 new Go tests** in `system_test.go`.

---

## v2026.3.7 — 2026-03-07

### Fixed (Security — P0)

- **Rate limiting** — Token-bucket (10 req/min per IP) on `/api/chat`; returns `429 Too Many Requests` with `Retry-After: 60` header
- **HTTP server timeouts** — Go server enforces Read 30s / Write 90s / Idle 120s; prevents slow-client and resource exhaustion attacks
- **Path traversal guard hardened** — Resolved path is checked to remain inside the serve root
- **Goroutine lifecycle** — Server lifecycle context propagated to all goroutines; all goroutines exit cleanly on SIGTERM

### Fixed (P1 Parity)

- **Python `ThreadingHTTPServer`** — Python server upgraded from single-threaded to `ThreadingHTTPServer`; a long refresh no longer blocks all concurrent clients
- **CORS on all error paths** — CORS headers now set on 4xx/5xx responses in both servers (previously only on 2xx)
- **Python gateway timeouts** — Explicit connect + read timeouts on all gateway HTTP calls

### Changed

- **Unified cache layer (Go)** — `loadData()` fills raw bytes + parsed map atomically under one lock; eliminates double-`stat`/double-read on concurrent requests
- **Mtime-based Python cache** — Python `/api/chat` re-reads `data.json` only when mtime changes (was per-request)
- **Pre-rendered index (Go)** — `index.html` embed pre-rendered at startup; zero per-request disk reads
- **Static file allowlist (Python)** — Python server restricts static file serving to an explicit allowlist; was previously serving any file in the repo directory including `.git/config`
- **Linux CPU sample interval** — Reduced from 200ms to 50ms for faster `/api/system` response time

### Added

- **87 Go tests** pass with `-race` flag
- **165 Python tests** pass

---

## v2026.3.6 — 2026-03-05

### Added

- **Collapsible sections** — 9 collapsible dashboard sections with right-aligned chevron toggles, localStorage persistence, FOUC prevention, Expand All / Collapse All buttons, and full ARIA keyboard navigation
- **OpenClaw version freshness** — Version pill in the top metrics bar is colour-coded: green (up to date), yellow (1 release behind), red (2+ releases behind); latest version polled from npm registry
- **npm latest version check** — `/api/system` now returns `versions.latest` (best-effort, non-blocking)

### Fixed (Multi-Model Audit — 30+ fixes)

- **Python static file allowlist** — Was exposing all repo files; now restricted to explicit list
- **CORS preflight handlers** — `OPTIONS` handler added to both Go and Python servers
- **HEAD body suppression** — All endpoints correctly suppress body on HEAD requests
- **UTF-8 rune-safe truncation** — Chat history truncation now operates on rune boundaries, not bytes
- **XSS defense** — Theme buttons use data attributes + event delegation instead of inline `onclick`
- **CSS `--glass` variable** — Properly defined and applied across all 6 built-in themes
- **`_gwOnlineConfirmed` cleared on fetch failure** — Gateway online state correctly reset when `/api/system` fetch fails

### Changed

- **Layout** — Models, Skills, Git Log panels moved inside Agent Configuration collapsible section
- **GW pill removed** from Top Metrics Bar — gateway status displayed in System Health panel instead

---

## v2026.3.5 — 2026-03-04

### Fixed
- **README panel numbering** — Removed duplicate panel entries; panels now correctly numbered 1–12 with no repeats
- **README test counts** — Updated Architecture comparison table: Python 123 tests (was 14), Go 57 tests (was 39)
- **README config example** — Added `system` block with per-metric thresholds to the `config.json` example
- **README Architecture table** — Added `/api/system` row showing both Python and Go implementations
- **Release assets** — All 4 platform binaries now properly built from source and attached to GitHub release (darwin-arm64, darwin-amd64, linux-amd64, linux-arm64) with SHA256 checksums

---

## v2026.3.4 — 2026-03-04

### Added
- **Top Metrics Status Bar** — New always-on bar at the top of the dashboard showing live CPU, RAM, swap, disk usage, OpenClaw version, and gateway status. Updates every 10 seconds (configurable). Supports both Go binary and Python server backends.
- **`GET /api/system` endpoint** — New endpoint returning JSON with all host metrics, per-metric thresholds, and version info. Uses TTL cache with stale-serving semantics (returns cached data immediately, refreshes in background). Returns `degraded: true` on partial failures, `503` only on full cold-start failure.
- **Per-metric configurable thresholds** — CPU, RAM, swap, and disk each have independent `warn` and `critical` percent thresholds (defaults: 80%/95%). Configurable via `config.json` under `system.cpu`, `system.ram`, `system.swap`, `system.disk`.
- **Cross-platform collectors (Go)** — macOS: `top -l 2` (current delta, not boot average), `vm_stat`, `sysctl vm.swapusage`. Linux: `/proc/stat` dual-sample with steal field, single `/proc/meminfo` read shared between RAM+Swap. Disk via `syscall.Statfs` on both platforms.
- **Python backend parity** — `system_metrics.py` implements identical API shape using stdlib/subprocess only. Uses pre-compiled regexes, atomic refresh flag (`should_start` pattern), HTTP-only gateway probe, per-metric threshold clamping.
- **Dynamic OpenClaw binary resolution** — Both Go and Python backends probe `$HOME/.asdf/shims`, all installed asdf nodejs versions, and common system paths — no hardcoded user paths.
- **Configurable gateway port** — `system.gatewayPort` (synced from `ai.gatewayPort`) used for the gateway liveness HTTP probe in both backends.
- **Parallel collection** — Go backend collects CPU/RAM/Swap/Disk/Versions concurrently via `sync.WaitGroup`, reducing wall-clock time from ~4s to ~1.5s per cycle.
- **Stderr capture in subprocess calls** — `runWithTimeout()` now appends stderr from failed subprocesses to the error message for better diagnostics.
- **15 new Go tests** — Schema, HEAD, CORS, disabled 503, thresholds in response, global+per-metric clamping, cache hit, degraded 200, disk, defaults.
- **13 new Python unit tests** — Parser tests for `parse_top_cpu`, `parse_vm_stat`, `parse_swap_usage_darwin`, `parse_proc_meminfo` covering edge cases.
- **5 new Python integration tests** — `TestSystemEndpoint`: schema, HEAD no body, CORS, content-type, degraded 200.

### Changed
- **Poll interval** — Default `system.pollSeconds` is 10s (previously 5s) to give `top -l 2` comfortable headroom within the TTL.
- **`tests.yml` CI** — Added `test_system_metrics.py` to static analysis step; added `requirements.txt` for pip cache compatibility.

### Technical Details
- `system_types.go` — `SystemResponse`, `SystemCPU`, `SystemRAM`, `SystemSwap`, `SystemDisk`, `SystemVersions`, `SystemGateway`, `SystemThresholds`, `ThresholdPair` structs
- `system_collect_darwin.go` — Pre-compiled regexes (`reTopIdle`, `reVmPageSize`, etc.), `collectCPURAMSwapParallel()`
- `system_collect_linux.go` — `ramFromMeminfo()`, `swapFromMeminfo()` helpers for shared meminfo map, steal field in CPU total
- `system_service.go` — `SystemService` with `sync.RWMutex` cache, `resolveOpenclawBin()`, `detectGatewayFallback()`, configurable port
- `config.go` — `MetricThreshold` struct, `SystemConfig` with per-metric fields, clamping invariants (`0 < warn < critical ≤ 100`)
- `server.go` — `/api/system` route with `system.enabled` gate → 503
- `system_metrics.py` — `_MetricsState`/`_VersionsState` containers (no `globals()` anti-pattern)
- `index.html` — `div#systemTopBar`, `.sys-pill` CSS, `SystemBar` JS object with `??` operators, `Math.max(ms, 2000)` poll guard

---

## v2026.3.3 — 2026-03-03

### Fixed
- **Cache coherence bug** — `getDataRawCached()` now invalidates the parsed data cache when the raw cache is updated. Previously, `/api/refresh` could bump the shared mtime without clearing the parsed cache, causing `/api/chat` to silently use stale dashboard data for its AI context.
- **HEAD requests on `/api/refresh`** — HEAD responses no longer write a body (HTTP spec compliance). Added missing `Content-Length` header.
- **Gateway error status code** — `/api/chat` now returns HTTP 502 (Bad Gateway) instead of 200 when the upstream gateway fails. Clients can now distinguish "AI answered" from "AI is down".
- **`.env` quote stripping** — `readDotenv()` now strips surrounding double and single quotes from values (e.g., `KEY="value"` → `value`).

### Added
- **Graceful shutdown** — Go binary now handles SIGINT/SIGTERM signals and drains in-flight requests (5s timeout) before exiting. Clean container stops, no more orphaned `refresh.sh` processes or `data.json.tmp` files.
- **Gateway response size limit** — `callGateway()` now caps response body at 1MB via `LimitReader`. Prevents memory exhaustion from a misbehaving gateway.
- **Comprehensive Go test suite** — 39 tests with `-race` flag covering:
  - Cache coherence between raw and parsed caches
  - HEAD vs GET behavior for all endpoints
  - Static file allowlist and path traversal defense
  - CORS origin reflection and rejection
  - Chat input validation (empty, too long, too large, invalid JSON)
  - Gateway calls (success, errors, empty responses, oversized responses)
  - System prompt building with empty and populated data
  - Config loading (defaults, overrides, invalid JSON, zero-value clamping)
  - Dotenv parsing (comments, blanks, equals-in-value, quotes, missing file)
  - Version detection (git tag, VERSION file, fallback)

### Changed
- **Agent Bindings UI** — Changed from `flex-wrap` to a symmetric 2-column CSS grid layout. Cards are now equal-width with text overflow ellipsis for long names.

---

## v2026.2.24 — 2026-02-24

### Fixed
- **Accurate model display for sub-agents** — sub-agents now show their actual model (e.g., "GPT 5.3 Codex", "Claude Opus 4.6") instead of defaulting to the parent agent's model (k2p5). Root cause: sub-agents store model in `providerOverride`/`modelOverride` fields, which the dashboard wasn't reading.
- **5-level model resolution priority chain** — Gateway live data → providerOverride/modelOverride → session store `model` field → JSONL `model_change` event → agent default. Ensures the most accurate model is always displayed.

### Added
- **Gateway API query** in `refresh.sh` — queries `openclaw sessions --json` for live session model data as the primary source of truth. Graceful fallback if gateway is unavailable.
- **Model alias resolution** — sub-agent models now display friendly names (e.g., "GPT 5.3 Codex" instead of "openai-codex/gpt-5.3-codex").

## v2026.2.23 — 2026-02-23

### Added
- **AI Chat panel** (`💬`) — floating action button opens a chat panel backed by your OpenClaw gateway
  - Natural language queries about costs, sessions, cron jobs, alerts, and configuration
  - System prompt built from live `data.json` on every request (always up to date)
  - Stateless gateway calls via OpenAI-compatible `/v1/chat/completions` — no agent memory bleed
  - Conversation history with configurable depth (`ai.maxHistory`, default 6 turns)
  - 4 quick-action chips for common questions
  - Dismissible with Escape key or clicking outside
- **`/api/chat` endpoint** in `server.py` — POST `{"question": "...", "history": [...]}` returns `{"answer": "..."}` or `{"error": "..."}`
- **`read_dotenv()`** — parses `~/.openclaw/.env` to load `OPENCLAW_GATEWAY_TOKEN` without requiring env var exports
- **`build_dashboard_prompt()`** — compresses `data.json` into a structured ~300-token system prompt
- **`call_gateway()`** — stateless HTTP call to the OpenClaw gateway with 60s timeout
- New `ai` section in `config.json`: `enabled`, `gatewayPort`, `model`, `maxHistory`, `dotenvPath`
- 14 new tests in `tests/test_chat.py` covering config validation, dotenv parsing, prompt building, gateway error handling, and HTTP endpoint behaviour (AC-CHAT-1 through AC-CHAT-8)
- Converted `test_critical.py` and `test_hierarchy_recent.py` from pytest to stdlib `unittest` — no external test dependencies required

### Prerequisites (one-time setup in `~/.openclaw/openclaw.json`)
The gateway's `chatCompletions` endpoint is disabled by default. Enable it once:
```json
"gateway": {
  "http": { "endpoints": { "chatCompletions": { "enabled": true } } }
}
```
The gateway hot-reloads this change — no restart needed.

### Changed
- Architecture diagram updated to show `/api/chat` endpoint

---

## v2026.2.21 — 2026-02-21

### Fixed
- Handle dict-style model config for agents in refresh script

### Changed
- Ignore `.worktrees/` directory in git

---

## v2026.2.20 — 2026-02-20

### Added
- 6 built-in themes (Midnight, Nord, Catppuccin Mocha, GitHub Light, Solarized Light, Catppuccin Latte)
- Theme switcher in header bar, persisted via `localStorage`
- Custom theme support via `themes.json`
- Sub-agent activity panel with 7d/30d tabs
- Charts & trends panel (cost trend, model breakdown, sub-agent activity) — pure SVG
- Token usage panel with per-model breakdown

### Fixed
- Dynamic channel/binding status for Slack/Discord
