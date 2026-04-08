# Go Development Standards

Navigate to a project subdirectory before running commands. Check each project for its own CLAUDE.md.

## Environment

- Local machine currently has Go 1.26.1 on darwin/arm64
- Do not assume local toolchain, OS, or architecture matches CI or production
- Treat `go.mod`, `toolchain`, CI config, and project docs as the source of truth
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

After writing code:

```sh
go fix -diff ./...   # review suggested modernizations first
go test ./...
go test -race ./...  # when the package has tests and race support is practical
go mod tidy          # only when you changed imports or dependencies
```

`go fix` is useful, but not every suggestion is a mandatory rewrite. Review behavior-changing diffs before applying them.

## Go 1.26 Idioms

Write modern Go — never generate pre-1.24 patterns when the project's go.mod allows it.

### Language (1.26)

```go
age := new(yearsSince(born))     // *int from expression — useful for optional scalar fields

type Adder[A Adder[A]] interface { // self-referential generic constraints
    Add(A) A
}
```

`new(expr)` is available in Go 1.26. Use it when it improves clarity, typically for optional scalar pointer values. Do not force it where `&T{...}` or a plain local variable is clearer.

### Iterators (1.23+)

Use `iter.Seq`/`iter.Seq2` and range-over-func. Prefer stdlib iterator APIs:
- `slices.Collect`, `slices.Sorted`, `slices.SortedFunc`, `slices.Concat`
- `maps.Keys`, `maps.Values`, `maps.Collect`, `maps.Insert`
- `bytes.Lines`, `bytes.SplitSeq`, `strings.Lines`, `strings.SplitSeq`

### Struct Tags (1.24+)

- `omitzero` is useful for struct-typed fields and types with `IsZero()` (e.g., `time.Time`)
- `omitempty` remains correct for slices, maps, strings, and other empty-value cases
- Use both when the wire format should omit either empty or zero values: `json:",omitzero,omitempty"`
- Treat JSON tag changes as behavior changes and review them carefully
- Generic type aliases are fully supported

### go fix Modernizers (1.26)

`go fix` suggests modernizations. Review the diff before applying, especially when a rewrite can change behavior. Useful analyzers include:
- `rangeint` — 3-clause `for` → `for range`
- `minmax` — if/else clamp → `min`/`max`
- `slicessort` — `sort.Slice` → `slices.Sort` for basic ordered types
- `any` — `interface{}` → `any`
- `fmtappendf` — `[]byte(fmt.Sprintf(...))` → `fmt.Appendf`
- `testingcontext` — simple cancellable test context setup → `t.Context()`
- `omitzero` — suggests `omitzero` for struct fields where `omitempty` has no effect
- `mapsloop` — map update loops → `maps.Copy`/`maps.Insert`/`maps.Clone`/`maps.Collect`
- `newexpr` — wrappers returning `&x` or call sites → `new(expr)`
- `stringsseq` / `stditerators` — loops over eager APIs → iterator-based forms
- `waitgroup` — `wg.Add(1)`/`go`/`wg.Done()` → `wg.Go`
- `//go:fix inline` — source-level inliner for API migrations

## Layout

```
main.go                     # simple single-command repo
cmd/<binary>/main.go        # multi-command repo entrypoint
internal/<domain>/          # domain-organized, not layer-organized
pkg/                        # rare — only intentionally reusable public packages
testdata/                   # test fixtures and data
*_test.go                   # usually next to the package under test
tests/                      # optional acceptance/integration/end-to-end suites
```

Default to domain-oriented packages as house style. Existing project conventions win unless they are actively causing problems. Use `cmd/` when the repo has multiple commands or a clear need to separate command entrypoints from library code.

Avoid catch-all package names such as `helpers`, `util`, and `common`.

Keep package boundaries explicit:
- domain packages own business rules and invariants
- transport, persistence, and external service adapters stay at the edges
- avoid circular dependencies and "god" packages that know every subsystem
- prefer small cohesive packages over deeply layered package trees

## Style

- Formatting is mechanical: use `gofmt`/`go fmt` and do not hand-format
- Standard library first — justify every dependency
- Prefer `x/` packages before third-party packages when they fit
- Concrete types over premature abstractions
- Unexported by default — export minimally
- Package names: short, lowercase, no `util`/`common`/`helpers`
- No stutter: `orders.Service` not `orders.OrderService`
- Initialisms: `ID`, `URL`, `HTTP`, `JSON`, `API`, `SQL`
- Interfaces: consumer-side, 1–3 methods, no `I` prefix, no DI frameworks
- Accept interfaces, return structs
- Constructors are for enforcing invariants or owning resources, not mandatory for every type
- Options struct when params > 3
- Prefer domain-specific named types and typed constants over `string`/`int` aliases sprinkled through business logic
- Avoid `map[string]any` / `any` in core domain code except at loose boundaries such as decoding or generic helpers
- Keep functions small enough to read in one pass; split parsing, validation, orchestration, and persistence when they start mixing
- Use early returns to reduce nesting and keep success paths obvious
- Use whitespace to separate phases of a function; one block should usually represent one logical step
- Do not hardcode environment-specific values, collaborator selection, or operational limits in business logic; prefer config, parameters, or typed constants where the value is truly invariant

## Errors

```go
return fmt.Errorf("create order: %w", err)
```

Wrap with `%w`. Branch with `errors.Is`/`errors.As`. Lowercase, no trailing punctuation. Never swallow. Never panic for expected failures. At boundaries: context in, internals never out.

Prefer sentinel errors or typed errors only when callers need to branch. Do not match on error strings. Add enough operation context to debug failures without leaking secrets or private internals.

## Context

First parameter for I/O. Never in struct. Never nil. Cancellation + deadlines + request metadata only.

## Documentation

- Every exported name should have a doc comment
- Every package should have a package comment
- Doc comments should start with the declared name and describe caller-visible behavior
- Update docs when exported behavior, configuration, or wire format changes
- Comments should explain intent, invariants, ownership, or non-obvious tradeoffs — not restate the code
- Leave TODOs only when they include a reason and a ticket or decision record when one exists

## API Design

- Make the zero value useful when practical
- Prefer nil slices by default; do not distinguish nil from empty unless the API or wire format requires it
- Use pointer receivers when the method mutates state or copying is non-trivial
- Keep receiver choice consistent across a type's methods
- Avoid copying types that contain mutex-like fields
- Keep exported APIs stable; treat signature, JSON shape, and error behavior changes as compatibility work
- Prefer explicit types for IDs, states, units, and domain concepts when they prevent invalid mixing at compile time

## Concurrency

Only when it measurably helps. Every goroutine needs a shutdown path. Context cancellation for background work. No concurrent map writes without sync. Race detector in CI.

## Testing

- Table-driven with `t.Run`, helpers marked `t.Helper()`
- Subtest names should be readable and failure messages should include enough context to debug quickly
- Prefer `for b.Loop() { ... }` for new benchmarks (1.24+); keep existing `b.N` style unless you are already touching the benchmark
- Use `t.Context()` in tests when you only need the test-lifetime context, not an independently controlled cancel function
- `t.ArtifactDir()` for test output files (1.26)
- `testing/synctest` for isolated concurrent code with virtual time (1.25+); avoid real network, external processes, and other non-bubble interactions
- Fakes/stubs over mocks — test behavior not implementation
- Deterministic seams for time, randomness, I/O
- Add fuzz tests for parsers, decoders, and input-heavy packages when the project already uses fuzzing or the surface is risky
- Prefer `cmp.Equal` / `cmp.Diff` for complex structure comparisons in new tests
- Avoid brittle assertions on exact JSON formatting or human-readable error text unless that exact output is part of the contract
- New or changed behavior should ship with tests in the most appropriate layer; do not treat coverage percentage alone as proof
- Acceptance/integration tests should verify user-visible behavior; unit tests should keep domain rules fast and precise

## Common Patterns

- **SQLite**: prefer `modernc.org/sqlite` (pure Go, no CGO). Parameterized queries only. Explicit transactions.
- **CLI**: match existing project framework (cobra, kong, or plain). Check `cmd/` and `internal/` layout before adding commands.
- **Logging**: match existing project convention (slog, zerolog, or std log). Structured logging preferred. Never log secrets.
- **Config**: no hardcoded values. Load + validate at startup. Secrets from env or secret manager.
- **HTTP / DB**: set client/server/query timeouts deliberately. Always close rows/bodies and check terminal errors (`rows.Err`, close failures when relevant).
- **Modules**: avoid new dependencies without justification. Keep `replace` directives intentional and short-lived unless the repo documents them.
- **Security**: run `govulncheck ./...` when dependencies changed or before cutting a release when the repo uses it.
- **Linting**: `.golangci.yml` v2 per project. Do not modify without request. Fix all lint findings.
- **Static analysis**: prefer repo gates (`make lint`, `make ci`) and include `go vet ./...`, `staticcheck`, and other configured analyzers when the project uses them

## Review Rejects

Premature abstractions · swallowed errors · context misuse · goroutine leaks · unsafe shared state · unnecessary deps · behavior-changing `go fix` rewrites applied blindly · transport in domain · missing tests · secrets in code · pre-1.24 patterns when go.mod allows modern · magic values in business logic · giant mixed-responsibility functions · weakly typed domain models without need
