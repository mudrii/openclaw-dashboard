# Field availability repairs — 2026-09-06

Status: implemented and locally regression-tested; **not release-certified**.

## Implemented

- Session collection retains separate token/context availability states. Missing, stale and invalid counts do not become zero or a current context percentage. A known context limit remains available independently.
- Cost cards and token tables distinguish pricing missing, incomplete collection and unreported costs. The donut explanation retains known subtotals and missing-model counts instead of an unexplained placeholder. No pricing or historical costs are invented.
- A pricing coverage disclosure joins exact unpriced provider/model IDs to numeric base rates from the selected configuration. Only input/output/cache-read/cache-write prices are projected; credentials and provider URLs are excluded. Rates are configuration diagnostics, not billed totals, and do not represent tiered/plan-specific billing in full.
- Backup timestamps use already-collected last/latest attempt/success fields. Runtime enablement, explicit configuration, absent defaults, and restorability are not conflated.
- Model authentication explains the CLI's `missing` kind separately from its missing-provider-in-use list. An absent list now stays null rather than becoming false. No inference probes or credential mutations occur.
- Task ownership can show an explicitly reported child-session key; imported agent identities remain explicitly unreported. No owners are inferred from names.
- Channel and embedding unprobed states remain distinct from failures. Bundled plugin versions are not fabricated. These existing safeguards were retained, not treated as upstream repairs.
- Earlier fixes in the same working tree add missing task placeholders, corrected chart/session headings, disabled pagination boundaries, and a mandatory Node frontend gate in Make/CI/GoReleaser. Contributor/runtime docs now describe this gate. Runtime compatibility documentation is included in release archives/Homebrew packaging.

## Validation

Test-first failures reproduced missing token-state and configured-pricing projections, cost/session reason helpers, and backup/auth/task rendering. Tests exercise the embedded renderer as well as helper functions.

- `make frontend-test`: 24 groups passed.
- Root frontend syntax/escaping/regression tests and release-gate contract: passed with race detector.
- `go test -race ./internal/apprefresh ./internal/appopenclaw -skip '^TestReadyzProbe_ParsesFailingFromHTTP503$' -count=1`: passed. This is a supplemental sandbox-compatible run, **not** a full-suite pass; normal tests were not removed or weakened.
- `make vet` and `make lint`: passed (zero lint issues).
- Full `make check`: attempted; listener-dependent tests fail with `bind: operation not permitted`.
- Pinned govulncheck and Staticcheck: attempted individually; blocked by DNS access to `proxy.golang.org`.
- Production-style Go 1.27.1 build: passed. Nonfatal module-stat-cache write warning occurred outside the writable sandbox.
- Artifact: `dist/openclaw-dashboard-data-reasons`, SHA-256 `c10019e53e1d52a878de8cdfdd1036e28a785d1a03e4169083bc15cc32147815`.
- Live desktop field-reason verification passed after the user restarted the candidate; see the scoped evidence below. This is not full release certification.

### Live follow-up after restart

On 2026-09-06 at approximately 07:42–07:46 UTC, the existing `http://localhost:8081` browser tab was reloaded and inspected through computer use. The new field-reason UI was present, and the selected container reported OpenClaw 2026.9.2. Manual Refresh advanced the displayed session collection timestamp from `07:42:07Z` to `07:44:09Z` without losing the All Time usage selection.

- All 17 source collections displayed `ready`. This establishes successful collection, not completeness of every upstream field.
- Gateway PID `3`, uptime `15m 44s`, and RSS `598.5 MiB` were populated at the initial observation; these are time-specific values.
- The automation session displayed `Stale token count` for tokens and context utilization, matching `tokenState: stale`, `contextState: tokens_stale`, and null current tokens in the collected snapshot. The two other sessions retained numeric counts.
- Pricing disclosure showed 42 MiniMax M3 and 8 Mimo 2.5 Pro unpriced historical entries, with all four configured base rates zero for each model. Cost cards retained the known subtotal `$0.047708`, explicitly not a total. Today's two unpriced Mimo entries were distinguished from the all-history 50.
- All Time usage displayed exactly `1,076,171` tokens, matching `usageAll.totalTokens`. Genuine zero-cost delivery-mirror records still displayed `$0.00`. The 30-day chart showed the token fallback and its date range instead of fabricated cost data.
- Authentication rows distinguished CLI unresolved credentials from the explicit false missing-provider-in-use flags. Backup rows distinguished absent runtime status from omitted explicit configuration. Neither section claimed verified inference or restorable backups.
- Explicit child-session identities rendered for tasks that supply them; remaining missing owners and imported agent identities stayed visibly unreported.
- Pricing, backup, auth, collection, plugin and skill disclosures opened successfully. Open diagnostic tables had no blank cells; the expanded page had no literal `Unknown` labels. Explained unavailable/unprobed states and historical error logs remain intentionally visible.
- Desktop screenshots verified cost-card/donut and diagnostic-table layout. No horizontal document overflow or browser console errors were observed. Current mobile layout and full action workflows were not certified by this focused retest.

The displayed upstream logs also contained a setup-inference authentication failure followed later by a successful local-provider Mimo request. These are different execution paths; the successful local request does not establish that gateway authentication is repaired. No inference was initiated by this validation.

## Upstream work still pending

The last inspected candidate snapshot reported fresh token collection but 50 unpriced all-history entries (MiniMax M3 42, Mimo 2.5 Pro 8), an automation session without a current token count, empty backup status, and four CLI auth kinds of `missing` with explicit false missing-provider-in-use flags. These counts are time-specific, not hardcoded acceptance criteria.

1. Confirm direct API versus subscription/reseller billing and actual rates before correcting selected-runtime pricing. [OpenClaw pricing configuration](https://github.com/openclaw/openclaw/blob/main/docs/reference/token-use.md) uses `models.providers.<provider>.models[].cost` in USD per million tokens. [MiniMax's official pricing](https://platform.minimax.io/docs/guides/pricing-paygo) distinguishes standard/priority and long-context tiers. Applying one flat rate without checking the plan is unsafe for correctness.
2. Verify raw selected-runtime session freshness and auth resolution. Direct Podman socket access is sandbox-denied; do not inspect the host installation as if it were the selected container.
3. Verify whether the deployed OpenClaw version exposes a supported backup status source. Do not fabricate backup health from cron scheduling or attempt restoration against live state.
4. Run the unrestricted full gate and live native/Docker/Podman acceptance checks. Probe/inference/action tests need an appropriate disposable setup; they are not silently enabled on live jobs.

No runtime credentials, upstream configuration, historical records, services, release tags or deployment state were changed by this repair.
