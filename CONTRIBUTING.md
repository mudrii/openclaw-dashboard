# Contributing to OpenClaw Dashboard

Thanks for your interest in contributing!

## Quick Start

```bash
git clone https://github.com/mudrii/openclaw-dashboard.git
cd openclaw-dashboard

# Preferred repo commands
make build
make test
make lint
make check
```

---

## How to Contribute

1. **Fork** the repository
2. **Create** a feature branch (`git checkout -b feature/my-feature`)
3. **Write a failing test** before writing any implementation code
4. **Implement** the minimal code to make it pass
5. **Run the full test suite** — all tests must pass
6. **Commit** with a conventional message (`feat:`, `fix:`, `perf:`, `test:`, `docs:`)
7. **Open** a Pull Request

---

## Core Constraints

Before writing a line of code, understand these non-negotiable constraints:

- **Zero frontend dependencies** — no npm, no CDN, no external fonts, no build tools. The entire frontend is a single `web/index.html` with vanilla HTML/CSS/JS.
- **Zero Go external dependencies** — `go.mod` has no third-party modules; only the Go standard library is allowed.
- **Backend standard library only** — the Go server, refresh collector, and tests use `net/http`, `encoding/json`, `os/exec` (with `argv` slices, not shells), and other stdlib packages only.
- **Single file frontend** — all JS lives inside one `<script>` tag in `web/index.html`. No splitting into modules, no bundler.
- **7-module JS structure** — new JS must fit into the existing `State / DataLayer / DirtyChecker / Renderer / Theme / Chat / App` object hierarchy. Do not add globals outside these objects (except the four allowed utilities: `$`, `esc`, `safeColor`, `relTime`).
- **XSS safety** — every value inserted into the DOM via template literals must be wrapped in `esc()`. Never concatenate raw user data into HTML strings.

---

## Test Suite Overview

Automated tests live in both the repository root (`package dashboard`) and the
internal packages. Run the full suite before every commit:

```bash
make check
```

`make check` runs `go vet ./...`, `golangci-lint run ./...`, and `go test -race -count=1 ./...`.
The race detector is part of the expected workflow; do not skip it for local runs.

### What the Go tests cover

| File | Focus |
|------|--------|
| `server_test.go` | HTTP handlers, status codes, JSON bodies, CORS, debouncing behavior, static asset serving |
| `system_test.go` | `/api/system` schema, runtime collectors, gateway-related responses, caching and probe behavior |
| `chat_test.go` | Chat API contract and validation |
| `config_test.go` | `config.json` loading and defaults |
| `version_test.go` | Version string and build metadata behavior |

Tests use `net/http/httptest`, temporary directories, and real handler wiring where possible — prefer exercising the actual server types over heavy mocking.

### Running a subset

```bash
# Single test by regex
go test -race ./... -run TestHandleSystem_CORS -v

# One package file’s tests (same module, all in `.`)
go test -race -v -count=1 ./... -run '^TestYourName$'
```

### Refresh and `data.json`

`assets/runtime/refresh.sh` locates or builds the `openclaw-dashboard` binary and runs it with **`--refresh`**. The collector that writes `data.json` lives in **`internal/apprefresh`** (`RunRefreshCollector`, `collectDashboardData`, and helpers behind root compatibility wrappers).

When you change the shape of `data.json`, add or extend Go tests that assert the new keys and types (for example by unmarshaling into `map[string]any` or a small struct, or by calling exported helpers from `*_test.go` in the same package). If logic is hard to reach without a full OpenClaw home directory, use minimal fixtures under `t.TempDir()` and keep tests deterministic.

---

## What to Test (by Change Type)

### Changing `web/index.html` JS

There are no static-analysis tests for the frontend. Rely on:

- **Manual exercise** of the UI after your change
- **Code review discipline** for the 7-module structure and dirty-flag wiring
- **XSS audit** — search for template literals that insert dynamic data without `esc()` (see [Security Testing](#security-testing))

For behavior that is easy to get wrong (tab switching, chart toggles, scroll preservation), describe the manual scenario in the PR and run through it locally.

### Changing the Go HTTP server (`internal/appserver`, `internal/appchat`, etc.)

Add or extend tests in **`server_test.go`** or **`chat_test.go`**. Prefer `httptest.NewRecorder` and a real `ServeHTTP` call on the same handler stack the binary uses.

**Example pattern:**

```go
func TestMyEndpoint_GET(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, dir)
	req := httptest.NewRequest(http.MethodGet, "/api/my-endpoint", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := resp["expectedKey"]; !ok {
		t.Fatal("missing expectedKey")
	}
}
```

Always cover where it matters:

- Happy path (200 + correct body)
- Error path (4xx for bad input), if applicable
- CORS: allowed origin behavior matches existing tests — never `Access-Control-Allow-Origin: *` for credentialed-style use

### Changing `refresh.sh` and refresh collection logic

- **`assets/runtime/refresh.sh`** only resolves `OPENCLAW_HOME`, finds the binary, and runs **`openclaw-dashboard --refresh`**. Keep it small, `set -euo pipefail`, and avoid `eval` or string-built shell commands.
- **Data collection and `data.json` layout** belong in **`internal/apprefresh`**. When you add fields or change types, update Go tests (new test functions or table-driven cases) so CI catches schema drift.

### Changing CSS or themes

There is no automated visual regression suite. Required manual checks:

1. Build and run: `make build && ./openclaw-dashboard --port 8080`, then open `http://127.0.0.1:8080` (or your configured bind/port).
2. Switch through all 6 themes via the 🎨 button — verify nothing breaks.
3. Resize to a narrow viewport (under 768px width) — verify layout adapts.
4. Ensure new colors use the theme variable system (`var(--accent)`, not hardcoded hex) where appropriate.

### Adding a new theme

1. Add the theme object to the runtime `themes.json` defaults in `assets/runtime/themes.json` — include all required color keys used by existing themes.
2. Manually verify all themes still render (the menu regenerates dynamically).
3. If you only touch `assets/runtime/themes.json`, you typically do not need Go changes.

### Adding a new dashboard panel

1. **Data:** extend **`collectDashboardData`** in **`internal/apprefresh`** — add the new slice or object to the `map[string]any` returned to `data.json`.
2. **UI:** add Renderer (and optional `State` / `DirtyChecker`) wiring in **`web/index.html`**, following the 7-module pattern.
3. **Tests:** add a Go test that proves the new field is present and correctly typed under controlled fixtures, or add a handler/integration test if the panel is also driven by an API.

### Adding a new alert type

Alerts are built in **`BuildAlerts`** in **`internal/apprefresh`**. Each alert is a `map[string]any` with `type`, `icon`, `message`, and `severity` keys, appended to a slice.

**Example — new condition and alert:**

```go
if myCondition {
	alerts = append(alerts, map[string]any{
		"type":     "warning",
		"icon":     "📌",
		"message":  "Short human-readable explanation",
		"severity": "medium",
	})
}
```

Add or extend Go tests that construct inputs where `myCondition` is true and assert the alerts slice contains the expected entry (either by testing `buildAlerts` directly with stub arguments, or by asserting on generated `data.json` in a temp-dir fixture).

### Modifying chart rendering

When changing `renderCostChart`, `renderModelChart`, or `renderSubagentChart`:

1. Test both `chartDays = 7` and `chartDays = 30` views manually.
2. Verify with empty `dailyChart: []` (should render nothing, not throw).
3. Verify with a single data point (edge case for x-axis step calculation).
4. The `patchSvg()` cache means the chart only re-renders when data changes — verify the cache invalidates correctly when data does change.

### Modifying the agent tree / sessions

When changing `renderAgentTree` or the sessions section:

1. Test with 0 sessions, 1 session, and 20+ sessions (the table caps at 20 shown).
2. Test with nested sub-agents (3 levels deep).
3. Verify scroll position is preserved after re-render (the `scrollTop` save/restore pattern).

---

## Security Testing

Every change to HTML generation must be checked for XSS. The rule: **every dynamic value in a template literal must go through `esc()`**.

**Manual audit helper** — look for `${` insertions that are not wrapped in `esc(`:

```bash
grep -n '\${[^}]*}' web/index.html | grep -v 'esc(' | head -40
```

Review each hit; some may be safe literals, but anything derived from API or `data.json` must be escaped.

**CORS** — new or altered endpoints must keep the same origin-reflection rules as existing handlers. Covered by Go tests in `server_test.go` / `system_test.go`; do not widen to `*`.

**Subprocess and shell safety**

- Prefer **`exec.Command` / `exec.CommandContext` with a string slice argv** — never pass untrusted strings through `/bin/sh -c`.
- Keep **`assets/runtime/refresh.sh`** free of `eval` and of dynamically assembled commands from environment variables you do not fully control.

---

## TDD Workflow

Follow this cycle for every change:

```
1. Write a failing test that describes the behaviour you want
2. Run it: verify it fails for the right reason (not a compile error)
3. Write the minimal code to make it pass
4. Run the test again: verify it passes
5. Run the full suite: verify no regressions
6. Commit: test file + implementation file together
```

Never commit implementation without a test. Never write tests after the fact only to wrap existing code.

---

## Commit Messages

Use conventional commits. Keep the subject under 72 characters.

```
feat: add CSV export for token usage table
fix: reconcileRows drops orphaned rows on tab switch
perf: skip SVG re-render when data hash unchanged
test: cover new refresh field in data.json
docs: update CONTRIBUTING with apprefresh notes
refactor: extract donut chart logic into renderDonut()
```

---

## Pull Request Checklist

Before opening a PR, verify:

- [ ] All repo checks pass: `make check`
- [ ] New behaviour has a Go test in the appropriate `*_test.go` file (or a justified reason in the PR if not feasible)
- [ ] Any new HTML template literals use `esc()` on every dynamic value
- [ ] No new globals added outside the 7 module objects + 4 utilities
- [ ] Tested all 6 themes manually if any CSS was touched
- [ ] Tested 7d and 30d chart views if any chart code was touched
- [ ] `CHANGELOG.md` updated with the change under the correct version heading

---

## Ideas for Contributions

- [ ] CSV export for token usage table
- [ ] Session details modal on row click
- [ ] Cron history sparklines (last 7 runs)
- [ ] Keyboard shortcut to trigger manual refresh
- [ ] Alert silence / snooze button
- [ ] Configurable refresh interval from the UI (without editing `config.json`)

---

## Questions?

Open an issue or join the [OpenClaw Discord](https://discord.com/invite/clawd).
