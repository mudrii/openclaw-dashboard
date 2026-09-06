# Missing-field repair candidate

Date: 2026-09-05. These are implemented source changes, not deployment or live UI certification. The earlier [field audit](2026-09-05-ui-data-field-audit.md) remains the historical reproduction record.

## Implemented

| Audit fields | Repair |
| --- | --- |
| F01–F03 | Selected gateway `system.info` PID/uptime, typed minimal projection, independent collection status and same-target stale retention; gateway RSS mapped to both summaries. Native legacy fallback retained; container host fallback prohibited. Installed OpenClaw 2026.9.1 returns top-level `pid` and `uptimeMs`. |
| F04–F07 | Explain known subtotal, unpriced entries/models and unavailable projection. Populate breakdowns only when pricing is complete, preserving genuine zero cost. |
| F10, F12–F13 | Token trend fallback independent of pricing; usage bars use raw token volume; total row sums raw input/output/cache-read/total tokens. |
| F14–F17 | Explain inherited/no explicit cron model, absent run/schedule; missing duration remains null, genuine zero remains zero. |
| F19–F20 | Active loaded-session count separate from stored total; no-goal state and budget are not applicable. |
| F24–F26 | Live per-account channel values drive configuration cards; unknown booleans stay unknown, no synthetic healthy status; absent probe is not probed. |
| F27, F29–F30 | Unchecked embedding is not checked; absent plugin version is not reported; skills summary uses runtime inventory with eligibility/model-visible counts and collection status. |

No price, credential, inference result, live job, backup, database repair, or permission change was fabricated or performed. No additional dependencies or polling loops were introduced. The existing health collection adds one bounded read-only RPC per refresh.

## Verification

- Added failing regression tests before fixing cost breakdowns, missing cron duration, selected-runtime metrics and the main UI mappings.
- `node scripts/frontend-regression.cjs`: **16 tests pass**, exercising actual embedded renderer functions, not duplicate implementations.
- Root frontend regression tests pass with the race detector. All ten packages compile with `go test ./... -run '^$'`; both embedded scripts parse. Compilation-only checks are not full-suite passes.
- Race tests pass for `internal/appopenclaw` and `internal/apprefresh`, with **only** `TestReadyzProbe_ParsesFailingFromHTTP503` excluded from that supplemental run because it binds a listener. The normal suite was not changed to skip it.
- Target-routing fixtures cover explicit native and separately named Docker/Podman targets, rejecting inherited unrelated container selection. These are routing fixtures, not live engine certification.
- `make check` passed vet and lint (zero issues), then failed on sandbox listener restrictions. It also exposed an older sidecar test expecting absent duration to equal zero; that expectation was updated to null while preserving the full-replacement assertion. The corrected collector package passed the supplemental race run.
- Standalone Staticcheck remains blocked by Go proxy DNS/network access. Earlier user-run full-check success predates these changes and is not reused as certification.
- Separate build: `dist/openclaw-dashboard-field-fixes`. The running root binary and candidate configuration were not replaced.
- Built candidate SHA-256: `749fcd3f06bb56e7ecad0ebdd7489920fc4a0024ee7a51541ec432959de47979`. Build completed successfully despite a non-fatal sandbox warning about writing the module stat cache. `git diff --check` passed.

## Remaining gates

The subsequent [live desktop replay](2026-09-05-field-fixes-ui-retest.md) confirms the core missing-field repairs on Podman and records residual presentation issues. It does not certify mobile layout, native/Docker targets, or unrestricted full checks.

1. Run unrestricted `GOTOOLCHAIN=go1.27.1 make check` and the frontend regression harness on this source revision.
2. Restart the test instance using the separate candidate binary and its existing isolated data directory. Replay the field audit through Computer Use, including refreshes, chart range changes, mobile layout, and PID/RSS consistency. Confirm the new `runtimeInfo` collection succeeds against the selected gateway.
3. Verify equivalent native and Docker live read contracts as well as the current Podman target. Confirm inaccessible targets do not show host metrics.
4. Investigate the genuine upstream missing-pricing entries, session token freshness, provider auth-route discrepancy and intermittent OpenClaw database-lock warnings. Missing backup/probe/goal data must not be made to look like successful verification.

The new candidate is **not yet release-certified**. No deployment occurred.
