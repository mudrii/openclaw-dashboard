# Audit Follow-ups

Items from the Sonnet/Opus deep-dive audits (2026-03-06) that are non-actionable or too architectural for the current hardening pass.

## Deferred Items

### 1. Extract embedded Python from refresh.sh → refresh_data.py
**Sonnet MAINT-1 | Effort: 2-3h**

The ~900-line `refresh.sh` contains a Python heredoc that isn't independently testable or IDE-navigable. Extracting to `refresh_data.py` with a `main()` entry point would enable direct pytest coverage and proper tooling support.

**Why deferred:** Functional correctness is unaffected. The heredoc is well-tested indirectly via `data.json` schema tests. Extraction is a refactor that should be done in isolation with its own test suite to prevent regressions.

**Proposed path:**
1. Extract heredoc to `refresh_data.py` with `def main(dir, openclaw_path)` entry point
2. `refresh.sh` becomes a thin wrapper: `python3 "$DIR/refresh_data.py" "$DIR" "$OPENCLAW_PATH"`
3. Add `tests/test_refresh_data.py` with unit tests for each data collection function
4. Validate output parity: `diff <(bash refresh.sh) <(python3 refresh_data.py)`

### 2. Python stale-while-revalidate for /api/refresh (match Go behavior)
**Sonnet §4 / Opus §3.1 | Effort: 2-3h**

Go's `handleRefresh()` returns cached `data.json` immediately and refreshes in background. Python's `handle_refresh()` blocks the request thread while running `refresh.sh` (up to 15s). With `ThreadingHTTPServer` (now applied), this doesn't block _other_ requests, but the requesting client still waits.

**Why deferred:** Behavioral change (first load would return stale/empty instead of fresh data). Needs UI-side handling for "no data yet" state. The threading fix (applied) eliminates the worst case (all-clients-blocked).

**Proposed path:**
1. Serve cached `data.json` immediately on `/api/refresh` (like Go)
2. Trigger `run_refresh()` in a background thread if stale
3. Add `X-Data-Stale: true` header when serving cached data
4. Frontend: detect stale header, show subtle "refreshing..." indicator

### 3. Browser E2E tests
**Opus §6.3 | Effort: 4-8h**

No browser-based end-to-end tests exist. All frontend tests are regex-based static analysis. Adding Playwright tests would verify actual rendering, theme switching, chart display, and interactive behavior.

**Why deferred:** Requires Playwright infrastructure in CI (browser install, test runner). The existing static analysis tests catch XSS, structural, and semantic issues effectively.

**Proposed path:**
1. Add `tests/e2e/` directory with Playwright test files
2. CI job: install Playwright browsers, start server, run E2E suite
3. Cover: dashboard render, theme switch, chat send, system metrics display

### 4. Go/Python parity test suite
**Opus §6.3 | Effort: 4h**

No automated check that both servers respond identically to the same requests. A shared acceptance test suite would catch future drift.

**Proposed path:**
1. Create `tests/parity/` with shared test definitions (JSON or YAML)
2. Test runner starts both Go and Python servers, sends identical requests
3. Compare: status codes, JSON keys, CORS headers, error shapes

### 5. Rate limiting on /api/chat
**Sonnet SEC-2 | Effort: 1-2h**

No per-minute rate limit on `/api/chat`. Any local process can spam the endpoint, forwarding to the LLM gateway and incurring costs. Request size limits (64KB body, 2000 char question) are in place.

**Why deferred:** Low risk since both servers only listen on localhost. Rate limiting adds complexity (token bucket state, configuration surface). The existing body size limits prevent the worst abuse vectors.

**Proposed path:**
1. Add `chat.rateLimit` config option (default: 10 req/min)
2. Go: `golang.org/x/time/rate` token bucket per IP
3. Python: simple sliding-window counter in `handle_chat()`
4. Return 429 with `Retry-After` header

### 6. Single-file frontend extraction
**Opus §5.3 | Effort: 4-8h**

`index.html` is 1636 lines of inline HTML/CSS/JS. Extracting JS/CSS to separate files with a minimal build step would improve code review, testing, and collaboration.

**Why deferred:** The current `//go:embed index.html` pattern is simple and works. Extraction requires a build step (esbuild/vite), which adds CI complexity.

### 7. Merge getDataRawCached/getDataCached into single cache layer
**Sonnet PERF-3 | Effort: 1h**

Both functions stat and read `data.json` independently. Merging into a single `loadData()` that fills both caches atomically would eliminate the (very minor) possibility of double-read on concurrent requests.

**Why deferred:** Current code is correct (double-check locking ensures coherence). The optimization is marginal since `data.json` is typically < 100KB.
