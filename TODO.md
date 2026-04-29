# TODO

## ✅ Released

- **v2026.4.29**: cron sidecar merge (#25), `/api/system` cold-path deadline + degraded fallback (#26), `system.gatewayPort` inheritance fix, systemd `Environment=` + `restart` on reinstall, per-instance latest-version fetcher
- **v2026.4.13**: diagnostics + log visibility (#14), release hardening pass, structured logging cleanup
- Built-in service management (`install`/`uninstall`/`start`/`stop`/`restart`/`status`) via launchd (macOS) and systemd (Linux)
- Security hardening (XSS, CORS, O(N²), shell safety, file handles)
- Performance, dirty-checking & test suite (initial 44 ACs, rAF, scroll preserve, tab fix)
- AI chat integration (`/api/chat`, chat panel UI, `ai` config block, chat test suite)
- Python removal — Go-only codebase (server, data collection, system metrics)

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
- [x] DOM/SVG incremental updates — `reconcileRows()` keyed reconciliation (>100 rows threshold) + `patchSvg()` content-hash cache (AC25, AC26)

## 🐳 Deployment

- [x] **Dockerfile** — Go binary, non-root user, port 8080, volume mount, healthcheck
- [x] **Nix flake** — `flake.nix` with `devShell`, `packages.default`, `apps.default` via flake-utils

## 🧪 Tests

- [x] Go test suite — `go test -race` covering all endpoints and core logic
- [x] Playwright E2E tests — 16 tests covering tabs, charts, countdown, chat panel, theme menu

## 📦 Release Plan

1. ~~Architecture refactor (State/DataLayer/DirtyChecker/Renderer/Theme) with synchronized test updates.~~ ✅
2. Performance follow-ups (incremental DOM/SVG updates if benchmark thresholds justify it).
3. Deployment artifacts (Dockerfile + Nix flake).
4. ~~Python removal — Go-only codebase.~~ ✅

## 🔖 Notes

- Go test suite: `go test -race ./...`
- Architecture doc: `ARCHITECTURE.md`
