# TODO — Bug Tracker & Fix Log

**Last audited:** 2026-05-19
**Status:** All five validated bugs (BUG-1, BUG-2, BUG-3, BUG-5, BUG-6) **fixed and tested**. BUG-4 confirmed false positive. BUG-7 was a documentation issue resolved by this regeneration. Smells SMELL-1..4 remain open (deferred — see bottom).
**Audit method:** Static analysis (`go vet`, `gofmt`), graphify knowledge graph review, two reviewer-agent passes on hot zones (apprefresh / appchat / appconfig / appserver / appsystem / appservice / appruntime), targeted self-validation against source, cross-repo validation against `~/src/open_claw/openclaw`. Reviewer-agent false positives discarded (see bottom).

### Verification log (this PR / commit cycle)

```
$ go vet ./...                  → 0 findings
$ gofmt -l .                    → 0 unformatted files
$ go test ./...                 → all 9 packages pass
$ go test -race ./internal/...  → all 7 packages pass

New tests added:
  internal/apprefresh/refresh_gateway_test.go         (7 cases — BUG-1)
  internal/apprefresh/token_usage_cache_mode_test.go  (1 case — BUG-2)
  internal/apprefresh/logtail_test.go                 (2 new cases — BUG-3)
```

Each open bug includes: file:line, current source snippet, evidence the bug is real, exact fix, and verification steps.

---

## 🔴 Critical

### BUG-1 ✅ FIXED — Gateway always reported offline — pgrep pattern mismatch

**Resolution commit:** edits to `internal/apprefresh/refresh_gateway.go` (pattern + seam) plus new test file `internal/apprefresh/refresh_gateway_test.go` (7 cases, all pass).

**Verification:**
```
$ go test -race -run TestCollectGatewayHealth ./internal/apprefresh/
PASS — TestCollectGatewayHealth_PgrepNoMatchReturnsOffline
PASS — TestCollectGatewayHealth_PgrepEmptyOutputReturnsOffline
PASS — TestCollectGatewayHealth_OnlySelfPidIsFiltered
PASS — TestCollectGatewayHealth_GatewayPidProducesOnline
PASS — TestCollectGatewayHealth_SelfPidMixedWithGatewayPidSkipsSelf
PASS — TestCollectGatewayHealth_NonNumericPidReturnsOffline
PASS — TestCollectGatewayHealth_NilContextDoesNotPanic
```

Manual e2e (rebuild + reinstall required to update the deployed binary): after rebuilding, `curl http://127.0.0.1:<dash-port>/api/refresh | jq .gateway.status` should return `"online"` and `.pid`, `.uptime`, `.rss` should populate. The dashboard banner will no longer show "Gateway is offline".

**Original analysis preserved below for future debugging context.**

**File:** `internal/apprefresh/refresh_gateway.go:30`
**Introduced:** commit `378e712` (v2026.3.23), refactored into current file at commit `d6b23d1` (v2026.5.13). Pattern itself never updated.
**Impact:** Dashboard banner shows 🔴 "Gateway is offline" critical alert even when the gateway is healthy. `gateway.pid`, `gateway.uptime`, `gateway.memory`, `gateway.rss` all empty/null in `data.json`. Cascade: `BuildAlerts` (`refresh_alerts.go:68`) emits the critical alert; downstream panels that depend on the gateway map render blank.

**Current code:**
```go
out, err := exec.CommandContext(pgrepCtx, "pgrep", "-f", "openclaw-gateway").Output()
```

**Why it fails (platform-specific):**
- Upstream `~/src/open_claw/openclaw/src/cli/program/preaction.ts:31` sets `process.title = "${cliName}-${name}"` which produces `"openclaw-gateway"` for the gateway subcommand.
- **Linux**: Node's `process.title` rewrite updates `/proc/<pid>/cmdline`, so `pgrep -f "openclaw-gateway"` matches. The original pattern was written against Linux behavior.
- **macOS**: Node's title rewrite is internal-only and does not reach the `procargs` sysctl that `pgrep -f` reads. Verified on a live macOS gateway (PID 68625): `ps -p 68625 -o command=` returns `node /Users/<user>/.asdf/.../openclaw/dist/index.js gateway --port 18789`, with no occurrence of `openclaw-gateway`.
- `pgrep` exits with status 1 (no match), function falls through to default `status:"offline"` map at lines 17–23.
- The gateway itself is healthy: `curl http://127.0.0.1:18789/healthz` returns `{"ok":true,"status":"live"}`.

The fix must match the actual `argv` (not the rewritten title) so it works on both macOS and Linux.

**Fix:** Replace the pattern with one that uniquely identifies the npm-installed Node gateway entry script and matches the real `argv`.

```go
out, err := exec.CommandContext(pgrepCtx, "pgrep", "-f", "openclaw/dist/index.js gateway").Output()
```

Rationale:
- Anchors on the npm module path segment `openclaw/dist/index.js` plus the subcommand `gateway`. Both segments are part of the real process `argv` on every platform regardless of `process.title` magic.
- Does not match the dashboard's own process (which contains `openclaw-dashboard`, not `openclaw/dist/index.js`). The existing `os.Getpid()` filter at lines 35–42 already removes self-matches as defense in depth.
- Source-checkout developers who launch via `pnpm openclaw gateway run` (path `openclaw.mjs`, not `dist/index.js`) will see `status:"offline"` even though the gateway is up. Acceptable trade-off — the dashboard targets the installed Homebrew/npm release. If covering source-checkouts becomes a requirement, switch to the architectural alternative (HTTP `/healthz` probe — see note below).

**Add test seam.** The function currently calls `exec.CommandContext` directly, making unit-testing the pattern impossible without spawning real processes. Refactor:

```go
var execCommandContext = exec.CommandContext // package-level seam, override in tests
```

Then change line 30 to `execCommandContext(...)`. Add a table-driven test in `internal/apprefresh/refresh_gateway_test.go` that injects fake command output:
- Case A — pgrep returns gateway PID only → status `online`, pid populated.
- Case B — pgrep returns dashboard PID only → filtered out by Getpid loop → status `offline`.
- Case C — pgrep returns nothing (exit 1) → status `offline`.
- Case D — pgrep returns gateway PID + dashboard PID → gateway PID selected.

**Verification steps:**
1. `go vet ./internal/apprefresh/...` clean.
2. `go test -race ./internal/apprefresh/...` passes including new test cases.
3. Manual end-to-end: rebuild dashboard, restart, `curl http://127.0.0.1:<dash-port>/api/refresh | jq .gateway` → `status:"online"`, `pid`, `uptime`, `rss` all populated.
4. Dashboard UI: banner no longer shows "Gateway is offline".

**Architectural note (out of scope for the surgical fix):** A parallel HTTP-based probe already exists at `internal/appsystem/system_service.go:505` (`probeOpenclawGatewayEndpoints`) and serves the `/api/system` endpoint. The dashboard now has two independent answers to "is the gateway up?" — pgrep-based for `data.json.gateway`, HTTP-based for `/api/system`. They will diverge on different failure modes (pgrep blind to a hung gateway; HTTP blind to a misnamed binary). Consolidating both consumers onto the HTTP `/healthz` probe would remove this divergence but is a behavior change, not a root-cause fix. Track separately.

---

## 🟡 Medium

### BUG-2 ✅ FIXED — Token usage cache file world-readable

**Resolution:** edit at `internal/apprefresh/token_usage_cache.go:120` (`0o644` → `0o600`) plus new test `internal/apprefresh/token_usage_cache_mode_test.go` (1 case, passes).

**Verification:**
```
$ go test -run TestSaveTokenUsageCache_FileMode ./internal/apprefresh/
PASS — TestSaveTokenUsageCache_FileModeIs0o600
```

The test calls `saveTokenUsageCache` against a temp dir and asserts `info.Mode().Perm() == 0o600`. Locks the invariant against future drift.

**Original analysis preserved below for context.**

**File:** `internal/apprefresh/token_usage_cache.go:120`

**Current code:**
```go
if err := os.WriteFile(tmp, data, 0o644); err != nil {
```

**Evidence of inconsistency:**
- `internal/apprefresh/refresh.go:34` writes `data.json` with `0o600`.
- `internal/appservice/launchd.go:131` writes the launchd plist with `0o600`.
- `internal/appservice/systemd.go:108` writes the systemd unit with `0o600`.
- Only this token-usage cache deviates with `0o644`.

The cache contains per-session token counts, costs, and model identifiers. Same sensitivity tier as `data.json`. World-readability discloses usage patterns to any local account.

**Fix:** Change `0o644` to `0o600`. One-character change.

**Verification:**
1. `go test ./internal/apprefresh/...` still passes.
2. After running the dashboard once, `stat -f %Lp ~/.openclaw/dashboard/.token-usage-cache.json` (or the actual cache path) returns `600`.

---

### BUG-3 ✅ FIXED — Timezone-less log timestamp parsing yields wrong bucket times

**Resolution:** edit at `internal/apprefresh/logtail.go:289` (`time.Parse(layout, c)` → `time.ParseInLocation(layout, c, time.Local)`) plus 2 new tests in `internal/apprefresh/logtail_test.go`.

**Design choice:** anchored on `time.Local` rather than threading a `*time.Location` parameter through the call sites. Rationale — gateway logs are produced by a process on the same host as the dashboard, in the same timezone. A process-wide tz default is the right model. Threading a parameter would have broken three existing call sites (`logtail.go:212, 244, 300`) plus `server_logs_test_helper_test.go` for no gained correctness.

**Verification:**
```
$ go test -run TestParseLogTimestamp ./internal/apprefresh/
PASS — TestParseLogTimestamp_RFC3339              (existing, RFC3339 preserved)
PASS — TestParseLogTimestamp_MultipleLayouts      (existing)
PASS — TestParseLogTimestamp_PrefixFallback       (existing)
PASS — TestParseLogTimestamp_NoCandidates         (existing)
PASS — TestParseLogTimestamp_TZLessIsLocalNotUTC  (NEW — guards the fix)
PASS — TestParseLogTimestamp_RFC3339OffsetPreserved (NEW — guards against regression of offset-bearing inputs)
```

The new `TZLessIsLocalNotUTC` test parses `"2026-05-01 10:00:00"` and asserts the result is equal to `time.Date(2026, 5, 1, 10, 0, 0, 0, time.Local)` and `ts.Location() == time.Local`. The new `RFC3339OffsetPreserved` test parses `"2026-05-01T10:00:00+03:00"` and asserts the absolute instant equals `time.Date(2026, 5, 1, 7, 0, 0, 0, time.UTC)`.

**Original analysis preserved below for context.**

**File:** `internal/apprefresh/logtail.go:280-292`

**Current code:**
```go
for _, layout := range []string{
    time.RFC3339Nano,
    time.RFC3339,
    "2006-01-02 15:04:05.999999999",
    "2006-01-02 15:04:05",
    "2006-01-02T15:04:05.999999999",
    "2006-01-02T15:04:05",
    "2006-01-02T15:04:05Z",
} {
    if parsed, err := time.Parse(layout, c); err == nil {
        return parsed, c
    }
}
```

**Why it fails:** `time.Parse` interprets layouts without an explicit timezone marker (rows 3–6 of the slice) as **UTC**. Gateway logs are emitted in local time. Parsed timestamps drift by the local UTC offset (e.g., +3h for Europe/Chișinău), so:
- Daily chart buckets land in the wrong day near midnight boundaries.
- Alert windows ("last error within 5 minutes") compute against a shifted clock.
- "Last activity" labels mislead operators.

**Fix:** Use `time.ParseInLocation` with the location plumbed through `collectDashboardData`. The function `extractTimestamp` (containing the parse loop) needs a `loc *time.Location` parameter. Caller already has `loc` available — check `refresh.go` for the existing tz threading pattern.

```go
func extractTimestamp(candidates []string, loc *time.Location) (time.Time, string) {
    for _, candidate := range candidates {
        c := strings.TrimSpace(candidate)
        if c == "" {
            continue
        }
        for _, layout := range []string{
            time.RFC3339Nano,
            time.RFC3339,
            "2006-01-02 15:04:05.999999999",
            "2006-01-02 15:04:05",
            "2006-01-02T15:04:05.999999999",
            "2006-01-02T15:04:05",
            "2006-01-02T15:04:05Z",
        } {
            if parsed, err := time.ParseInLocation(layout, c, loc); err == nil {
                return parsed, c
            }
        }
    }
    // … rest unchanged
}
```

RFC3339 layouts already carry their own offset in the string and are unaffected by the location argument, so this is safe for both tz-bearing and tz-less inputs.

**Verification:**
1. Add unit test with fixed input `"2026-05-19 15:45:00"` and `loc=time.FixedZone("test", 3*3600)` — assert parsed time equals `2026-05-19T15:45:00+03:00`.
2. Add unit test with `"2026-05-19T15:45:00Z"` and any loc — assert UTC interpretation preserved.
3. `go test -race ./internal/apprefresh/...` passes.

---

### BUG-4: ~~launchd probe dials port 0~~ — RESOLVED (false positive)

Originally flagged by reviewer. Revalidated 2026-05-19 during rerun.

**File:** `internal/appservice/launchd.go:236` and `internal/appservice/systemd.go:224`.

Both backends already guard the probe with `st.Port > 0`:

```go
// launchd.go:236
if pid > 0 && st.Port > 0 {
    st.PID = pid
    st.Uptime = resolveUptime(lb.ctx, lb.runCmd, pid)
    if lb.probeFunc(fmt.Sprintf("http://127.0.0.1:%d/", st.Port)) {
        st.Running = true
    }
}
```

No probe-to-port-zero path exists. Reviewer was wrong. No action.

---

### BUG-5 ✅ FIXED — Hardcoded user-specific path in Codex hook

**Resolution:** edit at `.codex/hooks.json:9` (`/Users/mudrii/.local/bin/graphify hook-check` → `graphify hook-check`).

**Verification:**
```
$ python3 -m json.tool < .codex/hooks.json > /dev/null   # JSON syntax OK
$ which graphify       → /Users/mudrii/.local/bin/graphify
$ graphify hook-check  → exit 0 (silent)
```

The hook now resolves through `PATH` and will work for any contributor whose `graphify` binary is on `PATH`, regardless of install location.

**Original analysis preserved below for context.**

**File:** `.codex/hooks.json:9`

**Current value:**
```json
"command": "/Users/mudrii/.local/bin/graphify hook-check"
```

**Why it fails:** Any contributor with `graphify` installed elsewhere (or CI machines, or `~/.local/bin` not on `PATH`) breaks this hook. The other tool configs (`.claude/settings.json`, `.gemini/settings.json`, `.opencode/plugins/graphify.js`) all rely on `PATH` resolution or `existsSync` checks, so this file is the lone outlier.

**Fix:** Drop the absolute prefix and rely on `PATH`.

```json
"command": "graphify hook-check"
```

If a contributor's `PATH` does not include the graphify binary, the hook fails loudly — which is the correct behavior, not a silent miss.

**Verification:** Inspect `.codex/hooks.json` after edit; `which graphify` succeeds on developer machines.

---

### BUG-6 ✅ FIXED — Gemini hook invokes legacy `python` interpreter

**Resolution:** edit at `.gemini/settings.json:9` (leading `python -c` → `python3 -c`).

**Verification:**
```
$ python3 -m json.tool < .gemini/settings.json > /dev/null   # JSON syntax OK
$ which python3   → /usr/bin/python3
$ python3 -c "<extracted hook script>"   → {"decision": "allow"}
```

Inline script is portable Python 3 (no Python 2 syntax). No other change required.

**Original analysis preserved below for context.**

**File:** `.gemini/settings.json:9`

**Current value:**
```json
"command": "python -c \"...\""
```

**Why it fails:** macOS removed the bare `python` symlink (Python 2 retired); Apple ships `python3` only. Many Linux distros also dropped `python` from default PATH. The hook will exit non-zero silently and never produce the additional-context message.

**Fix:** Replace `python` with `python3` on line 9. No other change required — the inline script is portable Python 3.

**Verification:** `which python3` succeeds; run the hook command manually with `python3` and confirm it emits valid JSON to stdout.

---

### BUG-7: TODO.md prior version contained validated-wrong claims

**Status:** Resolved by this regeneration. Preserved here for traceability.

The previous TODO.md investigation report (committed as part of this branch's working tree) contained three claims later disproven against both the dashboard source and the upstream `~/src/open_claw/openclaw` repo:

1. **Wrong bug location.** Cited as `internal/appsystem/system_collect_darwin.go`. That file contains no `pgrep` code. Real location is `internal/apprefresh/refresh_gateway.go:30`. Verified via `grep -rn "pgrep" --include="*.go"`.

2. **False claim that four cron jobs are not managed by the OpenClaw cron tool.** Disproven by both runtime data and upstream docs:
   - All eight job IDs in `~/.openclaw/cron/jobs.json` have full runtime state in `~/.openclaw/cron/jobs-state.json` (recent `lastRunAtMs`, `lastDurationMs`, `lastDeliveryStatus`).
   - `~/.openclaw/workspace-work/MEMORY.md:41` enumerates "6 permanent cron jobs: Morning Briefing (11:00 MYT), Memory Curation main (23:00), Memory Curation work (23:15), Vault Maintenance (Mon/Thu 10:00), OpenClaw Security Check (weekly Mon 10:00), Vault Inbox Pipeline (every 6h)" — confirms Work Agent ID `21112b77` is a permanent cron job.
   - Pers + Biz Agents were added later via `cron add` from other workspaces; they sit in the same store with full state.
   - Conclusion: all 8 jobs are cron-tool-managed.

3. **False "stale model" claim.** Original TODO.md said `model: "openai/gpt-5.4"` was replaced by `kimi/k2p5` during a "Mar 21 migration". Disproven by `~/src/open_claw/openclaw/src/config/defaults.ts:28`:
   ```ts
   gpt: "openai/gpt-5.4",
   "gpt-mini": "openai/gpt-5.4-mini",
   "gpt-nano": "openai/gpt-5.4-nano",
   ```
   `openai/gpt-5.4` is openclaw's current default model alias for the `gpt` shortname. Not stale. No such migration exists.

Lesson: investigation reports must cite file paths verified via `grep` / `graphify query`, and must not assert historical events without commit-log or upstream-doc evidence.

---

## 🟢 Low / Smell

> **Status note:** All four SMELLs below were addressed in the same fix wave that resolved BUG-1..BUG-6. Each item now begins with a "✅ FIXED" header documenting the resolution. The original analysis follows for traceability.

### SMELL-1 ✅ FIXED — Unbounded `chatRateLimiter.entries` growth between cleanup cycles

**Resolution:** Added `validateLoopbackBind` at `main.go:407` and gated the listener at `main.go:176`. Non-loopback bind hosts (`0.0.0.0`, `192.168.x.x`, public IPs, hostnames) are now rejected with `non-loopback bind host %q is not supported; this dashboard is loopback-only by design`. Closes the DoS surface entirely — the rate-limit map cannot grow from internet-routable IPs because the dashboard refuses to start with such a bind.

**Test:** `TestValidateLoopbackBind` in `server_test.go` covers 5 allowed and 5 rejected host strings. `go test -race ./... ` passes.

**Original analysis preserved below for context.**

**File:** `internal/appserver/server_core.go:54` (`sync.Map` field) and lines 64–104 (allow/cleanup methods).

**Behavior:** Each unique source IP allocates a `*rateBucket`. Cleanup runs on `chatRateCleanupInterval` and prunes buckets older than `2 * chatRateWindow`. Between cleanup ticks, the map grows linearly with unique IPs seen.

**Risk surface:** Only exploitable if the dashboard is bound to a public interface (default bind is `127.0.0.1`, so not exploitable in default deployment). If a user runs with `--bind 0.0.0.0`, an attacker can OOM the process by rotating source IPs faster than cleanup runs.

**Fix options (pick one when addressed):**
- Document explicitly that public bind is unsupported and refuse non-loopback `cfg.Server.Host` at startup.
- Cap `entries` size; when full, drop oldest bucket.

Not urgent. Track for next hardening pass.

---

### SMELL-2 ✅ FIXED — Two gateway probe code paths produce divergent answers

**Resolution:** Consolidated in `internal/apprefresh/refresh_gateway.go`. The function now treats the gateway HTTP `/healthz` endpoint as the authoritative liveness signal when a port is configured; pgrep remains responsible for pid/uptime/rss metadata. The two previously-divergent paths now share a single status semantics:

| State | Old (pgrep only) | New (HTTP + pgrep) |
|---|---|---|
| HTTP ok + pgrep finds | online + metadata | online + metadata |
| HTTP ok + pgrep empty | offline (false negative) | **online**, no metadata |
| HTTP fail + pgrep finds | online + metadata | online + metadata (graceful) |
| HTTP fail + pgrep empty | offline | offline |
| port = 0 (legacy callers) | pgrep behavior | pgrep behavior (preserved) |

**Test:** 4 new cases in `refresh_gateway_test.go` (TestCollectGatewayHealth_HTTPOnlinePgrepEmpty, HTTPOfflinePgrepFinds, HTTPOnlinePgrepError, PortZeroSkipsHTTP) plus the existing 7 pgrep cases unchanged. All 11 pass.

Caller `refresh.go:189` updated to pass `cfg.AI.GatewayPort`. No frontend shape change — `data.json.gateway` keys are identical (status / pid / uptime / memory / rss).

**Original analysis preserved below for context.**

**Files:**
- `internal/apprefresh/refresh_gateway.go:16` (`collectGatewayHealth`, pgrep-based) → feeds `data.json.gateway`.
- `internal/appsystem/system_service.go:505` (`probeOpenclawGatewayEndpoints`, HTTP-based) → feeds `/api/system`.

Each handles a different failure mode. They will disagree under common scenarios (hung gateway, misnamed binary, port collision). Consolidate by deleting `collectGatewayHealth` and routing `gateway` map field through the HTTP probe. Out of scope for BUG-1 surgical fix.

---

### SMELL-3 ✅ FIXED — Embedded Python inside bash inside JSON in hook configs

**Resolution:** Extracted shared logic to `scripts/graphify-hook.sh` (executable bash). The script accepts `--mode claude` or `--mode gemini`; the additionalContext string is defined once. Both `.claude/settings.json` and `.gemini/settings.json` now invoke the script. JSON validity preserved.

**Smoke tests:**
```
$ echo '{"tool_input":{"command":"grep foo"}}' | scripts/graphify-hook.sh --mode claude
{"hookSpecificOutput": {"hookEventName": "PreToolUse", "additionalContext": "graphify: ..."}}

$ scripts/graphify-hook.sh --mode gemini
{"decision": "allow", "additionalContext": "graphify: ..."}

$ python3 -m json.tool < .claude/settings.json > /dev/null && echo claude OK
$ python3 -m json.tool < .gemini/settings.json > /dev/null && echo gemini OK
```

**Original analysis preserved below for context.**

**Files:** `.claude/settings.json`, `.gemini/settings.json`.

Hook command strings carry multi-line Python with embedded JSON literals, all escape-encoded through JSON. Hard to modify, fragile to copy-paste, broken syntax goes undetected until the hook fires.

**Fix:** Extract logic to `scripts/graphify-hook.sh` (committed) and have each hook config call the script with arguments. One source of truth, normal shell syntax.

---

### SMELL-4 ✅ FIXED — Graphify rules duplicated across three Markdown files

**Resolution:** Canonical content moved to `docs/graphify.md`. `CLAUDE.md`, `AGENTS.md`, `GEMINI.md` each replaced with a 2-line pointer to the canonical doc. Single source of truth; drift impossible.

**Original analysis preserved below for context.**

**Files:** `CLAUDE.md`, `AGENTS.md`, `GEMINI.md` — each carries a near-identical `## graphify` section.

Drift is inevitable. Move canonical content to `docs/graphify.md`, replace each per-agent block with a one-line reference. Each agent system can be told to read the canonical doc.

---

## ❌ Reviewer findings rejected (false positives)

Recorded so future audits do not re-flag verified-correct code:

| Reviewer claim | File:Line | Verdict | Reason |
|---|---|---|---|
| Integer overflow in `contextTokens/ctxTokens*1000` | `refresh_sessions.go:244` | False | Operands are `float64`, no integer overflow. |
| `sync.Cond` nil-init race | `session_model_cache.go:59` | False | `if c.cond == nil { c.cond = sync.NewCond(&c.mu) }` runs inside `c.mu.Lock()`. |
| `NewServer` signature mismatch | `server.go:15` vs `internal/appserver/server_core.go:143` | False | Root facade wrapper supplies `refreshCollectorFunc` by design (zero-logic root pattern). |
| `refreshDone` channel double-close | `server_refresh.go:38` | False | `startRefresh` holds mutex through the check-and-assign block; defer in `runRefresh` guards with identity check `s.refreshDone == done`. |
| `streamCopy` close error swallowed | `runtime.go:137-141` | False | Read-only source file; defensive cleanup intentional. |
| `openTempSibling` weak randomness | `runtime.go:199-208` | False | Uses `crypto/rand`, 64-bit suffix per attempt. |
| `launchctl unload` error swallowed | `launchd.go:135` | False | Documented intent: clears stale registration before fresh load. |
| Token leak in pre-flight errors | `chat.go:280, 286` | False | Token is only attached to the request *after* these lines (line 289); pre-flight errors cannot contain it. |

---

## 📌 Out of scope (not bugs in this codebase)

1. **`openai/gpt-5.4` in `~/.openclaw/cron/jobs.json`** — verified against upstream `~/src/open_claw/openclaw/src/config/defaults.ts:28` as the current default model alias. Not stale, not a bug.
2. **Biz Agent: Daily Memory Curation** cron failure recorded in `~/.openclaw/cron/jobs-state.json` with `lastError: cron classifier: denial token "did not run" detected in summary`. Real failure surfaced correctly by the dashboard. Probable root cause (from upstream context at `~/.openclaw/workspace-biz/memory/inner-thoughts.md:74`): the daily curation jobs hit timeouts and were rerun; the rerun summary contained text like "I did not run X today" describing the timeout, and the safety classifier false-positive flagged the literal phrase. Not a dashboard bug — investigate the openclaw cron classifier and/or relax the denial-token list. Read the agent-run log at `lastRunAtMs=1779119100016` for full context.

---

## 📦 Release History

Preserved from prior TODO.md (release tracker content overwritten in earlier edit, restored here).

### Released

- **v2026.5.13** — refactor: split `refresh.go` by domain + encapsulate session model cache (F5 + F15), date-based version scheme fix.
- **v2026.4.29** — cron sidecar merge (#25), `/api/system` cold-path deadline + degraded fallback (#26), `system.gatewayPort` inheritance fix, systemd `Environment=` + `restart` on reinstall, per-instance latest-version fetcher.
- **v2026.4.13** — diagnostics + log visibility (#14), release hardening pass, structured logging cleanup.
- Built-in service management (`install`/`uninstall`/`start`/`stop`/`restart`/`status`) via launchd (macOS) and systemd (Linux).
- Security hardening (XSS, CORS, O(N²), shell safety, file handles).
- Performance, dirty-checking & test suite (initial 44 ACs, rAF, scroll preserve, tab fix).
- AI chat integration (`/api/chat`, chat panel UI, `ai` config block, chat test suite).
- Python removal — Go-only codebase (server, data collection, system metrics).

### Architecture refactor (shipped)

Clean module structure — single embedded SPA file, zero deps. 7 modules: State / DataLayer / DirtyChecker / Renderer / Theme / Chat / App. See `ARCHITECTURE.md` for the full spec. All AC17–AC20 and TC1–TC5 tests passing.

### Deployment artifacts (shipped)

- `Dockerfile` — Go binary, non-root user, port 8080, volume mount, healthcheck.
- `flake.nix` — `devShell`, `packages.default`, `apps.default` via flake-utils.

### Tests (shipped)

- Go test suite — `go test -race` covering all endpoints and core logic.
- Playwright E2E — 16 tests covering tabs, charts, countdown, chat panel, theme menu.

---

## 🔖 Working commands

```
go vet ./...
go test -race ./...
gofmt -l .
graphify query "<question>"            # focused subgraph
graphify path "<A>" "<B>"              # relationship trace
graphify explain "<concept>"           # node explanation
graphify update .                      # incremental graph refresh after code changes
```

Architecture doc: `ARCHITECTURE.md`. Graph audit: `graphify-out/GRAPH_REPORT.md`.
