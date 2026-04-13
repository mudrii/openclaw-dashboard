# Codex SKILLS

This file is a reference summary for humans.
It is not a native auto-loaded Codex skill file.

Native Codex skill location for this repo:
- `.codex/skills/go-rig/SKILL.md`
- `.codex/skills/go-review/SKILL.md`
- `.codex/skills/frontend-dashboard/SKILL.md`
- `.codex/skills/project-ops/SKILL.md`

## go-rig

Use this skill for Go work in this repository whenever one or more are true:
- adding or refactoring behavior
- touching package boundaries
- introducing dependency injections
- improving test strategy and review discipline

## Scope

This skill summary complements `AGENTS.md`:
- `AGENTS.md` owns version/tooling and concrete command policy.
- `RULES.md` owns Go idioms and style patterns.
- `go-rig` owns process, design, and review discipline.

## Process (ATDD/TDD)

1. Define boundary behavior first (acceptance-level or closest consumer tests).
2. Add the smallest failing behavior test.
3. Implement minimal code to pass.
4. Refactor for clarity and rerun.
5. Repeat for next increment.

### Testing expectations

Each meaningful change should cover:
- happy path
- invalid input and validation failure
- edge/boundary cases
- error/failure paths
- concurrency behavior when relevant

## Design Discipline

- SRP: one clear reason to change per function/type/package.
- DRY: remove duplicated logic/branching, not forced uniqueness.
- OCP: extend with purpose, avoid abstraction noise.
- Concrete types first; add interfaces only when a consumer truly needs substitution.
- Keep packages domain-focused, not layer-driven.
- Inject dependencies at boundaries; do not construct collaborators deep in domain code.
- No hidden seam creation for convenience.

## API and Type Discipline

- Keep exported APIs stable; avoid breaking JSON/API shape without migration work.
- Prefer named/typed domain values over primitive-typed `string`/`int` flags where possible.
- Use options structs for multi-parameter APIs.
- Keep weakly-typed data at edges (`any`/maps), translate quickly to strict structs.

## Dependency Injection

- Constructors for types enforcing invariants or managing resources.
- Function parameters for transient collaborators/pure logic.
- Inject time, randomness, filesystem, and process execution when behavior depends on them.

## Package Strategy

- Domain packages by ownership and cohesion.
- Avoid deep technical-layering for this small codebase.
- Split packages only when coupling pressure is real.

## Comments and Docs

Write comments only for intent, invariants, constraints, and tradeoffs.
Do not narrate obvious code.

## Review Checklist (before finalizing)

- [ ] Boundaries are coherent (domain ownership preserved)
- [ ] Dependencies injected explicitly (no hidden construction)
- [ ] Functions are easy to read in one pass
- [ ] Unit and behavior tests added/updated
- [ ] TDD/ATDD flow followed
- [ ] Facade wrappers kept as pass-through forwarding
- [ ] Gate checks run or deferred explicitly

## Native Source

Codex-native source: `.codex/skills/go-rig/SKILL.md`

## go-review

Use this skill for review-only Go work in this repository:
- code review
- regression and risk assessment
- API and JSON compatibility review
- test-gap analysis

Codex-native source: `.codex/skills/go-review/SKILL.md`

## frontend-dashboard

Use this skill for embedded SPA work in this repository:
- editing `web/index.html`
- dashboard layout and styling changes
- frontend behavior tied to server-rendered or embedded assets

Codex-native source: `.codex/skills/frontend-dashboard/SKILL.md`

## project-ops

Use this skill for repository operations work in this repository:
- Makefile and CI changes
- build, test, lint, and release workflow work
- command-surface and operational documentation alignment

Codex-native source: `.codex/skills/project-ops/SKILL.md`

## Legacy Source

Original Claude source remains in `.claude/skills/go-rig/SKILL.md` for reference.
