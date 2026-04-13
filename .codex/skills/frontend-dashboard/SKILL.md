---
name: frontend-dashboard
description: Use this skill when editing the embedded dashboard frontend in this repository. It focuses on preserving the single-file embedded SPA model, keeping the UI lightweight, and avoiding unnecessary frontend tooling or dependencies.
---

# Frontend Dashboard

This is the native Codex frontend skill for the embedded dashboard UI in this repository.

Use it when:
- editing `web/index.html`
- changing dashboard layout, styling, or interaction behavior
- improving usability of the embedded SPA

## Repository Constraints

- The frontend is embedded into the Go binary.
- Keep the frontend simple and self-contained.
- Avoid introducing new frontend build systems, frameworks, or third-party packages unless explicitly requested.
- Preserve compatibility with the Go server contract and embedded asset flow.

## UI Direction

- Prefer clear information hierarchy over visual noise.
- Keep interactions fast and obvious.
- Improve readability of metrics, status, and operational data first.
- Use intentional spacing, typography, and contrast.
- Favor small, maintainable JS and CSS over abstraction-heavy patterns.

## Change Discipline

- Do not turn the SPA into a toolchain-driven frontend without explicit approval.
- Keep API assumptions explicit.
- Treat data-shape changes between backend and frontend as contract changes.
- When in doubt, optimize for operability and clarity instead of novelty.
