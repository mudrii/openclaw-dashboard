**Validation verdict:** **Pass with changes**

The plan is strong and implementation-ready overall, but had one critical correctness gap (FOUC prevention approach) and one scope ambiguity (“bottom” section). I fixed both in v3.

---

### Critical fixes applied

1. **FOUC fix corrected**  
   The prior “early script after `<body>`” approach is unreliable because target section nodes may not be parsed yet.  
   **Fix:** bootstrap in `<head>` and inject a dynamic `<style>` for collapsed selectors before render.

2. **Section scope hardened**  
   **System topbar remains non-collapsible**.  
   **“Bottom” removed** until explicitly defined.

3. **State semantics locked**  
   `true === collapsed`, `false/absent === expanded` (except default override for `agent-config`).

4. **Single source of truth for keys**  
   Define one `SECTION_KEYS` list used by bootstrap + runtime module to avoid key drift.

5. **A11y + interaction safety kept robust**  
   Dedicated `<button>` toggle (not full-header click), `aria-expanded`, `aria-controls`, collapsed body via `display:none`.

---

## Final implementation plan v3

1. **Lock section inventory (9 keys)**
   - `alerts, health, cost, charts, crons, sessions, usage, subagent, agent-config`
   - Exclude: `system-topbar`
   - Storage key: `ocDash:collapse:v1`
   - Semantics: persisted map `{ [key]: booleanCollapsed }`

2. **Update `index.html` section structure**
   - For each section:
     - wrapper: `<section class="oc-section" data-section="health">`
     - header with title + dedicated `<button class="oc-section-toggle">`
     - body container: `<div class="oc-section-body" id="oc-body-health">…</div>`
   - Add global controls once: `Expand all` / `Collapse all`

3. **Add head bootstrap script (before main CSS/JS render completes)**
   - Read localStorage safely (`try/catch`)
   - Merge defaults: `{ "agent-config": true }`
   - Build CSS selector rules for collapsed keys and inject `<style id="oc-collapse-bootstrap">`
   - Store merged initial state on `window.__ocCollapsedInit` for runtime init reuse

4. **Add CSS rules**
   - `.oc-section.collapsed .oc-section-body { display:none; }`
   - `.oc-chevron` rotation for collapsed/expanded
   - Focus-visible styles for toggle button
   - `@media (prefers-reduced-motion: reduce)` disable chevron transition

5. **Implement `Sections` JS module**
   - `loadState()`, `saveState()` with localStorage `try/catch` and in-memory fallback
   - `isCollapsed(key)` using defaults
   - `applySectionState(el,key,collapsed)` updates class + `aria-expanded`
   - `toggle(key)`, `setAll(collapsed)`, `init()`
   - Use one shared `SECTION_KEYS` constant

6. **Initialize in app startup**
   - In `App.init()` call `Sections.init()`
   - On init, sync DOM class with `window.__ocCollapsedInit` / persisted state
   - Remove bootstrap style tag after class-based states are applied (optional cleanup)

7. **ATDD / verification**
   - Unit tests: load/save fallback, toggle behavior, aria sync, setAll, defaults
   - Manual QA: keyboard, screen reader, private mode, reload persistence

8. **Cross-backend validation**
   - Run the dashboard via the Go binary
   - Confirm identical behavior from source and release builds (shared `index.html` path)

---

## Final acceptance criteria checklist

- [ ] 9 target sections each have a visible toggle button.
- [ ] System topbar is always visible and never collapsible.
- [ ] Toggle changes only its own section.
- [ ] `aria-expanded` is correct after every toggle.
- [ ] Collapsed body is not tabbable (`display:none`).
- [ ] State persists across reloads with `localStorage`.
- [ ] `agent-config` is collapsed by default on first load.
- [ ] Expand All / Collapse All work and persist.
- [ ] No visible expand→collapse flash on load for persisted collapsed sections.
- [ ] Works when localStorage is unavailable (no crash; session-only behavior).
- [ ] Keyboard Enter/Space works on toggle button.
- [ ] Same behavior from source-served and release-served Go dashboard.

---

## Risk checklist

- [ ] **Layout regression** from added wrappers  
      Mitigation: verify existing CSS selectors; use minimal wrapper depth.
- [ ] **Key mismatch** between bootstrap and runtime module  
      Mitigation: one shared `SECTION_KEYS` constant.
- [ ] **Hidden dependencies on visible height** in existing widgets  
      Mitigation: audit code using `offsetHeight/getBoundingClientRect`; retest after expand.
- [ ] **localStorage exceptions** (private mode/quota)  
      Mitigation: full `try/catch` + in-memory fallback.
- [ ] **A11y drift** (aria not synced)  
      Mitigation: centralized `applySectionState()` updates class + aria together.

---

## Minimal open questions (blocking only)

1. **Confirm final section list:** is there any real “bottom” section to include, or do we proceed with the fixed 9-key list above?  
   (If unresolved, implementation should proceed with 9 only.)
