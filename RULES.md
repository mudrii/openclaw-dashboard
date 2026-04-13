# Codex RULES

This file is a short Go reference for this repository.
It is not a native auto-loaded Codex filename.

`AGENTS.md` owns always-on policy.
`.codex/skills/` owns workflow.
This file only captures concise language and API reminders.

## Modern Go

- Use current-toolchain Go features when they improve clarity.
- `new(expr)`, iterators, `strings.Lines`, `bytes.Lines`, `SplitSeq`, `t.Context`, `b.Loop`, `t.ArtifactDir`, `testing/synctest`, and `WaitGroup.Go` are available in this repo's Go level.
- Prefer allocation-saving and clarity-improving forms, not novelty for its own sake.

## JSON And API

- `omitempty` and `omitzero` are both available.
- Tag changes are wire-format changes and must be reviewed as compatibility work.
- Nil vs empty slice/map behavior must be intentional.

## API Design

- Keep zero value useful when practical.
- Prefer nil slices by default unless empty semantics are required.
- Use pointer receivers only when mutation/copy cost or invariants require it.
- Avoid copying mutex-containing types.
- Keep exported APIs stable and document behavior changes.
- Prefer explicit domain types (`UserID`, `SessionState`) over raw `string`/`int`.

## Naming and Packages

- Package names: short, lowercase; avoid `util`, `common`, `helpers`.
- Initialisms: `ID`, `URL`, `HTTP`, `JSON`, `API`, `SQL`.
- No stutter in names (`orders.Service`, not `orders.OrderService`).
- Prefer domain-specific names and typed constants.

## Comments and Documentation

- Every exported name/package has a doc comment.
- Export docs should describe caller behavior, invariants, and assumptions.
- Add docs when config, wire format, or API semantics change.

## Testing Rules

- Subtest names should be readable.
- Failure messages should make `got` and `want` obvious.
- Prefer stdlib helpers or small purpose-built test helpers that fit the repo's zero-dependency policy.
- Use `t.Helper`, `t.Context` (where available), and `for b.Loop()` for new benchmarks.
- Avoid brittle assertions on exact text unless contractually fixed.
- Use table-driven tests for variation-heavy behavior.

## HTTP and DB / I/O

- Set explicit timeouts for network/db operations.
- Always close bodies/rows and check close/terminal errors.
- Parameterized queries only for SQL and explicit transactions where needed.

## Concurrency and Safety

- Shutdown channels and context cancellation for background goroutines.
- No concurrent map writes without synchronization.
- Make concurrency behavior observable and testable without sleeps.

## Lint and Static Analysis

- Treat lint/staticcheck feedback as design feedback.
- Do not scatter ignores; fix root causes.
- Do not weaken lint rules as a default workaround.
