# Go Development Standards

Zero-dependency Go HTTP server with embedded SPA frontend for OpenClaw bot metrics.

## Project Constraints

- **Zero third-party dependencies** — `go.mod` has no `require` block, no `go.sum`. Do not suggest adding external packages. Use stdlib only.
- **Embedded frontend** — `web/index.html` is compiled into the binary via `//go:embed`. Frontend changes require a rebuild.
- **Root-package facade** — Logic lives in `internal/app<domain>/` packages. The root `dashboard` package has thin wrapper functions and type aliases that re-export internal APIs. New features: (1) add logic to `internal/`, (2) add root-level wrappers, (3) test at both levels.

## Internal Packages

| Package | Purpose |
|---------|---------|
| `appconfig` | Config struct definitions, JSON loading, dotenv |
| `appruntime` | Directory resolution (repo root vs Homebrew), version detection |
| `appchat` | AI gateway HTTP communication, system prompt building |
| `apprefresh` | Core data collector: sessions, tokens, crons, daily aggregation |
| `appserver` | HTTP server, routing, caching, rate limiting |
| `appsystem` | System metrics (CPU/RAM/disk), version probes, OpenClaw runtime |
| `appservice` | OS service management (launchd/systemd) |

## Environment

- Go 1.26.1 on darwin/arm64
- Treat `go.mod`, `toolchain`, CI config as the source of truth
- Prefer `make` targets over raw commands when a Makefile exists

## Commands

Check each project's Makefile for exact targets. Common conventions:

```sh
make build        # build binary
make test         # go test ./...
make lint         # golangci-lint
make fmt          # gofmt / gofumpt
make ci           # full gate (or make check, make all)
```

After writing code:

```sh
go vet ./...         # always run first — catches misuse before lint
go fix ./...         # applies modernizations in-place; review with git diff
go test ./...
go test -race ./...  # when the package has race support
go mod tidy          # only when imports or dependencies changed
```

## Go 1.26 Idioms

Write modern Go — never generate pre-1.24 patterns when go.mod allows it. Key features: `new(expr)`, self-referential generics, `iter.Seq`/`iter.Seq2`, range-over-func, `omitzero` struct tags, generic type aliases. See `.claude/rules/go-idioms.md` for the full catalog and go fix modernizers.

## Layout

```
cmd/<binary>/main.go        # multi-command repo
internal/<domain>/          # domain-organized, not layer-organized
pkg/                        # rare — only intentionally reusable public packages
*_test.go                   # next to the package under test
tests/                      # optional acceptance/integration/e2e suites
```

Domain-oriented packages as default. Avoid `helpers`, `util`, `common`.

## Style

- Formatting is mechanical: `gofmt`/`go fmt` — do not hand-format
- Standard library first; `x/` before third-party; justify every dependency
- Concrete types over premature abstractions; unexport by default
- Interfaces: consumer-side, 1–3 methods, accept interfaces return structs
- Early returns; whitespace to separate phases; functions readable in one pass
- See `.claude/rules/go-patterns.md` for naming, API design, and documentation rules

## Errors

```go
return fmt.Errorf("create order: %w", err)
```

Wrap with `%w`. Branch with `errors.Is`/`errors.As`. Lowercase, no trailing punctuation. Never swallow. Never panic for expected failures. Context in, internals never out.

## Context

First parameter for I/O. Never in struct. Never nil. Cancellation + deadlines + request metadata only.

## Concurrency

Only when it measurably helps. Every goroutine needs a shutdown path. Context cancellation for background work. No concurrent map writes without sync. Race detector in CI.

## Testing

- Table-driven with `t.Run`, helpers with `t.Helper()`
- Fakes/stubs over mocks — test behavior not implementation
- Deterministic seams for time, randomness, I/O
- `cmp.Equal`/`cmp.Diff` for complex comparisons
- See `.claude/rules/go-patterns.md` for detailed testing APIs

## Common Patterns

- **Logging**: prefer `slog` (stdlib, structured) for new code; match existing convention in existing projects. Never log secrets.
- **Config**: no hardcoded values. Load + validate at startup. Secrets from env or secret manager.
- **HTTP / DB**: set timeouts deliberately. Always close rows/bodies.
- **Security**: `govulncheck ./...` when deps changed or before release; include in CI.

## Commits

Use conventional commit format: `type(scope): message`

Types: `feat`, `fix`, `refactor`, `test`, `perf`, `docs`, `chore`
Scopes: `refresh`, `server`, `system`, `chat`, `config`, `service`, `runtime`, `web`

## Review Rejects

Premature abstractions · swallowed errors · context misuse · goroutine leaks · unsafe shared state · unnecessary deps · behavior-changing go fix applied blindly · transport in domain · missing tests · secrets in code · pre-1.24 patterns when go.mod allows modern · magic values · giant mixed-responsibility functions
