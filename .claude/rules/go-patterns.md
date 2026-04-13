---
globs:
  - "**/*.go"
---

# Go Patterns and Style Reference

Detailed conventions that complement `CLAUDE.md`. The go-rig skill owns process discipline (ATDD/TDD, DI, review workflow). This file owns language-specific patterns and API conventions.

## Style

- Package names: short, lowercase, no `util`/`common`/`helpers`
- Initialisms: `ID`, `URL`, `HTTP`, `JSON`, `API`, `SQL`
- No stutter: `orders.Service` not `orders.OrderService`
- Accept interfaces, return structs
- Constructors enforce invariants or own resources — not mandatory for every type
- Options struct when params > 3
- Prefer domain-specific named types and typed constants over `string`/`int` aliases
- Avoid `map[string]any` / `any` in core domain code except at loose boundaries
- Do not hardcode environment-specific values, collaborator selection, or operational limits

## API Design

- Make the zero value useful when practical
- Prefer nil slices by default; do not distinguish nil from empty unless the API requires it
- Pointer receivers when mutating or copying is non-trivial; keep receiver choice consistent
- Avoid copying types with mutex-like fields
- Keep exported APIs stable; treat JSON shape and error behavior changes as compatibility work
- Explicit types for IDs, states, units — prevent invalid mixing at compile time

## Documentation

- Every exported name and package should have a doc comment
- Doc comments start with the declared name and describe caller-visible behavior
- Update docs when exported behavior, config, or wire format changes
- Comments explain intent, invariants, ownership — not restate the code
- TODOs must include a reason and ticket reference when one exists

## Testing Patterns

- Subtest names should be readable; failure messages make `got` vs `want` obvious
- Prefer `for b.Loop() { ... }` for new benchmarks (1.24+)
- `t.Context()` for test-lifetime context (no independent cancel needed)
- `t.ArtifactDir()` for test output files (1.26)
- `testing/synctest` for isolated concurrent code with virtual time (1.25+)
- Prefer `cmp.Equal`/`cmp.Diff` for complex structure comparisons
- Avoid brittle assertions on exact JSON formatting or error text unless that output is part of the contract
- Acceptance tests verify user-visible behavior; unit tests keep domain rules fast and precise
- Add fuzz tests for parsers and input-heavy packages when the project uses fuzzing

## Common Patterns

- **SQLite**: prefer `modernc.org/sqlite` (pure Go, no CGO). Parameterized queries only. Explicit transactions.
- **CLI**: match existing framework (cobra, kong, or plain). Check `cmd/` and `internal/` layout before adding commands.
- **HTTP / DB**: set client/server/query timeouts deliberately. Always close rows/bodies and check terminal errors (`rows.Err`, close failures).
- **Modules**: avoid new dependencies without justification. Keep `replace` directives intentional and short-lived.
- **Linting**: `.golangci.yml` v2 per project. Do not modify without request. Fix all lint findings.
- **Static analysis**: prefer repo gates (`make lint`, `make ci`) and include `go vet ./...`, `staticcheck`, and other configured analyzers
