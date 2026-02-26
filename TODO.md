# TODO

## ✅ Released

- Security hardening (XSS, CORS, O(N²), shell safety, file handles)
- Performance, dirty-checking & test suite (initial 44 ACs, rAF, scroll preserve, tab fix)
- AI chat integration (`/api/chat`, chat panel UI, `ai` config block, chat test suite)

---

## ✅ Architecture Refactor

Clean module structure — single file, zero deps. 7 modules: State / DataLayer / DirtyChecker / Renderer / Theme / Chat / App.
See `ARCHITECTURE.md` for full spec.

- [x] App owns dirty flag computation via `DirtyChecker.diff(snap)` called from `App.renderNow()`
- [x] `window.OCUI` namespace for inline handlers — all globals eliminated
- [x] Immutable snapshot per render cycle — `Object.freeze(JSON.parse(JSON.stringify(...)))` + `commitPrev(snap)` inside rAF
- [x] Split bottom dirty flag into 4 granular: `models`, `skills`, `git`, `agentConfig`
- [x] Non-functional guarantees documented in ARCHITECTURE.md
- [x] Tests AC17–AC20, TC1–TC5, hierarchy tests updated atomically
- [x] Bug fix: `var(--blue)` → `var(--blue,#3b82f6)` fallback on all 4 lines
- [x] Bug fix: `'models'` → `'availableModels'` dirty key mismatch

## ⚡ Performance

- [x] Volatile timestamp fix — `stableSnapshot()` for sessions/crons/subagentRuns dirty-checks (excluding `lastRun`, `nextRun`, `timestamp`, `updatedAt`)
- [ ] DOM/SVG incremental updates — Option B keyed row reconciliation + Option C SVG attr updates (only if refresh < 10s or tables > 100 rows)

## 🐳 Deployment

- [ ] **Dockerfile** — containerized dashboard: Python slim image, copy `index.html` + `server.py` + `refresh.sh` + `themes.json`, expose port 8080, mount openclaw config as volume
- [ ] **Nix flake** — `flake.nix` with `devShell` (python3 + bash deps) and `packages.default` for reproducible installs on NixOS / nix-darwin

## 🧪 Tests

- [x] Update static tests AC17–AC20 after architecture refactor (done in refactor PR)
- [ ] Add Playwright E2E tests for tab switching, chart toggle, auto-refresh cycle (optional, needs `playwright` dep in venv)

## 📦 Release Plan

1. ~~Architecture refactor (State/DataLayer/DirtyChecker/Renderer/Theme) with synchronized test updates.~~ ✅
2. Performance follow-ups (incremental DOM/SVG updates if benchmark thresholds justify it).
3. Deployment artifacts (Dockerfile + Nix flake).

## 🔖 Notes

- 75 tracked tests collected (`test_frontend.py` + `test_data_schema.py` + `test_server.py` + `test_critical.py` + `test_hierarchy_recent.py` + `test_chat.py`)
- Architecture doc: `ARCHITECTURE.md`
- Test runner: `python3 -m pytest tests/ -v`
