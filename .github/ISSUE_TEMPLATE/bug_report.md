---
name: Bug Report
about: Something is broken or behaving unexpectedly
labels: bug
---

## Summary

<!-- One sentence: what is broken and where. Be specific.
     Good: "cronBody table flickers on every refresh even when cron data is unchanged"
     Bad:  "the dashboard is slow" -->

## Environment

| Field | Value |
|-------|-------|
| Dashboard version | <!-- run: git describe --tags --> |
| Python version | <!-- run: python3 --version --> |
| Browser | <!-- e.g. Chrome 130, Firefox 131, Safari 18 --> |
| OS | <!-- e.g. macOS 15.3, Ubuntu 22.04 --> |
| OpenClaw version | <!-- run: openclaw --version --> |
| Install method | <!-- one-line install / manual / Docker / Nix --> |

## Steps to Reproduce

<!-- Numbered steps. Each step must be a single action.
     Include exact values (tab names, config keys, row counts) where relevant. -->

1.
2.
3.

## Expected Behaviour

<!-- What should happen after step N. -->

## Actual Behaviour

<!-- What actually happens. Include exact error messages, console output, or
     visual description. Copy-paste over paraphrasing. -->

## Affected Component

<!-- Tick all that apply -->

- [ ] `server.py` — HTTP server, /api/refresh, /api/chat
- [ ] `refresh.sh` — data collection script
- [ ] `index.html` JS — State / DataLayer / DirtyChecker
- [ ] `index.html` JS — Renderer (specific section: ____________)
- [ ] `index.html` JS — Theme / Chat / App
- [ ] `index.html` CSS — layout or visual
- [ ] `config.json` — configuration parsing
- [ ] `themes.json` — theme definitions
- [ ] Tests
- [ ] Other: ____________

## Relevant Logs

<!-- Paste server.log lines, browser console errors, or refresh.sh stderr.
     Use code blocks. Omit if not applicable. -->

```
paste logs here
```

## data.json Shape (if applicable)

<!-- If the bug is in data rendering, paste the relevant slice of data.json.
     Trim to the affected keys only — do not paste the full file. -->

```json
{
  "affectedKey": "value here"
}
```

## Workaround

<!-- Is there a way to avoid the bug right now? "None known" is a valid answer. -->

## Additional Context

<!-- Screenshots, screen recordings, related issues, or anything else that
     helps reproduce or understand the bug. -->
