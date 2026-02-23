# TODO

## ✅ Released

- Security hardening (XSS, CORS, O(N²), shell safety, file handles)
- Performance, dirty-checking & test suite (initial 44 ACs, rAF, scroll preserve, tab fix)

---

## 🏗️ Architecture Refactor

Clean module structure — single file, zero deps. Opus designed, Codex reviewed.
See `ARCHITECTURE.md` for full spec.

Before implementing, apply these design tweaks (from Codex review):

- [ ] App owns `computeDirtyFlags()` — not Renderer (fix flow contract contradiction in doc)
- [ ] Introduce `window.OCUI` namespace for inline handlers and migrate current direct global handler calls
- [ ] Immutable snapshot per render cycle — `const snap = State.snapshot()` passed to both DirtyChecker and Renderer
- [ ] Split `bottom` dirty flag into 4 granular flags: `models`, `skills`, `git`, `agentConfig`
- [ ] Document non-functional guarantees in ARCHITECTURE.md: scroll preservation, rAF batching, error handling, out-of-order fetch protection
- [ ] Update ATDD tests AC17–AC20 in the same PR as architecture renames (`prevD` → `State.prev`, `loadData` → `App.refresh`, etc.)

## ⚡ Performance

- [x] Volatile timestamp fix — `stableSnapshot()` for sessions/crons/subagentRuns dirty-checks (excluding `lastRun`, `nextRun`, `timestamp`, `updatedAt`)
- [ ] DOM/SVG incremental updates — Option B keyed row reconciliation + Option C SVG attr updates (only if refresh < 10s or tables > 100 rows)

## 🐳 Deployment

- [ ] **Dockerfile** — containerized dashboard: Python slim image, copy `index.html` + `server.py` + `refresh.sh` + `themes.json`, expose port 8080, mount openclaw config as volume
- [ ] **Nix flake** — `flake.nix` with `devShell` (python3 + bash deps) and `packages.default` for reproducible installs on NixOS / nix-darwin

## 🧪 Tests

- [ ] Update static tests AC17–AC20 after architecture refactor (regex patterns reference old global names)
- [ ] Add Playwright E2E tests for tab switching, chart toggle, auto-refresh cycle (optional, needs `playwright` dep in venv)

## 📦 Release Plan

1. Architecture refactor (State/DataLayer/DirtyChecker/Renderer/Theme) with synchronized test updates.
2. Performance follow-ups (incremental DOM/SVG updates if benchmark thresholds justify it).
3. Deployment artifacts (Dockerfile + Nix flake).

## 🔖 Notes

- 46 tracked tests collected (`test_frontend.py` + `test_data_schema.py` + `test_server.py` + `test_critical.py`)
- Architecture doc: `ARCHITECTURE.md`
- Test runner: `python3 -m pytest tests/ -v`
