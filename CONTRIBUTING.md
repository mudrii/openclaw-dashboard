# Contributing to OpenClaw Dashboard

Thanks for your interest in contributing!

## Quick Start

```bash
git clone https://github.com/mudrii/openclaw-dashboard.git
cd openclaw-dashboard

# Run all tests (no server needed for most)
.venv/bin/python3 -m pytest tests/ --ignore=tests/test_e2e.py -v

# Run the full suite including E2E (requires playwright)
.venv/bin/python3 -m pytest tests/ -v --timeout=30
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

- **Zero frontend dependencies** — no npm, no CDN, no external fonts, no build tools. The entire frontend is a single `index.html` with vanilla HTML/CSS/JS.
- **Zero backend dependencies** — `server.py` uses Python stdlib only (`http.server`, `json`, `subprocess`, `threading`, `argparse`). No pip installs.
- **Single file frontend** — all JS lives inside one `<script>` tag in `index.html`. No splitting into modules, no bundler.
- **7-module JS structure** — new JS must fit into the existing `State / DataLayer / DirtyChecker / Renderer / Theme / Chat / App` object hierarchy. Don't add globals outside these objects (except the four allowed utilities: `$`, `esc`, `safeColor`, `relTime`).
- **XSS safety** — every value inserted into the DOM via template literals must be wrapped in `esc()`. Never concatenate raw user data into HTML strings.

---

## Test Suite Overview

The suite has four tiers. Run the fastest tiers first.

| Tier | File(s) | Server needed? | Speed | What it catches |
|------|---------|----------------|-------|-----------------|
| **Static analysis** | `test_frontend.py`, `test_critical.py`, `test_nix_flake.py`, `test_dockerfile.py` | No | ~0.1s | Missing functions, missing security patterns, wrong structure |
| **Schema** | `test_data_schema.py` | No (reads `data.json`) | ~0.1s | `refresh.sh` output drift, missing keys, wrong types |
| **Server integration** | `test_server.py`, `test_chat.py` | No (starts own server) | ~3s | HTTP contract, CORS, debouncing, chat endpoint |
| **E2E** | `test_e2e.py` | No (starts own server) | ~15s | Real browser tab switching, chart toggle, countdown, chat panel |

### Running Individual Tiers

```bash
# Tier 1: static only (fastest feedback loop)
.venv/bin/python3 -m pytest tests/test_frontend.py tests/test_critical.py -v

# Tier 1 + 2: no server required
.venv/bin/python3 -m pytest tests/test_frontend.py tests/test_data_schema.py tests/test_critical.py tests/test_nix_flake.py tests/test_dockerfile.py -v

# Tier 1-3: full static + integration (no browser)
.venv/bin/python3 -m pytest tests/ --ignore=tests/test_e2e.py -v

# Tier 4: E2E (requires playwright — see setup below)
.venv/bin/python3 -m pytest tests/test_e2e.py -v --timeout=30

# Run a single test by name
.venv/bin/python3 -m pytest tests/test_frontend.py::TestFrontendJS::test_ac15_esc_defined_and_used -v
```

### Setting Up E2E Tests

```bash
.venv/bin/pip install playwright
.venv/bin/python3 -m playwright install chromium
```

---

## Acceptance Criteria (AC) System

Every test has an AC number. The AC number is the source of truth — it appears in:
- The test method name: `test_ac15_esc_defined_and_used`
- The docstring: `"""AC15: esc() function is defined and used..."""`
- `tests/README.md` (the authoritative AC registry)

**When adding a new test, claim the next available AC number** (currently AC29+) and register it in `tests/README.md`. Don't reuse or skip numbers.

### Current AC Map

| Range | File | Topic |
|-------|------|-------|
| AC1–AC8 | `test_server.py` | HTTP contract, CORS, debouncing |
| AC9–AC14 | `test_data_schema.py` | `data.json` schema |
| AC15–AC26 | `test_frontend.py` | JS patterns, XSS safety, module structure |
| AC27 | `test_dockerfile.py` | Dockerfile correctness |
| AC28 | `test_nix_flake.py` | Nix flake structure |
| AC-CHAT-1–8 | `test_chat.py` | AI chat endpoint |
| TC1–TC20 | `test_critical.py` | Mixed static + smoke |

---

## What to Test (by Change Type)

### Changing `index.html` JS

Pick the right test file based on what changed:

| You changed... | Write test in... | Test approach |
|----------------|-----------------|---------------|
| A new function or method | `test_frontend.py` | `assertIn("functionName(", self.html)` |
| An XSS escape path | `test_frontend.py` | `assertIn("esc(", self.html)` count check |
| A dirty flag or DirtyChecker logic | `test_frontend.py` | `assertIn("flagName", self.html)` |
| Visual rendering (DOM output) | `test_e2e.py` | Playwright snapshot check |
| A tab switch or chart toggle | `test_e2e.py` | Click + wait + assert class |
| A new `window.OCUI` method | `test_frontend.py` | `assertIn("OCUI.newMethod", self.html)` |

Static analysis tests (`test_frontend.py`) do not run JS — they use `re` and string matching on the raw source. Write them to verify structural invariants, not runtime behavior.

**Example: adding a new Renderer section**

```python
# In test_frontend.py, class TestFrontendJS:
def test_ac29_render_my_section_guarded_by_flag(self):
    """AC29: Renderer.renderMySection() only runs when flags.mySection is true."""
    self.assertIn("renderMySection(", self.html)
    match = re.search(r'flags\.mySection.*?renderMySection', self.html, re.DOTALL)
    self.assertIsNotNone(match, "renderMySection not guarded by flags.mySection")
```

### Changing `server.py`

Tests go in `test_server.py`. The server fixture starts a real subprocess on a random port — no mocking.

```python
# Pattern: start server, hit endpoint, assert response
def test_my_new_endpoint_returns_json(self):
    conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=5)
    conn.request("GET", "/api/my-endpoint")
    resp = conn.getresponse()
    self.assertEqual(resp.status, 200)
    data = json.loads(resp.read())
    self.assertIn("expectedKey", data)
```

Always test:
- Happy path (200 + correct body)
- Error path (4xx for bad input)
- CORS header is present and not `*`

### Changing `refresh.sh`

Tests go in `test_frontend.py` (static shell analysis) and `test_data_schema.py` (output schema).

For shell safety, check:
```python
def test_ac30_no_eval_in_refresh_sh(self):
    """AC30: No eval in refresh.sh."""
    self.assertNotIn("eval ", self.sh)
```

For output schema, add a key check to `test_data_schema.py`:
```python
def test_ac14b_new_key_present(self):
    """AC14b: new_key is present and is a list."""
    self.assertIn("new_key", self.data)
    self.assertIsInstance(self.data["new_key"], list)
```

### Changing CSS or Themes

No automated test exists for visual output. Required manual checks:

1. Open the dashboard in a browser (`python3 server.py &` then `open http://127.0.0.1:8080`)
2. Switch through all 6 themes via the 🎨 button — verify nothing breaks
3. Resize to mobile width (< 768px) — verify layout adapts
4. Check that new CSS variables use the theme variable system (`var(--accent)`, not hardcoded hex)

### Adding a New Theme

1. Add the theme object to `themes.json` — include all 19 required color keys
2. Manually verify all 6 existing themes still render (the menu regenerates dynamically)
3. No code changes to `index.html` or `server.py` needed

### Modifying Chart Rendering

When changing `renderCostChart`, `renderModelChart`, or `renderSubagentChart`:

1. Test both `chartDays = 7` and `chartDays = 30` views manually
2. Verify with empty `dailyChart: []` (should render nothing, not throw)
3. Verify with a single data point (edge case for x-axis step calculation)
4. The `patchSvg()` cache means the chart only re-renders when data changes — verify the cache invalidates correctly when data does change

### Modifying the Agent Tree / Sessions

When changing `renderAgentTree` or the sessions section:

1. Test with 0 sessions, 1 session, and 20+ sessions (the table caps at 20 shown)
2. Test with nested sub-agents (3 levels deep)
3. Verify scroll position is preserved after re-render (the `scrollTop` save/restore pattern)

---

## Security Testing

Every change to HTML generation must be checked for XSS. The rule: **every dynamic value in a template literal must go through `esc()`**.

Run the existing XSS test to verify:
```bash
.venv/bin/python3 -m pytest tests/test_frontend.py::TestFrontendJS::test_ac15_esc_defined_and_used -v
```

Then manually audit your new template literals:
```bash
# Find unescaped dynamic insertions — look for ${ not followed by esc(
grep -n '\${[^}]*[^c][^)]\}' index.html | grep -v 'esc(' | head -20
```

CORS is tested by AC3 and AC23. If you add new endpoints, verify:
- The `Access-Control-Allow-Origin` header is set to the request's `Origin` only when it matches a localhost pattern
- Never set it to `*`

Shell injection is tested by AC21 and AC22. If you modify `refresh.sh`:
- Never use `shell=True` in embedded Python
- Never concatenate user-controlled values into shell commands — use arrays with `subprocess.run([...])` instead

---

## TDD Workflow

Follow this cycle for every change:

```
1. Write a failing test that describes the behaviour you want
2. Run it: verify it fails for the right reason (not an import error)
3. Write the minimal code to make it pass
4. Run the test again: verify it passes
5. Run the full suite: verify no regressions
6. Commit: test file + implementation file together
```

Never commit implementation without a test. Never write tests after the fact to wrap existing code.

---

## Commit Messages

Use conventional commits. Keep the subject under 72 characters.

```
feat: add CSV export for token usage table
fix: reconcileRows drops orphaned rows on tab switch
perf: skip SVG re-render when data hash unchanged
test: add AC29 for new export endpoint
docs: update CONTRIBUTING with CSV export testing guide
refactor: extract donut chart logic into renderDonut()
```

---

## Pull Request Checklist

Before opening a PR, verify:

- [ ] All tests pass: `.venv/bin/python3 -m pytest tests/ --ignore=tests/test_e2e.py -v`
- [ ] New behaviour has a test (AC number claimed, registered in `tests/README.md`)
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
