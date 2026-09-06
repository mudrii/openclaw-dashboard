# Field-fix candidate: live desktop UI replay

Date/time: 2026-09-05, approximately 15:32–15:43 UTC (23:32–23:43 Asia/Kuala_Lumpur).
Target: `http://localhost:8081/`, OpenClaw Candidate - Go 1.27.1, selected `openclaw` container, OpenClaw/host CLI 2026.9.1.

Computer Use exercised the running embedded UI. This is a desktop replay at a 1074px viewport, not native/Docker end-to-end certification, mobile replay, or a passing full release gate. No inference, automation execution, abort, credentials, permissions, or deployment was performed.

## Repaired fields observed live

The first page load displayed a persisted pre-fix snapshot: PID/uptime absent and never-run durations zero. After the new collection completed, all 17 collection rows were ready, including `gateway.system.info`. This startup transition must not be mistaken for a failed new collector.

- PID **3**, uptime **5h 17m 4s**, RSS **755.2 MiB** appeared consistently in both gateway summaries; subsequent refreshes advanced uptime and changed RSS. Later observation: 5h 26m 27s and 708.8 MiB. These are selected-container metrics, not asserted host process metrics.
- Backup and disabled heartbeat durations changed to **Not reported**. Their absent run/schedule states were explicit. Existing durations remained 0.5s, 1.6s, and 0.1s.
- Today token totals initially showed input 217,333, output 16,983, cache-read 486,388, total **720,704**. Usage bars were 2%, 100%, 20% despite missing prices. A later disk snapshot's raw component sum equalled its authoritative `usageToday.totalTokens` (**887,036**). Activity continued during testing; the final UI showed 931,772, so timestamped values are not fixed assertions.
- Seven/30-day charts displayed seven/30 token bars. Unknown model costs remained unknown, with known-subtotal and unpriced-model explanations; no fake zero-dollar total was introduced.
- Session summary showed **0 active / 3 stored**. No-goal fields displayed Not applicable.
- Skills showed **94 agent-skill entries, 61 eligible, 17 model-visible**; the detailed inventory contained 94 rows.
- Telegram/default showed connected Yes in both account and configuration views. Probe was Not probed, health Not reported. Embedding status was Not checked. Missing bundled plugin versions were Not reported.

## Interaction matrix

| Component | Result |
| --- | --- |
| Manual refresh and continuing collection | Pass: timestamps, uptime, RSS and usage changed; ready collection metadata observed |
| Collapse All / Expand All | Pass: all 12 section ARIA states changed correctly |
| Each individual section toggle | Pass: each of 12 collapsed and expanded independently |
| Keyboard section toggle | Pass: Enter toggled Charts |
| Usage Today / 7d / 30d / All Time | Pass: active tab and totals changed; initial totals 720,704 / 746,861 / 799,594 / 861,176 |
| Chart 7d / 30d | Pass: selected state and seven/30 bars; restored seven days |
| Sub-agent Today / 7d / 30d / All Time | Pass: one today row versus five longer-range rows; restored Today |
| Task search | Pass: nonexistent query yielded zero rows; exact run ID yielded one |
| Task runtime/status filters | Pass: cron yielded 15 rows; running/failed zero, succeeded 15; reset All yielded 21 |
| Pagination boundary controls | No out-of-range page: Next/Previous stayed on the only page; see usability issue below |
| Six themes | Pass: distinct computed background/foreground colors for Nord, Mocha, GitHub Light, Solarized Light, Latte, Midnight; restored Midnight |
| Five automation history dialogs | Pass: dreaming five runs, health seven, skill review one, backup/heartbeat zero; dialogs closed normally |
| All three session context dialogs | Pass: ready empty progress/widget responses; selected native-session links reflected the chosen session |
| Inventory disclosures | Pass: plugin 61 rows, skills 94, model readiness four, backup/diagnostics five; opened and closed |
| Log severity/source filters | Pass: Info/All populated; Warn/Error empty; Gateway/All selectable; legacy sources disabled |
| Invalid/valid log regex | Pass: `[` immediately showed Invalid regex; valid filter cleared the error and matched database-lock entries |
| Pause/resume and fast/normal log controls | Pass: states changed; restored resumed, normal 60s, All/All, empty regex |
| Error-feed sorting | Controls exercised; empty feed means non-empty ordering remains unverified |
| AI drawer / operations | Disabled chat explained, input/send disabled; operations hidden; no requests executed |
| Desktop layout / browser errors | Screenshot inspected; no page-wide overflow at 1074px; final error-log query returned no JavaScript errors |

Automation notes: UI rendering is deferred, so results were checked after a short settling interval. The browser tool's empty `fill` did not clear a field; select-all/backspace did. Theme selection leaves the menu open by design; it was closed explicitly. These were test-driver adjustments, not dashboard defects.

## Residual findings and unverified gates

1. **Heading mismatch:** the token fallback remains inside a card titled Daily Cost Trend. The inner Token trend — cost unavailable label is correct, but the outer heading should reflect the current metric.
2. **Heading mismatch:** the Sessions section still says Active Sessions while listing stored idle sessions. The repaired top summary and hierarchy are consistent; the section title is not.
3. **Pagination affordance:** Previous/Next remain enabled on a one-page list, although handlers clamp safely. Disable unavailable directions to avoid implying another page exists.
4. **Startup freshness:** a persisted older snapshot can display ready labels with old timestamps until the first new collection completes. Consider an explicit loading/stale-snapshot banner; startup should not suggest old data was just collected.
5. **Upstream data:** MiniMax/Mimo pricing remains incomplete. Automation session token/context values remain unknown. Four provider diagnostic rows report auth kind missing while missing-provider-in-use is No. Backup/diagnostic fields remain unreported. These were not repaired by inventing values or changing configuration.
6. **Upstream runtime:** logs still contain Config health-state write failed: database is locked. They arrive at INFO severity, so an empty warning/error feed does not mean this storage symptom is absent.
7. Phone/tablet sizes were **not rerun in this pass**. Native and Docker live targets, full unrestricted checks, multi-page populated fixtures, non-empty error sorting, complete-pricing live charts, paid inference and mutating operator actions remain unverified.

The running page was left on Midnight with sections expanded, diagnostic disclosures closed, Today usage/sub-agent ranges, seven-day charts, filters reset, logs resumed at normal rate, and dialogs/chat closed.
