---
name: go-rigor
description: Use this skill when building, reviewing, or refactoring Go code that must follow strict design discipline — ATDD/TDD workflow, explicit dependency injection, clean package design, and structured code review. Complements CLAUDE.md Go 1.26 standards with process rigor.
metadata:
  short-description: Go 1.26 design & test discipline
---

# Go Rigor

Strict design and testing discipline for Go 1.26 projects.

This skill **complements** `CLAUDE.md`, which defines Go 1.26 idioms, style, error handling, concurrency, and tooling. This skill adds:
- ATDD/TDD workflow
- design principles (SRP, DRY, OCP)
- dependency injection discipline
- comment quality standards
- structured review process

If `CLAUDE.md` is stricter on any point, follow `CLAUDE.md`.

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

Use Go 1.24+ testing APIs — see `CLAUDE.md` Testing section for `b.Loop()`, `t.Context()`, `t.ArtifactDir()`, `testing/synctest`.

## Design Principles

Apply without ceremony — these guide decisions, not generate boilerplate.

**SRP** — each package, type, and function has one clear reason to change. Split when a change in one concern forces changes in an unrelated concern.

**DRY** — extract repeated validation, mapping, branching, and business rules. Do not DRY away incidental similarity — two things that look alike but change for different reasons should stay separate.

**OCP** — extend behavior through new types and wiring, not by destabilizing stable contracts. Add new implementations behind existing interfaces rather than modifying working code.

## Dependency Injection

- **Constructors** for long-lived collaborators (stores, clients, loggers)
- **Function parameters** for short-lived collaborators and pure logic
- Never construct DB clients, HTTP clients, loggers, or repositories inside domain methods
- No DI frameworks — explicit wiring only
- No hidden globals or singletons

```go
// constructor injection for long-lived deps
func NewOrderService(store OrderStore, clock Clock) *OrderService {
    return &OrderService{store: store, clock: clock}
}

// function parameter for short-lived/pure logic
func ValidateOrder(order Order, now time.Time) error {
    if order.ExpiresAt.Before(now) {
        return fmt.Errorf("order %s expired: %w", order.ID, ErrExpired)
    }
    return nil
}
```

## Package Design

Organize by domain, not by technical layer.

- Group related domain logic together until splitting clearly improves cohesion
- Keep transport and storage close to the domain when the package is small
- One file, one concern — split files when they serve distinct purposes
- Split packages only when coupling pressure is real, not speculative

Avoid:
- interface-per-struct without a consumer need
- deep layering in small services
- `internal/platform/` catch-all layers — keep cross-cutting concerns in focused packages (`internal/config/`, `internal/db/`)

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

## Review Checklist

Before finishing any change, verify:

- [ ] Package boundaries are coherent — no cross-domain leaks
- [ ] No premature abstractions — interfaces have real consumers
- [ ] Dependencies injected explicitly — no hidden construction
- [ ] No hardcoded runtime values (URLs, ports, credentials, timeouts)
- [ ] Functions are readable in one pass
- [ ] Errors wrapped with useful context (`%w`)
- [ ] Tests cover acceptance behavior and unit behavior
- [ ] Modern Go idioms used (check `CLAUDE.md` Go 1.26 section)
- [ ] `go fix ./...` run to auto-modernize
- [ ] Lint clean — `make lint` passes

## Reject These Patterns

- Interface-per-struct without consumer need
- Giant functions mixing validation, orchestration, and persistence
- Hardcoded configuration or collaborator selection
- Comments that restate code
- Brittle mock-only tests — prefer fakes with real behavior
- Transport concerns embedded in core domain logic
- Production design distorted to satisfy a mocking framework
- Pre-1.24 patterns when go.mod allows modern (`interface{}`, `for i := 0; i < b.N; i++`, manual `context.WithCancel` in tests)

