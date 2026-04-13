---
name: project-ops
description: Use this skill when working on repository operations in this project, including build, test, lint, release, CI alignment, Makefile-driven checks, and operational packaging constraints.
---

# Project Ops

This is the native Codex operations skill for this repository.

Use it when:
- changing build, test, lint, or check workflows
- editing `Makefile` or CI workflows
- reviewing release and packaging behavior
- validating commands agents should run in this repo

## Source Of Truth

- `go.mod` defines the Go toolchain level.
- `Makefile` defines the preferred local command surface.
- `.github/workflows/` defines CI expectations.
- If these disagree with prose docs, follow code and workflow config first.

## Repository Expectations

- Prefer `make build`, `make test`, `make lint`, `make check`.
- Keep local and CI command surfaces aligned.
- Do not invent commands that the repo does not expose.
- Preserve the zero-dependency bias unless a change explicitly requires otherwise.
- Treat release workflow changes as operational changes that need careful compatibility review.

## Change Discipline

- Keep command names stable when possible.
- If command behavior changes, update docs in the same change.
- Prefer small operational changes with obvious rollback paths.
- Do not weaken checks casually to make a change pass.

## Review Focus

- command drift between docs, Makefile, and CI
- hidden dependency additions
- missing vet/lint/test coverage in local or CI paths
- release workflow mismatches
