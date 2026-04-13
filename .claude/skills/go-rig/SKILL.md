---
name: go-rig
description: Use this skill when building, reviewing, or refactoring Go code that must follow strict design discipline — ATDD/TDD workflow, explicit dependency injection, package-boundary discipline, and structured code review. Complements CLAUDE.md by focusing on process and design judgment rather than version-specific Go features.
metadata:
  short-description: Go design, workflow, and review discipline
  slash-command: enabled
---

# Go Rig

Strict design and testing discipline for Go projects.

This skill **complements** `CLAUDE.md`.

`CLAUDE.md` owns:
- Go version, toolchain, and commands
- Key style, error, context, and concurrency rules

`.claude/rules/` owns:
- Go 1.26 idioms and go fix modernizer catalog (`go-idioms.md`)
- Detailed style, API, documentation, and testing patterns (`go-patterns.md`)

This skill adds:
- ATDD/TDD workflow
- design principles and abstraction discipline
- dependency injection discipline
- package-boundary judgment
- documentation discipline
- comment quality standards
- structured review process

Do not restate or override version-specific guidance from `CLAUDE.md`. If `CLAUDE.md` is stricter on a shared point, follow `CLAUDE.md`.

## When to Use

Use this skill when:

- implementing a new feature or behavior increment
- refactoring Go code for clearer ownership or testability
- reviewing package boundaries or dependency flow
- replacing hidden collaborator construction with explicit injection
- tightening tests around user-visible or integration behavior

## ATDD/TDD Workflow

Test-first is a design tool, not an afterthought.

1. **Acceptance first** — define the boundary behavior before writing code
2. **Acceptance test** — add or update an acceptance-level test if the project has that layer; otherwise express boundary behavior in the closest consumer-level test
3. **Smallest failing unit test** — for the next behavior increment
4. **Minimal implementation** — only enough to pass
5. **Refactor** — improve readability and cohesion while green
6. **Repeat** — next behavior increment

If repository policy does not allow automatic test execution, still design test-first and ask before running.

### Test Coverage Expectations

Every meaningful change should cover:
- expected behavior (happy path)
- invalid input and validation failures
- edge cases and boundary values
- error and failure paths
- concurrency behavior when relevant

Use the project’s existing test layers where possible. Reach for acceptance tests when the change is user-visible or integration-heavy, and unit tests when isolating business rules or edge cases.

### Definition Of Done

A change is not done when the code "works on one path." It is done when:
- acceptance behavior is specified at the right boundary
- the smallest relevant unit behavior is covered
- failure and edge behavior are covered
- code was refactored back to clarity after going green
- repo test/lint/static-analysis expectations were satisfied or explicitly deferred

## Design Principles

Apply without ceremony — these guide decisions, not generate boilerplate.

**SRP** — each package, type, and function has one clear reason to change. Split when a change in one concern forces changes in an unrelated concern.

**DRY** — extract repeated validation, mapping, branching, and business rules. Do not DRY away incidental similarity — two things that look alike but change for different reasons should stay separate.

**OCP** — extend stable areas carefully, but do not invent indirection to satisfy the idea of extensibility. In Go, a concrete type with a small seam at the consumer is usually better than an abstract framework.

When applying SRP/DRY/OCP in Go, prefer deleting duplication caused by mixed responsibilities before introducing new abstractions. The first move is usually better boundaries, not more interfaces.

## Abstraction Discipline

- Start with concrete types and direct calls
- Introduce an interface only when a real consumer needs substitution
- Prefer one seam at a boundary over many tiny abstractions in the core
- If an abstraction adds files, wiring, and names but no clear testability or ownership win, do not add it

Avoid:
- repositories or services that only forward calls
- configuration objects passed everywhere to avoid choosing explicit parameters
- “future-proofing” abstractions without a concrete second implementation or consumer

## Function Design

- A function should usually do one thing: validate, transform, orchestrate, persist, or render
- If a function mixes business rules with transport, storage, or logging details, split it
- Keep parameter lists explicit and intention-revealing; if many values travel together for one reason, introduce a small typed struct

Refactor when a function:
- needs comments to explain the control flow
- mixes unrelated reasons to change
- carries mutable state across many screens of code
- repeats branching or validation logic that belongs in a helper or type method

## Dependency Injection

- **Constructors** for types that must enforce invariants or own long-lived collaborators
- **Function parameters** for short-lived collaborators and pure logic
- Never construct DB clients, HTTP clients, loggers, or repositories inside domain methods
- Prefer passing dependencies from the composition root (`main`, wiring package, or test setup) instead of looking them up deep inside the call stack
- Inject seams for time, randomness, process execution, filesystem, and external I/O when behavior depends on them

## Package Design

Organize by domain, not by technical layer.

- Group related domain logic together until splitting clearly improves cohesion
- Keep transport and storage near the owning domain in the repo when the service is small, but do not let core business logic depend on transport details
- Split files when doing so improves readability; file count is not a goal by itself
- Split packages only when coupling pressure is real, not speculative

Avoid:
- deep layering in small services
- `internal/platform/` catch-all layers — keep cross-cutting concerns in focused packages
- packages that combine unrelated domains because they share a datastore or transport

## Hardcoding And Configuration

- Do not hardcode URLs, ports, credentials, file paths, timeouts, feature flags, environment names, or dependency selection in core logic
- Domain invariants may be constants, but operational values should come from config, constructor parameters, or function arguments
- Prefer typed config structs validated at startup over scattered `os.Getenv` calls
- Keep configuration loading at the edge; pass validated values inward

## Type Discipline

- Model domain concepts with named types when that prevents invalid mixing and clarifies intent
- Prefer concrete structs over `map[string]any` for stable data
- Keep weakly typed data at the boundary and translate it into strict internal types quickly
- Avoid boolean parameter soup; use named option structs or dedicated methods when intent is unclear

## Comment Quality

**Write comments when they add**:
- why a tradeoff exists
- package-level intent
- non-obvious invariants or constraints
- concurrency ownership rules
- boundary assumptions

**Do not write comments that**:
- restate the code
- narrate obvious assignments
- explain syntax instead of intent
- leave vague TODOs without reason or ticket reference
- duplicate the doc comment with less precision

## Documentation Discipline

- Exported names and packages need doc comments
- Public docs should describe contract, invariants, and caller-visible behavior
- When a change affects configuration, wire format, or API semantics, update docs in the same change
- Add or update examples when they materially improve discoverability of a public API

## Test Quality

- Prefer readable subtest names over encoded case IDs
- Failure messages should make `got` and `want` obvious
- Prefer semantic comparisons over formatting-sensitive comparisons
- Avoid asserting on exact human-readable error strings unless the exact string is part of the contract
- Use `t.Fatal` only when the test cannot continue meaningfully
- Acceptance tests should speak in business behavior, not internal implementation vocabulary
- Use table-driven tests where variation is the point; do not force tables when a direct narrative test is clearer
- Add test seams instead of using sleeps, global mutation, or network reliance to force determinism

## Static Analysis Discipline

Treat linting and static analysis as design feedback, not cosmetic cleanup.

- Respect repo gates for `go vet`, `golangci-lint`, `staticcheck`, `govulncheck`, and related analyzers when configured
- Fix root causes instead of scattering ignores
- If an analyzer warning is intentionally ignored, leave a precise justification close to the suppression
- Do not weaken lint configuration casually to make a change pass

## Review Checklist

Before finishing any change, verify:

- [ ] Package boundaries are coherent — no cross-domain leaks
- [ ] Dependencies injected explicitly — no hidden construction
- [ ] Functions are readable in one pass and do not mix unrelated responsibilities
- [ ] Tests cover acceptance behavior and unit behavior
- [ ] TDD/ATDD flow was followed as closely as the repo constraints allowed
- [ ] Behavioral compatibility checked where public APIs, JSON, or persistence shape changed
- [ ] Nil vs empty behavior is intentional for slices, maps, pointers, and JSON fields
- [ ] Concurrency changes have a shutdown path and observable ownership
- [ ] Root-package wrappers added/updated for any new `internal/` exports
- [ ] Lint and test gates (`make check`) have been run or consciously deferred

## Project-Specific: Root-Package Facade

This project uses a facade pattern where the root `dashboard` package re-exports `internal/` APIs via thin wrappers and type aliases.

When adding a new feature:
1. Implement logic in `internal/app<domain>/` with exported names
2. Add type aliases (`type X = appfoo.X`) and wrapper functions in the root package
3. Write tests at both the internal package level (unit) and root level (integration)

When modifying an existing internal API:
- If the signature changes, update the corresponding root-level wrapper
- Root-level wrappers must remain zero-logic forwarding — no business logic in the facade

Legacy note: `CollectTokenUsage` and `CollectTokenUsageWithCache` have 15+ map parameters. New code should use options structs per CLAUDE.md convention. These functions are candidates for future refactoring but are not blocking.

## Success Criteria

This skill is being followed correctly when:

- changes are small, test-backed, and easy to review
- dependency flow is explicit from the composition root
- package responsibilities are cleaner after the change, not blurrier
- the implementation follows the Go standards in `CLAUDE.md`
- tests speak in behavior terms, not implementation vocabulary
- the resulting code reads clearly without comments explaining the control flow
