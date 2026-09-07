# Dashboard UI and data verification — 2026-09-07

## Scope and result

Reviewed the embedded dashboard against a freshly built server on port 8082 using the selected OpenClaw container, plus synthetic fixtures covering populated, empty, error, pagination, chat, and operation states. Computer control was used for the final hands-on verification. No production chat request or runtime operation was submitted.

All 17 original live collections were ready on the initial source check. The follow-up below adds gateway health and provider usage collectors. Temporary collection timeouts during validation were displayed as stale, retaining the last successful data, and subsequently recovered. This is a point-in-time UI and data validation, not a production release approval.

## Repairs

- Preserve explicitly configured zero reserve tokens, compaction threshold, Telegram group count, search result limit, and search cache lifetime.
- Name absent configuration values instead of displaying `undefined`, blank workspace/model cells, or an unexplained context-capability dash. An explicitly empty fallback list says `None configured`.
- Explain empty session, automation, sub-agent, and token-usage tables. Disable session inspection and hide the session link when there is no target; restore them when sessions return.
- Update the container gateway card when collection arrives after the initial system poll. PID, uptime, memory, version, and freshness use the selected container's data.
- Expand the error section when navigating from the error badge.
- Wrap header controls on narrow screens and label graphical token-volume bars for accessibility.

## Source versus display

| Fields | Verified source and interpretation |
| --- | --- |
| Gateway PID, uptime, RSS, version | `gateway.system.info` and `gateway.status`; values now populate both health and runtime views without waiting for another system poll. |
| Gateway health | Host readiness probes do not apply to the selected container. The follow-up adds its own `health` RPC verdict for Online/Unhealthy; stale or unavailable health remains explicit. Process metrics alone do not establish health. |
| Sessions, activity, context, tokens | `gateway.sessions.list`; 18 stored sessions in the inspected snapshot. Source-marked stale token counts remain explicitly stale. |
| Tasks, owners, runs, progress, duration | `gateway.tasks.list`; absent owner/agent metadata in imported records is explained, and zero-second durations are preserved. |
| Automation schedule and history | `gateway.cron.list` and history endpoint; disabled heartbeat has no recorded run or duration, which is explicitly stated. |
| Usage and charts | `gateway.sessions.usage`; token totals remain visible even when costs are incomplete. Fixture totals independently checked: 160 input + 40 output = 200 tokens, and $3 + $2 = $5. |
| Missing prices | Live snapshot had 12 unpriced entries today and 60 across history. Known subtotals are labelled as subtotals; no prices were invented. |
| Memory and index | Memory status and index collectors; an unperformed embedding probe says `Not checked`, while reported zero counters are shown as zero. |
| Channels | Account collector distinguishes connected state, unperformed probe, and whether an error was reported. |
| Plugin versions, model authentication | CLI inventory/status; missing versions and missing credentials remain explicit. Credential presence does not imply successful model inference. |
| Backup status | Diagnostics/configuration does not report enablement or last success/attempt; UI states the missing source fields. Scheduled backup job history alone does not prove backup integrity. |
| Configuration | Configuration collector plus agent/model inventory; zero, false, empty list, and absent values are rendered distinctly. |
| Graphical usage cells | These are token-volume bars, not missing data. They now have accessible labels. |

After expanding the live and fixture data panels, the inspected visible tables had no unexplained blank data cells. Source absences listed above remain genuine limitations.

## Verification evidence

- `make check`: frontend and workflow regressions, vet, lint, race tests across all Go packages, vulnerability scan, staticcheck, and build passed.
- `make frontend-test`: passed after the final regression additions, including zero settings, missing configuration, empty tables, container card arrival order, and empty-session control recovery.
- Computer control: all 12 section toggles, chart and usage time ranges, session pagination, task filtering, automation history pagination, session progress/widgets, Workboard summary, all diagnostic disclosures, six themes, log severity/regex/pause controls, error badge navigation, synthetic chat rendering, and empty-data recovery were exercised.
- Narrow-screen check at 390 pixels: document width stayed within the viewport after the header fix. Desktop and populated dashboard views were inspected visually.
- Earlier optional browser suite covered 18 groups, including all four synthetic operation actions and failure/recovery states. Its four red groups (mobile overflow, collapsed error navigation, zero configuration, empty tables) were repaired and retested through Computer control. Do not describe that earlier red report as a green automated run.
- Computer control verified missing-token validation. The in-app browser could not complete its native confirmation-dialog interaction; this repeat was stopped. Operation submission coverage comes from the earlier synthetic browser run, not a live runtime action.

## Reproduce

Run `make frontend-test` for dependency-free renderer regressions. For manual browser checks, run `node scripts/ui-fixture-server.cjs` and open `http://127.0.0.1:8083`; `/?scenario=empty` exercises empty session/automation/usage data, and `/?scenario=outage` exercises unavailable refresh/system endpoints. This fixture server binds only to loopback and never contacts OpenClaw.

The optional `scripts/ui-browser-smoke.cjs` requires an external Playwright installation selected with `PLAYWRIGHT_MODULE` and a running dashboard selected with `DASHBOARD_TEST_URL`. It intercepts every API endpoint with synthetic responses and writes its results under `dist/ui-browser-results`. It adds no application dependencies and is not part of the standard CI gate.

## Provider-page follow-up

The authenticated native Model Providers page exposed data absent from the first dashboard collection path. Added `gateway.health` and `gateway.usage.status` through the existing bounded, selected-runtime CLI adapter. The adapter distinguishes an unhealthy health verdict from a failed RPC envelope. Credential/error payloads and channel details are excluded from the new projections.

The cost section now displays provider plan names, quota percentages remaining, reset times, balances, and recorded 30-day session subtotals. It includes providers from model inventory and historical sessions, so a historical provider can appear even when it is no longer configured. Provider snapshot time and both collection statuses are visible. The main cost cards expose known dollar subtotals rather than hiding those amounts behind a missing-price label. Unknown historical prices remain explicitly incomplete; provider quota and balance data cannot establish those historical costs.

Computer verification compared the dashboard with the authenticated native provider page: MiniMax, z.ai, Copilot, and DeepSeek values were present. The selected container reported a successful health RPC and the dashboard displayed Online. Its own `/healthz` and `/readyz` also returned HTTP 200 during read-only diagnosis. The production implementation uses the portable CLI/RPC path and does not require direct access to container-engine sockets.

Regression coverage includes false health verdicts versus RPC errors, missing responses, credential projection, zero balances and quotas, known subtotal labelling, stale health, and independence from failed host-metric polls. No provider credential or historical billing record was changed.

## Design revision

The provider card section was subsequently removed at the user's request to restore the previous dashboard layout. Provider collection remains available in the data contract; replacement presentation is pending design selection. Gateway health and known-subtotal corrections remain in place.
