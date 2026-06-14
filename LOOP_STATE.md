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
