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
