# Review & Revised Plan: Collapsible Sections for openclaw-dashboard

---

## Review Findings

### 1. Scoping Errors

**System topbar should NOT be collapsible.** The topbar contains critical navigation, status indicators, and the version badge. Collapsing it hides essential persistent UI. Every dashboard convention keeps the topbar fixed. This is a scoping mistake — remove it from the collapsible set.

**"Bottom" section is undefined.** What is "bottom"? Footer? A specific widget? This is ambiguous and not implementation-ready. Needs a concrete section name or should be dropped.

**Revised count: 9 sections** (alerts, health, cost, charts, crons, sessions, usage, subagent, agent-config). System topbar excluded. "Bottom" requires clarification before inclusion.

### 2. CSS Animation Strategy is Fragile

The `max-height + opacity` approach has well-known problems:
- Requires a hardcoded or calculated max-height value (can't animate to `auto`).
- If max-height is too high (e.g., `9999px`), the animation has a visible delay before content appears to move because it's animating from 9999px down.
- If max-height is too low, content gets clipped.
- Dashboard sections like sessions/crons have **dynamic content** that changes height at runtime — a static max-height will break.

**Fix:** Use JS-assisted `scrollHeight` measurement or CSS `grid-template-rows: 1fr → 0fr` (modern, supported in all current browsers). Or, pragmatically: skip animation entirely. This is an information-dense admin dashboard, not a consumer app. Instant toggle with `display: none` is perfectly fine and eliminates an entire class of bugs.

### 3. Boolean Semantics Undefined

The plan says `sectionKey->boolean` but never defines what the boolean means. Is `true` = collapsed or `true` = expanded? This ambiguity **will** cause a bug. Must be explicit.

**Fix:** Define `true` = collapsed (opt-in to collapse; default state is expanded, which means absent/false = expanded).

### 4. Missing Section Keys

No stable identifiers are defined for each section. The JS needs to map DOM elements to localStorage keys. Without an explicit key list, implementers will invent inconsistent keys.

**Fix:** Define an explicit enum of section keys in the plan.

### 5. Dynamic Content Timing

Sections like sessions, crons, and subagent are populated **after page load via JS**. If collapse state is applied in `App.init()` before content renders, there's a flash-of-content (content renders, then collapses). If applied after, there's a layout shift.

**Fix:** Apply collapsed class **before** first render by reading localStorage synchronously in a `<script>` block at the top of `<body>`, before the main app JS runs. This eliminates FOUC.

### 6. Keyboard/Focus Trap Missing

When a section is collapsed, its inner content must be removed from tab order. Otherwise keyboard users will tab through invisible elements. The draft mentions `focus-visible` but not `tabindex` management or `visibility: hidden` (which removes from tab order unlike `display: none` with opacity hacks).

**Fix:** Use `visibility: hidden` + `height: 0` for collapsed state (removes from tab order and accessibility tree) or set `inert` attribute on collapsed body.

### 7. Event Delegation Conflict

"Click section header to toggle" — but section headers may contain interactive elements (buttons, links, dropdowns). A naive click handler on the header div will intercept clicks on child elements.

**Fix:** Use a dedicated toggle button/icon within the header, not the entire header as a click target. Or: use event delegation with `e.target.closest('.oc-section-toggle')` check.

### 8. Architecture Validation ✅

Single shared `index.html` served by both Go and Python backends: **the plan is compatible.** All changes are frontend-only (HTML structure, CSS, JS). No backend changes needed. No divergence risk between Go and Python. The shared static file approach means both backends get the feature simultaneously.

One thing to validate: are CSS and JS inline in `index.html` or in separate static files? If separate, ensure both backends serve the same static directory. If inline, all changes go in one file.

### 9. No Error Handling for localStorage

`localStorage.getItem`/`setItem` can throw in private browsing mode (Safari), when quota is exceeded, or when storage is disabled. The draft mentions "fallback" but doesn't specify the mechanism.

**Fix:** Wrap all localStorage access in try/catch. Fallback = in-memory Map for current session (no persistence, but no crash).

### 10. `prefers-reduced-motion` Implementation Not Specified

The draft mentions "reduced-motion support" but doesn't say how. Does it disable animation? Make it instant? Use a different transition?

**Fix:** `@media (prefers-reduced-motion: reduce)` → set all transition durations to `0s`. Clean, simple, correct.

---

## Revised Final Implementation Plan

### Phase 0: Prerequisites

0.1. **Verify current HTML structure.** Read the actual `index.html` to identify the 9 section boundaries and their current wrapper elements. Determine if CSS/JS is inline or external.

0.2. **Define the section key enum:**

| Section Key     | Content                          | Default State |
|-----------------|----------------------------------|---------------|
| `alerts`        | Alert/notification panel         | expanded      |
| `health`        | System health indicators         | expanded      |
| `cost`          | Cost/billing metrics             | expanded      |
| `charts`        | Chart/graph panels               | expanded      |
| `crons`         | Cron job listing                 | expanded      |
| `sessions`      | Active sessions list             | expanded      |
| `usage`         | Usage statistics                 | expanded      |
| `subagent`      | Sub-agent status                 | expanded      |
| `agent-config`  | Agent configuration panel        | collapsed     |

*Agent-config defaults to collapsed because it's a settings panel, not a monitoring view.*

### Phase 1: HTML Structure (index.html)

1.1. **Wrap each section** in a container div:
```html
<div class="oc-section" data-section="health">
  <div class="oc-section-header">
    <span class="oc-section-title">System Health</span>
    <button class="oc-section-toggle" aria-expanded="true" aria-controls="oc-body-health">
      <svg class="oc-chevron"><!-- chevron-down icon --></svg>
    </button>
  </div>
  <div class="oc-section-body" id="oc-body-health">
    <!-- existing section content unchanged -->
  </div>
</div>
```

1.2. **Key structural decisions:**
- Toggle is a **real `<button>` element** (not role=button on a div) — correct semantics, free keyboard handling, no tabindex needed.
- `aria-expanded` on the button, `aria-controls` pointing to the body `id`.
- `data-section` attribute holds the section key for JS lookup.
- Existing content inside `.oc-section-body` is **unchanged** — just wrapped.

1.3. **Add global controls** in the main dashboard header (not inside any section):
```html
<div class="oc-global-controls">
  <button id="oc-expand-all" title="Expand all sections">Expand All</button>
  <button id="oc-collapse-all" title="Collapse all sections">Collapse All</button>
</div>
```

### Phase 2: CSS

2.1. **Collapse mechanics** — use `display: none` for simplicity and robustness:
```css
.oc-section.collapsed .oc-section-body {
  display: none;
}

.oc-section.collapsed .oc-chevron {
  transform: rotate(-90deg);
}

.oc-chevron {
  transition: transform 0.2s ease;
  width: 16px;
  height: 16px;
}

@media (prefers-reduced-motion: reduce) {
  .oc-chevron {
    transition: none;
  }
}
```

**Why `display: none` over max-height animation:**
- Zero bugs with dynamic content heights.
- Collapsed content is automatically removed from tab order and accessibility tree.
- No need for `inert`, `visibility: hidden`, or tabindex management.
- Chevron rotation provides sufficient visual feedback for the toggle action.
- This is an admin dashboard; smooth expand/collapse animation adds no value and introduces fragility.

2.2. **Section header styling:**
```css
.oc-section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  cursor: default;
  padding: 8px 12px;
  user-select: none;
}

.oc-section-toggle {
  background: none;
  border: none;
  cursor: pointer;
  padding: 4px;
  border-radius: 4px;
  color: inherit;
}

.oc-section-toggle:focus-visible {
  outline: 2px solid var(--oc-focus-color, #4d90fe);
  outline-offset: 2px;
}
```

### Phase 3: JavaScript

3.1. **Early collapse application** (prevents FOUC). Add a `<script>` block **immediately after `<body>` opens**, before the main app script:
```javascript
(function() {
  try {
    var state = JSON.parse(localStorage.getItem('ocDash:collapse:v1') || '{}');
    // Apply default: agent-config starts collapsed
    var defaults = { 'agent-config': true };
    var merged = Object.assign({}, defaults, state);
    Object.keys(merged).forEach(function(key) {
      if (merged[key]) {
        var el = document.querySelector('[data-section="' + key + '"]');
        if (el) el.classList.add('collapsed');
      }
    });
  } catch (e) { /* localStorage unavailable — no persistence this session */ }
})();
```

3.2. **Sections module** (in the main app JS):
```javascript
const Sections = (() => {
  const STORAGE_KEY = 'ocDash:collapse:v1';
  const SECTION_KEYS = ['alerts','health','cost','charts','crons','sessions','usage','subagent','agent-config'];
  const DEFAULTS = { 'agent-config': true }; // true = collapsed
  let memoryFallback = null; // used if localStorage unavailable

  function load() {
    try {
      return JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}');
    } catch {
      memoryFallback = memoryFallback || {};
      return { ...memoryFallback };
    }
  }

  function save(state) {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
    } catch {
      memoryFallback = { ...state };
    }
  }

  function isCollapsed(key) {
    const state = load();
    return key in state ? state[key] : (DEFAULTS[key] || false);
  }

  function toggle(sectionKey) {
    const el = document.querySelector(`[data-section="${sectionKey}"]`);
    if (!el) return;
    const nowCollapsed = !el.classList.contains('collapsed');
    el.classList.toggle('collapsed', nowCollapsed);
    // Update aria
    const btn = el.querySelector('.oc-section-toggle');
    if (btn) btn.setAttribute('aria-expanded', String(!nowCollapsed));
    // Persist
    const state = load();
    state[sectionKey] = nowCollapsed;
    save(state);
  }

  function setAll(collapsed) {
    const state = load();
    SECTION_KEYS.forEach(key => {
      const el = document.querySelector(`[data-section="${key}"]`);
      if (!el) return;
      el.classList.toggle('collapsed', collapsed);
      const btn = el.querySelector('.oc-section-toggle');
      if (btn) btn.setAttribute('aria-expanded', String(!collapsed));
      state[key] = collapsed;
    });
    save(state);
  }

  function init() {
    // Bind toggle buttons (early script handled initial class, this adds interactivity)
    document.querySelectorAll('.oc-section-toggle').forEach(btn => {
      const section = btn.closest('.oc-section');
      if (!section) return;
      const key = section.dataset.section;
      // Sync aria-expanded with actual DOM state (from early script)
      btn.setAttribute('aria-expanded', String(!section.classList.contains('collapsed')));
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        toggle(key);
      });
    });

    // Global controls
    const expandBtn = document.getElementById('oc-expand-all');
    const collapseBtn = document.getElementById('oc-collapse-all');
    if (expandBtn) expandBtn.addEventListener('click', () => setAll(false));
    if (collapseBtn) collapseBtn.addEventListener('click', () => setAll(true));
  }

  return { init, toggle, expandAll: () => setAll(false), collapseAll: () => setAll(true) };
})();
```

3.3. **Integration into App.init():**
```javascript
// In App.init(), after DOM setup:
Sections.init();
// Expose globally if needed:
window.OCUI = window.OCUI || {};
OCUI.expandAll = Sections.expandAll;
OCUI.collapseAll = Sections.collapseAll;
```

### Phase 4: Testing

See Test Strategy section below.

### Phase 5: Verification & Merge

5.1. Test in both Go and Python backend modes — serve the same `index.html`, verify identical behavior.
5.2. Test with localStorage disabled (Safari private mode).
5.3. Test with keyboard-only navigation.
5.4. Test with screen reader (VoiceOver).
5.5. Code review, squash-merge.

---

## Acceptance Criteria Checklist

- [ ] **AC1:** Each of the 9 defined sections has a visible toggle button in its header.
- [ ] **AC2:** Clicking a toggle button collapses/expands the corresponding section body.
- [ ] **AC3:** Chevron icon rotates to indicate collapsed (right-pointing) vs expanded (down-pointing) state.
- [ ] **AC4:** Collapse state persists across page reloads via localStorage.
- [ ] **AC5:** "Expand All" button expands all sections and persists the state.
- [ ] **AC6:** "Collapse All" button collapses all sections and persists the state.
- [ ] **AC7:** Keyboard users can toggle sections via Enter/Space on the toggle button.
- [ ] **AC8:** Screen readers announce expanded/collapsed state via `aria-expanded`.
- [ ] **AC9:** Collapsed section content is not reachable via Tab key.
- [ ] **AC10:** When localStorage is unavailable, toggle still works (no persistence, no errors).
- [ ] **AC11:** No FOUC — sections that were collapsed before reload appear collapsed immediately, with no visible expand→collapse flash.
- [ ] **AC12:** agent-config section defaults to collapsed on first visit.
- [ ] **AC13:** System topbar is NOT collapsible and remains always visible.
- [ ] **AC14:** Dashboard renders identically when served from Go backend and Python backend.
- [ ] **AC15:** `prefers-reduced-motion` users see no animation (chevron transition disabled).

---

## Test Strategy

### Automated Tests (ATDD)

**Unit tests for Sections module** (use existing test framework or add a minimal one):

| Test | Assertion |
|------|-----------|
| `toggle()` adds `.collapsed` class | DOM class check |
| `toggle()` twice restores original state | Roundtrip |
| `toggle()` updates `aria-expanded` | Attribute check |
| `save/load` roundtrip | localStorage mock |
| `load()` with corrupted JSON returns `{}` | Error handling |
| `load()` with localStorage disabled returns `{}` | memoryFallback used |
| `setAll(true)` collapses all 9 sections | Bulk operation |
| `setAll(false)` expands all 9 sections | Bulk operation |
| `init()` syncs aria-expanded with DOM state | Post-init check |
| Default state: agent-config collapsed, rest expanded | First-load behavior |

**Static analysis:**
- HTML validator: all `id` values unique, `aria-controls` IDs match.
- CSS lint: no `!important` except overrides with clear comments.
- JS lint: no unhandled exceptions.

### Manual QA Checklist

- [ ] Fresh browser (no localStorage) — verify defaults.
- [ ] Collapse 3 sections, reload — verify those 3 are still collapsed.
- [ ] Private browsing mode — toggle works, no console errors.
- [ ] Keyboard-only: Tab to toggle, press Enter, confirm toggle.
- [ ] Screen reader (VoiceOver): navigate to section, hear "expanded"/"collapsed".
- [ ] Resize browser to mobile width — sections still collapse correctly.
- [ ] Open via Go backend, repeat via Python backend — identical behavior.
- [ ] Clear localStorage, reload — resets to defaults.

---

## Risks + Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| Wrapping sections in `.oc-section` breaks existing CSS grid/flex layouts | **High** | Test each section after wrapping. Use `display: contents` on wrapper if needed as escape hatch. Carefully review existing CSS selectors that depend on parent-child relationships. |
| Dynamic content (sessions list, cron table) has event handlers that break when parent is `display: none` | **Medium** | `display: none` preserves DOM — event handlers remain. But any JS that measures element dimensions while collapsed will get 0. Audit for `offsetHeight`/`getBoundingClientRect` calls in existing code. |
| localStorage quota exceeded in edge cases | **Low** | The state object is <200 bytes. Not a real risk, but try/catch handles it. |
| Early `<script>` runs before DOM elements exist (if sections are rendered dynamically) | **Medium** | Verify all 9 section `[data-section]` elements are in static HTML, not dynamically created. If any are dynamic, the early script silently skips them (fine — the Sections.init() in App.init() handles the rest). |
| Multiple dashboard tabs writing to same localStorage key | **Low** | Last-write-wins is acceptable for preferences. No need for `storage` event sync. |
| CSS class name `.oc-section` conflicts with existing styles | **Low** | Grep existing CSS for `.oc-section` before implementation. The `oc-` prefix is namespaced and unlikely to conflict. |

---

## Delta vs Sonnet

| # | Sonnet Draft | This Revision | Why |
|---|-------------|---------------|-----|
| 1 | 11 sections including system topbar | 9 sections; topbar excluded | Topbar is persistent navigation — collapsing it violates UX conventions and hides critical status |
| 2 | `role=button` on header div | Real `<button>` element for toggle | Native button = free keyboard handling, correct semantics, no tabindex/role hacks needed |
| 3 | `max-height + opacity` animation | `display: none` + chevron rotation only | max-height is fragile with dynamic content heights; display:none is robust, eliminates tab-order/focus bugs for free, and animation adds no value in an admin dashboard |
| 4 | Entire header clickable | Dedicated toggle button in header | Prevents click-conflict with interactive elements inside section headers |
| 5 | Boolean semantics unspecified | Explicit: `true` = collapsed | Prevents implementer ambiguity and inevitable off-by-one-boolean bug |
| 6 | No FOUC prevention | Early synchronous `<script>` before main JS | Eliminates visible flash-of-expanded-then-collapse on page load |
| 7 | "localStorage with fallback" (vague) | In-memory Map fallback with try/catch on every access | Concrete implementation, handles Safari private mode, quota exceeded |
| 8 | Section keys not enumerated | Explicit key enum with defaults table | Implementation-ready; no room for ad-hoc key invention |
| 9 | A11y mentions `focus-visible` and `aria` | Added: collapsed content removed from tab order (via display:none), `inert` not needed, real button for focus | More complete a11y story |
| 10 | `prefers-reduced-motion` "support" (vague) | Specific: `transition: none` in media query | Concrete CSS rule, not a hand-wave |
| 11 | No default collapsed sections | agent-config defaults to collapsed | Config is a settings panel, not a monitoring view — should be tucked away by default |
| 12 | "Bottom" section included | Excluded until defined | Can't implement something unnamed |

---

## Open Questions

1. **Is "bottom" a real section?** If so, what's its `data-section` key and content? Needs answer before implementation.

2. **Are any of the 9 sections dynamically created by JS?** The early-script FOUC prevention only works for sections present in static HTML. If any section is injected by `App.init()` or a fetch callback, the init sequence needs adjustment.

3. **Are CSS and JS inline in `index.html` or in external files?** This determines where the new code lives. If inline, it all goes in one file. If external, we may need to add a new CSS block and JS module.

4. **Are there existing interactive elements in section headers?** (buttons, dropdowns, links) — this affects whether the dedicated toggle button approach is sufficient or if we need additional event delegation safeguards.

5. **Should there be a URL parameter override?** E.g., `?sections=all-expanded` for sharing a dashboard link with full visibility, bypassing localStorage state. Low priority but worth deciding now.

6. **Mobile/responsive behavior:** Should sections auto-collapse on small screens to save space? Out of scope for v1 but worth noting as a v2 enhancement.