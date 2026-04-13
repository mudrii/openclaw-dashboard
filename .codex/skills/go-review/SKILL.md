---
name: go-review
description: Use this skill when the task is to review Go code in this repository. Focus on bugs, regressions, API compatibility, test gaps, concurrency risks, and violations of the zero-dependency and root-facade constraints.
---

# Go Review

This is the native Codex review skill for Go work in this repository.

Use it when:
- the user asks for a review
- the task is to validate a Go change
- the task is to assess risks before or after a refactor

## Review Priorities

Report findings first.
Prioritize:
- correctness bugs
- behavioral regressions
- API and JSON compatibility changes
- missing or weak tests
- concurrency and shutdown risks
- dependency policy violations
- facade leakage from root package into domain logic

## Repository-Specific Checks

- Root package wrappers must stay zero-logic.
- New behavior belongs in `internal/app<domain>/`.
- Do not assume third-party helpers are acceptable in this zero-dependency repo.
- Treat `json:",omitempty"` and `json:",omitzero"` changes as API behavior changes.
- Check that goroutines have shutdown and ownership semantics.

## Review Output

- Lead with concrete findings.
- Use file references.
- Keep summaries brief.
- If there are no findings, say that explicitly and mention residual risk or testing gaps.
