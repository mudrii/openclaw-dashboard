# Go Development Standards

Navigate to a project subdirectory before running commands. Check each project for its own CLAUDE.md.

## Environment

- Go 1.26.1 darwin/arm64 — Green Tea GC enabled by default
- Linting/formatting tools often pinned per-project in Makefile
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

After writing code, modernize with: `go fix ./...`

## Go 1.26 Idioms

Write modern Go — never generate pre-1.24 patterns when the project's go.mod allows it.

### Language (1.26)

```go
p := new(42)                     // *int initialized to 42 — not new(int) then assign
s := new("hello")                // *string initialized to "hello"
age := new(yearsSince(born))     // *int from expression — useful for optional JSON fields

type Adder[A Adder[A]] interface { // self-referential generic constraints
    Add(A) A
}
```

### Iterators (1.23+)

Use `iter.Seq`/`iter.Seq2` and range-over-func. Prefer stdlib iterator APIs:
- `slices.Collect`, `slices.Sorted`, `slices.SortedFunc`, `slices.Concat`
- `maps.Keys`, `maps.Values`, `maps.Collect`, `maps.Insert`
- `bytes.Lines`, `bytes.SplitSeq`, `strings.Lines`, `strings.SplitSeq`

### Struct Tags (1.24+)

- `omitzero` for struct-typed fields and types with `IsZero()` (e.g., `time.Time`)
- `omitempty` remains correct for slices and maps (omits empty, not just zero)
- Use both when needed: `json:",omitzero,omitempty"`
- Generic type aliases are fully supported

### go fix Modernizers (1.26)

`go fix ./...` auto-applies safe rewrites. Key modernizers:
- `rangeint` — 3-clause `for` → `for range`
- `minmax` — if/else clamp → `min`/`max`
- `sortslice` — `sort.Slice` → `slices.Sort`
- `any` — `interface{}` → `any`
- `fmtappendf` — `[]byte(fmt.Sprintf(...))` → `fmt.Appendf`
- `testingcontext` — `context.WithCancel` in tests → `t.Context`
- `omitzero` — `omitempty` → `omitzero` on struct-typed fields
- `mapsloop` — map update loops → `maps.Collect`/`maps.Insert`
- `//go:fix inline` — source-level inliner for API migrations

## Layout

```
cmd/<binary>/main.go       # entrypoint
internal/<domain>/          # domain-organized, not layer-organized
pkg/                        # rare — only intentionally reusable public packages
tests/                      # acceptance, integration, golden files
testdata/                   # test fixtures and data
```

Never create `controllers/`, `services/`, `models/`, `helpers/`, `util/`, `common/`.

## Style

- Standard library first — justify every dependency
- Concrete types over premature abstractions
- Unexported by default — export minimally
- Package names: short, lowercase, no `util`/`common`/`helpers`
- No stutter: `orders.Service` not `orders.OrderService`
- Initialisms: `ID`, `URL`, `HTTP`, `JSON`, `API`, `SQL`
- Interfaces: consumer-side, 1–3 methods, no `I` prefix, no DI frameworks
- Accept interfaces, return structs
- Options struct when params > 3

## Errors

```go
return fmt.Errorf("create order: %w", err)
```

Wrap with `%w`. Branch with `errors.Is`/`errors.As`. Lowercase, no trailing punctuation. Never swallow. Never panic for expected failures. At boundaries: context in, internals never out.

## Context

First parameter for I/O. Never in struct. Never nil. Cancellation + deadlines + request metadata only.

## Concurrency

Only when it measurably helps. Every goroutine needs a shutdown path. Context cancellation for background work. No concurrent map writes without sync. Race detector in CI.

## Testing

- Table-driven with `t.Run`, helpers marked `t.Helper()`
- `for b.Loop() { ... }` for benchmarks (1.24+), not `for i := 0; i < b.N; i++`
- `t.Context()` instead of `context.WithCancel` in tests (1.24+)
- `t.ArtifactDir()` for test output files (1.26)
- `testing/synctest` for concurrent code with virtual time (1.25+)
- Fakes/stubs over mocks — test behavior not implementation
- Deterministic seams for time, randomness, I/O

## Common Patterns

- **SQLite**: prefer `modernc.org/sqlite` (pure Go, no CGO). Parameterized queries only. Explicit transactions.
- **CLI**: match existing project framework (cobra, kong, or plain). Check `cmd/` and `internal/` layout before adding commands.
- **Logging**: match existing project convention (slog, zerolog, or std log). Structured logging preferred. Never log secrets.
- **Config**: no hardcoded values. Load + validate at startup. Secrets from env or secret manager.
- **Linting**: `.golangci.yml` v2 per project. Do not modify without request. Fix all lint findings.

## Review Rejects

Premature abstractions · swallowed errors · context misuse · goroutine leaks · unsafe shared state · unnecessary deps · transport in domain · missing tests · secrets in code · pre-1.24 patterns when go.mod allows modern
