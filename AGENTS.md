# OpenClaw Dashboard — Codex Instructions

This `AGENTS.md` is the native always-on Codex instruction file for this repository.

Use these native Codex files:
- `.codex/skills/go-rig/SKILL.md` for Go implementation and refactoring work
- `.codex/skills/go-review/SKILL.md` for Go-focused review work
- `.codex/skills/frontend-dashboard/SKILL.md` for embedded frontend/UI work
- `.codex/skills/project-ops/SKILL.md` for build, CI, release, and command-surface work

Do not treat repo-root `SKILLS.md` or `RULES.md` as native auto-loaded Codex files.
Do not treat `CLAUDE.md` or `.claude/` as the native Codex instruction mechanism.
Those files remain for compatibility and reference only.

## Default Behavior

- Use the `go-rig` skill for meaningful Go code changes.
- Use the `go-review` skill when the task is review-first.
- Use the `frontend-dashboard` skill when changing the embedded SPA or dashboard UI.
- Use the `project-ops` skill when changing Makefile, CI, release, or operational docs.
- Keep output aligned with the current repo architecture and toolchain.

## Go Development Standards

Zero-dependency Go HTTP server with embedded SPA frontend:
- **Zero third-party dependencies** (`go.mod` has no `require` block, no `go.sum`).
- `web/index.html` is embedded via `//go:embed` and rebuilds on frontend changes.
- Root package is a façade: internal logic in `internal/app<domain>/`, thin wrappers at repo root.

### Project Constraints
- Do not add external dependencies unless explicitly justified and necessary.
- Treat `go.mod`, toolchain declarations, and CI as the source of truth.
- Prefer `make` targets when available.
- Prefer repo-defined checks over invented commands.

### Internal Packages
- `appconfig` — config loading, env, dotenv
- `appruntime` — path resolution, version detection
- `appchat` — AI gateway HTTP communication
- `apprefresh` — collection jobs and aggregations
- `appserver` — HTTP routing, caching, rate limiting
- `appsystem` — OS/system metrics and probes
- `appservice` — OS service management

### Style Baseline
- Go fmt/gofmt only (mechanical formatting).
- Stdlib first; `x/` before third-party; justify every dependency.
- Concrete types over abstractions; `unexported` by default.
- Short interfaces; consumer-side only; accept interfaces, return structs.
- Early returns and one-pass readable functions.
- No stale comments: explain why, not how.

### Errors and Context
- Wrap errors with `%w` and check with `errors.Is/As`.
- Do not swallow errors.
- Never panic on expected failures.
- `context.Context` is first parameter for I/O and request-scoped functions.

### Concurrency
- Use only when measurable.
- Every goroutine must have a shutdown path.
- No unsafe shared writes; protect maps when mutated concurrently.

### Testing
- Use table-driven tests with `t.Run`.
- Add acceptance-like coverage for user-visible behavior and meaningful unit tests for invariants.
- Fakes/stubs over mocks when possible.
- Deterministic seams for time, randomness, I/O.

### Layout and Design
- Domain-oriented packages under `internal/` and `pkg/` only when intentionally reusable.
- Root wrappers must be zero-logic forwarding for compatibility.
- Avoid speculative abstractions and generic utility folders (`helpers`, `util`, `common`).

### Security and Ops
- Prefer `slog` for structured logging.
- Validate config at startup; keep secrets out of logs and code.
- Set timeouts in HTTP/db/client usage and close all resources.

### Commands (Common)
- `make build`
- `make test`
- `make lint`
- `make check`
- After edits, run `go vet ./...`, tests, race tests where supported, then `go mod tidy` only when imports changed.

### Review Rejects
- Swallowed errors
- Context misuse
- Goroutine leaks
- Unsafe shared state
- Blind `go fix` changes
- Transport logic inside domain packages
- Behavior-changing refactors without matching tests
- Hardcoded magic values
- Unnecessary dependency creep

### Go Version and Modernization
- Go toolchain follows project `go.mod`/`toolchain` (1.26.x in CI context).
- Prefer modern Go features when they improve clarity and match the current toolchain.
- Do not apply `go fix` rewrites blindly; review behavior changes before accepting them.

## Testing Guidance

- Use stdlib testing features that are available in the project toolchain (`t.Context`, `b.Loop`, `t.ArtifactDir`, `testing/synctest`) when they improve test clarity.
- For complex comparisons in this zero-dependency repo, prefer stdlib approaches or small purpose-built helpers. Do not assume `github.com/google/go-cmp/cmp`.

## JSON and API Compatibility

- `json:",omitzero"` and `json:",omitempty"` are available in the current toolchain, but tag changes are wire-format changes and must be reviewed as compatibility work.
- Nil vs empty slice/map behavior must be intentional in JSON responses and persisted data.

## Architecture Summary

- The repo is a zero-dependency Go HTTP server with an embedded SPA.
- Core logic belongs in `internal/app<domain>/`.
- The root package is a facade and must stay zero-logic.
- Frontend changes require rebuild because `web/index.html` is embedded.
