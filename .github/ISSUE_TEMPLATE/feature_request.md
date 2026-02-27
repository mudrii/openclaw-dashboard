---
name: Feature Request
about: Propose a new panel, behaviour, or integration
labels: enhancement
---

## Summary

<!-- One sentence: what you want and why.
     Good: "Add a CSV export button to the token usage table so I can track costs in a spreadsheet"
     Bad:  "CSV export would be nice" -->

## Problem This Solves

<!-- Describe the current friction. What can you not do today?
     Agents use this to understand motivation and prioritise.
     Be specific: "I have to manually copy values from the token table every week" is better
     than "it would be convenient". -->

## Proposed Solution

<!-- Describe the behaviour you want. Include:
     - Where in the UI it appears (which panel, which button)
     - What triggers it (click, auto, config option)
     - What the output looks like (format, structure, destination) -->

## Affected Component

<!-- Where does this change live? Tick all that apply. -->

- [ ] `server.py` — new endpoint or server behaviour
- [ ] `refresh.sh` — new data collection
- [ ] `index.html` — new panel or section
- [ ] `index.html` — change to existing panel: ____________
- [ ] `config.json` — new config key
- [ ] `themes.json` — theme change
- [ ] Deployment (Docker / Nix)
- [ ] Tests
- [ ] Other: ____________

## Constraints to Respect

<!-- Tick which constraints this feature must stay within. -->

- [ ] Zero frontend dependencies (no npm, no CDN, no build step)
- [ ] Zero backend dependencies (Python stdlib only)
- [ ] Single `index.html` file (no splitting into multiple JS files)
- [ ] 7-module JS structure (State / DataLayer / DirtyChecker / Renderer / Theme / Chat / App)
- [ ] XSS-safe: all dynamic values through `esc()`
- [ ] Works with all 6 built-in themes
- [ ] This feature intentionally relaxes a constraint (explain below)

## Alternatives Considered

<!-- What other approaches did you consider and reject, and why?
     "None" is acceptable. -->

## Acceptance Criteria

<!-- Define what "done" looks like. Write as testable statements.
     Agents use this directly to write tests.
     Example:
     - [ ] Clicking "Export CSV" on the token usage panel downloads a `.csv` file
     - [ ] CSV contains columns: model, input_tokens, output_tokens, cost
     - [ ] Export works for all 4 time ranges (today / 7d / 30d / all)
     - [ ] Button is visible in all 6 themes -->

- [ ]
- [ ]
- [ ]

## Additional Context

<!-- Mockups, related issues, external references, or example implementations
     from other projects. -->
