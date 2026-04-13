---
name: go-rig
description: Use this skill when building, reviewing, or refactoring Go code in this repository. It adds process discipline for TDD/ATDD, package boundaries, dependency injection, and review quality, and complements the always-on rules in AGENTS.md.
---

# Go Rig

This is the native Codex Go workflow skill for this repository.

`AGENTS.md` owns:
- always-on project constraints
- Go version and toolchain expectations
- compatibility, dependency, testing, and command policy

This skill owns:
- TDD/ATDD workflow
- design and abstraction discipline
- dependency injection discipline
- package-boundary judgment
- review checklist for Go changes

## When To Use

Use this skill when:
- implementing a new behavior
- refactoring Go code
- changing package boundaries
- changing exported APIs or JSON behavior
- adding test seams, DI, or acceptance coverage

## Workflow

1. Define the boundary behavior first.
2. Add the smallest failing test at the closest useful layer.
3. Implement the minimum code to pass.
4. Refactor back to clarity.
5. Repeat in small increments.

If tests are not being run in the current turn, still design the change test-first.

## Testing Expectations

Each meaningful change should consider:
- happy path
- invalid input and validation failure
- edge and boundary cases
- error paths
- concurrency behavior when relevant

Use acceptance-style tests for user-visible behavior and unit tests for domain rules.

## Design Discipline

- Start with concrete types.
- Add interfaces only when a real consumer needs substitution.
- Keep packages domain-focused.
- Avoid speculative abstractions.
- Inject collaborators at boundaries instead of constructing them deep in domain code.
- Prefer option structs over long positional parameter lists.

## Package And API Discipline

- Keep exported APIs stable unless compatibility work is intentional.
- Keep root facade wrappers as zero-logic forwarding.
- Translate weakly typed edge data into strict domain types quickly.
- Avoid transport or persistence concerns leaking into core domain behavior.

## Review Checklist

- Boundaries are coherent.
- Dependencies are explicit.
- Functions are readable in one pass.
- Tests cover behavior, not just implementation details.
- JSON and API compatibility changes were reviewed intentionally.
- Nil vs empty behavior is intentional.
- Concurrency changes have shutdown and ownership semantics.

## Repository Notes

- The repository uses a root facade package over `internal/app<domain>/`.
- New functionality belongs in `internal/` first, with root wrappers added only for compatibility.
- This repo has a zero-dependency bias; do not assume external helper libraries for tests or comparisons.
