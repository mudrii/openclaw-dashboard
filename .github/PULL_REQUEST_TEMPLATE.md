# Pull Request

## Type

<!-- Tick exactly one. -->

- [ ] `feat` — new feature or panel
- [ ] `fix` — bug fix
- [ ] `perf` — performance improvement (no behaviour change)
- [ ] `test` — tests only (no production code change)
- [ ] `docs` — documentation only
- [ ] `refactor` — internal restructure (no behaviour change)
- [ ] `chore` — tooling, CI, config

## Summary

<!-- One paragraph: what this PR does and why.
     Agents use this as the primary context for review.
     Include the issue number if applicable: "Closes #N" -->

Closes #

## What Changed

<!-- List the files touched and what changed in each.
     Be specific enough that a reviewer can verify without reading every line.
     Example:
     - `index.html` — added `renderExport()` to Renderer, wired to new `flags.export` dirty flag
     - `server.py` — added GET /api/export endpoint, returns CSV with Content-Disposition header
     - `tests/test_server.py` — AC29: export endpoint returns 200 with text/csv content-type -->

| File | What changed |
|------|-------------|
| | |
| | |

## New Acceptance Criteria

<!-- List AC numbers added in this PR, or "none" if test-only or docs-only.
     Every new behaviour must have an AC registered in tests/README.md. -->

| AC | Test file | Description |
|----|-----------|-------------|
| AC | | |

## Test Evidence

<!-- Paste the output of running the test suite.
     Required for any PR that touches production code. -->

```
.venv/bin/python3 -m pytest tests/ --ignore=tests/test_e2e.py -v
```

<details>
<summary>Test output</summary>

```
paste here
```

</details>

## Checklist

<!-- All boxes must be ticked before requesting review.
     Agents use this list to verify completeness before merging. -->

### Code quality
- [ ] No new globals outside the 7 module objects + 4 utilities (`$`, `esc`, `safeColor`, `relTime`)
- [ ] Every dynamic value inserted into the DOM goes through `esc()`
- [ ] No `shell=True` in any Python code
- [ ] No hardcoded hex colors — CSS variables only (`var(--accent)`, etc.)
- [ ] No new frontend dependencies (no `import`, no CDN `<script>`)
- [ ] No new backend pip dependencies

### Tests
- [ ] All existing tests pass: `.venv/bin/python3 -m pytest tests/ --ignore=tests/test_e2e.py -v`
- [ ] New behaviour has at least one test
- [ ] New AC number(s) registered in `tests/README.md`

### Manual verification
- [ ] Tested in at least one dark theme and one light theme
- [ ] Tested on desktop and mobile viewport (< 768px)
- [ ] If chart code changed: verified both 7d and 30d views
- [ ] If session/cron table changed: verified scroll position preserved after refresh

### Documentation
- [ ] `CHANGELOG.md` updated under the correct version heading
- [ ] `README.md` updated if a new panel or config key was added
- [ ] `tests/README.md` updated with new ACs

## Screenshots / Recordings

<!-- For any visual change, include before/after screenshots.
     Omit for backend-only or test-only PRs. -->

**Before:**

**After:**

## Breaking Changes

<!-- Does this PR change any existing behaviour that callers depend on?
     - API response shape changes (new/removed keys in /api/refresh or /api/chat)
     - config.json key renames or removals
     - data.json schema changes that break existing refresh.sh output
     Answer "None" if not applicable. -->

## Agent Review Notes

<!-- Optional. Anything specific you want the reviewing agent to focus on,
     verify, or question. Leave blank if standard review is sufficient.
     Example: "Please verify the reconcileRows key function handles null session.key gracefully" -->
