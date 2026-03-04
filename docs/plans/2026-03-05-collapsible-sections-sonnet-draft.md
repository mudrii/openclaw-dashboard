Now I have everything I need. Here's the complete, implementation-ready plan:

---

# OpenClaw Dashboard — Collapsible Sections: Implementation Plan

**File:** `docs/plans/2026-03-05-collapsible-sections.md`  
**Branch:** `exp/collapsible-sections`  
**Date:** 2026-03-05  
**Author:** Planning subagent (for review before execution)

---

## Executive Summary

The dashboard currently renders 11 sections in a fixed, always-visible layout. This plan adds per-section collapse/expand toggles with:
- **Zero backend changes** — 100% client-side, one `index.html` edit covers both Go binary and Python server modes
- **localStorage persistence** — collapse state survives page reloads with versioned schema and graceful fallback
- **Full accessibility** — ARIA attributes, keyboard navigation, focus indicators, prefers-reduced-motion
- **ATDD-first delivery** — failing tests written before any HTML/CSS/JS touches
- **Max-height CSS animation** — smooth 300ms collapse, no JS animation loops

Estimated LOC: ~250 CSS + ~120 JS + ~100 HTML restructuring. No new deps.

---

## 1. Scope — All Collapsible Sections

| # | Section Name | Current DOM Anchor | Has `.section-header`? | Section Key |
|---|---|---|---|---|
| 1 | System Metrics Topbar | `#systemTopBar` + `div.system-topbar` | ❌ None | `systembar` |
| 2 | Alerts Banner | `#alertsSection` + `div.alerts-section` | ❌ None | `alerts` |
| 3 | System Health Row | `#healthRow` + `div.health-row` | ❌ None | `health` |
| 4 | Cost Overview | `div.cost-row` | ❌ None | `cost` |
| 5 | Charts & Trends | `div.charts-section` | ✅ `.section-header` inside | `charts` |
| 6 | Cron Jobs | `div[mb:24px]` first | ✅ `.section-header` inside | `crons` |
| 7 | Active Sessions | `div[mb:24px]` second | ✅ `.section-header` inside | `sessions` |
| 8 | Token Usage & Cost | `div[mb:24px]` third | ✅ `.section-header` + tabs | `usage` |
| 9 | Sub-Agent Activity | `div[mb:24px]` fourth (includes token breakdown sub-section) | ✅ `.section-header` + tabs | `subagent` |
| 10 | Bottom Row (Models + Skills + Git) | `div.grid-3` | Each panel has its own `.section-header`; treat as one outer section | `bottom` |
| 11 | Agent Configuration | `div.glass.panel[mt:16px]` | ✅ `.section-header` inside | `agentconfig` |

**Sections 1–4** require a new wrapper div + new collapsible header to be added.  
**Sections 5–11** already have `.section-header` which gets converted to the clickable toggle header.

---

## 2. UX Behavior

### 2.1 Default State
- **All 11 sections expanded** on first load (no saved state)
- Dashboard behaves identically to today for new users

### 2.2 Per-Section Toggle
- Clicking anywhere on a **section header row** collapses/expands that section
- A **chevron icon** (▾ expanded / ▸ collapsed) appears at the right of every section header
- The chevron rotates 90° via CSS transform on state change
- Toggle is idempotent: clicking again re-expands

### 2.3 Global Controls
- Two buttons added to `div.header-right` (between "↻ Refresh" and theme picker):
  - **"⊟ Collapse All"** — collapses all sections at once
  - **"⊞ Expand All"** — expands all sections at once
- Both buttons use existing `.refresh-btn` visual style

### 2.4 Animation
- **Expand:** `max-height: 0 → 5000px` over `300ms ease-out`, `opacity: 0 → 1` over `200ms`
- **Collapse:** `max-height: 5000px → 0` over `300ms ease-in`, `opacity: 1 → 0` over `150ms`
- `overflow: hidden` during transition, `overflow: visible` once expanded (allows tooltips/dropdowns to break out)
- `@media (prefers-reduced-motion: reduce)` → `transition: none` (instant show/hide)

### 2.5 Affordance
- Section header cursor: `pointer` when hoverable
- Section header `background` subtly changes on hover (`var(--surfaceHover)`)
- Chevron visible in `var(--muted)` color; brightens to `var(--textStrong)` on header hover
- Collapsed sections show a 1px bottom border on the header to visually indicate "more content below"

### 2.6 Alerts Section Special Behavior
- When `alerts.length === 0`: entire Alerts section wrapper is `display:none` (existing behavior preserved — no empty heading cluttering the layout)
- When `alerts.length > 0` AND section is collapsed: header shows "⚠️ Alerts (N)" count badge
- Collapse toggle only applies when section is visible (i.e., has alerts)

---

## 3. Accessibility Requirements

### 3.1 ARIA
```html
<div class="oc-section-header" 
     role="button" 
     tabindex="0" 
     aria-expanded="true" 
     aria-controls="section-cost-body"
     id="section-cost-hdr">
  <span class="section-title">💰 Cost Overview</span>
  <button class="oc-collapse-btn" 
          aria-hidden="true" 
          tabindex="-1">▾</button>
</div>
<div class="oc-section-body" 
     id="section-cost-body" 
     role="region" 
     aria-labelledby="section-cost-hdr">
  <!-- content -->
</div>
```
- `role="button"` on the header div (not the chevron button — that's `aria-hidden`)
- `aria-expanded` reflects current state ("true"/"false")
- `aria-controls` links to the body element by ID
- Body has `role="region"` + `aria-labelledby` pointing to the header
- When collapsed, body gets `aria-hidden="true"` (screen readers skip invisible content)

### 3.2 Keyboard Navigation
- **Tab**: moves focus through section headers in DOM order
- **Enter / Space**: toggles the focused section (prevent default scroll on Space)
- **No arrow-key navigation** between sections (they are not a composite widget, just a list of buttons)
- "Collapse All" and "Expand All" buttons are standard `<button>` elements — fully keyboard accessible out of the box

### 3.3 Focus Indicator
- Section headers get `:focus-visible` outline: `2px solid var(--accent)` with `2px offset`
- Do NOT use `:focus` (avoids showing outline on mouse click in modern browsers)

### 3.4 Reduced Motion
```css
@media (prefers-reduced-motion: reduce) {
  .oc-section-body {
    transition: none !important;
  }
}
```
- Instant visibility change with no animation when user has this preference set

---

## 4. State Persistence Strategy

### 4.1 localStorage Key Design
```
Key:   "ocDash:collapse:v1"
Value: JSON object mapping section keys to boolean (true = expanded)
```

Example stored value:
```json
{
  "systembar": true,
  "alerts": true,
  "health": false,
  "cost": true,
  "charts": true,
  "crons": false,
  "sessions": true,
  "usage": true,
  "subagent": true,
  "bottom": true,
  "agentconfig": false
}
```

### 4.2 Defaults Object (source of truth)
```js
const SECTION_DEFAULTS = {
  systembar:  true,
  alerts:     true,
  health:     true,
  cost:       true,
  charts:     true,
  crons:      true,
  sessions:   true,
  usage:      true,
  subagent:   true,
  bottom:     true,
  agentconfig: true,
};
```

### 4.3 Load Logic
```js
Sections.load():
  1. Try localStorage.getItem("ocDash:collapse:v1")
  2. If null/empty → use SECTION_DEFAULTS (all expanded)
  3. If JSON.parse throws → use SECTION_DEFAULTS (log warning to console)
  4. If valid object → merge: state = { ...SECTION_DEFAULTS, ...parsed }
     (new sections added in future code get their default, old unknown keys are ignored)
```

### 4.4 Save Logic
```js
Sections.save():
  1. JSON.stringify(this._state)
  2. localStorage.setItem("ocDash:collapse:v1", serialized)
  3. Catch any error silently (quota exceeded, private browsing)
```

### 4.5 Versioning & Migration
- **v1** is the first version — no migration needed
- **Future v2** migration pattern (documented for reference):
  ```js
  const v1 = localStorage.getItem("ocDash:collapse:v1");
  if (v1) {
    const migrated = migrate_v1_to_v2(JSON.parse(v1));
    localStorage.setItem("ocDash:collapse:v2", JSON.stringify(migrated));
    localStorage.removeItem("ocDash:collapse:v1");
  }
  ```
- No migration needed now; just note the pattern in code comments

### 4.6 Fallback Behavior Matrix
| Condition | Result |
|---|---|
| No localStorage key | All sections expanded (defaults) |
| Malformed JSON | All sections expanded (defaults) + `console.warn` |
| localStorage unavailable (private mode, security policy) | All sections expanded (defaults), save silently fails |
| Section key missing in stored object | That section uses its default (expanded) |
| Unknown key in stored object | Ignored (forward compatibility) |

---

## 5. Technical Implementation Design

### 5.1 DOM Structure

**Sections 1–4 (no existing header) — add wrapper + new header:**
```html
<div class="oc-section" id="section-health">
  <div class="oc-section-header section-header" 
       role="button" tabindex="0"
       aria-expanded="true" aria-controls="section-health-body"
       id="section-health-hdr">
    <span class="section-title">🔋 System Health</span>
    <span class="oc-collapse-btn" aria-hidden="true">▾</span>
  </div>
  <div class="oc-section-body" id="section-health-body"
       role="region" aria-labelledby="section-health-hdr">
    <!-- original health-row content -->
    <div class="health-row" id="healthRow">...</div>
  </div>
</div>
```

**Sections 5–11 (existing `.section-header`) — inject chevron, add wrapper + IDs:**
```html
<div class="oc-section" id="section-crons">
  <div class="section-header oc-section-header"
       role="button" tabindex="0"
       aria-expanded="true" aria-controls="section-crons-body"
       id="section-crons-hdr">
    <span class="section-title">⏰ Cron Jobs</span>
    <div style="display:flex;align-items:center;gap:8px">
      <span class="section-count" id="cronCount"></span>
      <span class="oc-collapse-btn" aria-hidden="true">▾</span>
    </div>
  </div>
  <div class="oc-section-body" id="section-crons-body"
       role="region" aria-labelledby="section-crons-hdr">
    <div class="glass panel" style="overflow-x:auto">
      <table class="dtable" id="cronTable">...</table>
    </div>
  </div>
</div>
```

### 5.2 CSS Additions (add to `<style>` block)

```css
/* ── Collapsible Sections ─────────────────────────────── */
.oc-section { margin-bottom: 24px; }

/* Headers that double as toggle buttons */
.oc-section-header {
  cursor: pointer;
  user-select: none;
  border-radius: 8px;
  padding: 6px 8px;
  transition: background 0.15s;
}
.oc-section-header:hover {
  background: var(--surfaceHover);
}
.oc-section-header:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

/* Chevron */
.oc-collapse-btn {
  font-size: 14px;
  color: var(--muted);
  line-height: 1;
  transition: transform 0.2s ease, color 0.15s;
  display: inline-block;
  flex-shrink: 0;
}
.oc-section-header:hover .oc-collapse-btn {
  color: var(--textStrong);
}
.oc-section[data-collapsed="true"] .oc-collapse-btn {
  transform: rotate(-90deg);
}

/* Collapsible body */
.oc-section-body {
  overflow: hidden;
  max-height: 5000px;
  opacity: 1;
  transition: max-height 0.3s ease-out, opacity 0.2s ease;
}
.oc-section-body.oc-collapsed {
  max-height: 0 !important;
  opacity: 0;
  pointer-events: none;
}

/* Border cue when collapsed */
.oc-section[data-collapsed="true"] > .oc-section-header {
  border-bottom: 1px solid var(--border);
  border-radius: 8px;
}

/* Reduced motion — instant, no animation */
@media (prefers-reduced-motion: reduce) {
  .oc-section-body,
  .oc-collapse-btn {
    transition: none !important;
  }
}

/* Global control buttons */
.oc-global-btn {
  background: var(--surface);
  border: 1px solid var(--border);
  padding: 5px 10px;
  border-radius: 8px;
  color: var(--muted);
  font-size: 11px;
  cursor: pointer;
  transition: all 0.15s;
  line-height: 1;
}
.oc-global-btn:hover {
  background: var(--surfaceHover);
  color: var(--textStrong);
}
```

### 5.3 JS Module — `Sections` (add before `App.init()`)

```js
// === Collapsible Sections ===
const Sections = {
  KEY: 'ocDash:collapse:v1',
  DEFAULTS: {
    systembar:   true,
    alerts:      true,
    health:      true,
    cost:        true,
    charts:      true,
    crons:       true,
    sessions:    true,
    usage:       true,
    subagent:    true,
    bottom:      true,
    agentconfig: true,
  },
  _state: {},

  load() {
    try {
      const raw = localStorage.getItem(this.KEY);
      const parsed = raw ? JSON.parse(raw) : {};
      this._state = { ...this.DEFAULTS, ...parsed };
    } catch (e) {
      console.warn('[Sections] Failed to parse collapse state, using defaults', e);
      this._state = { ...this.DEFAULTS };
    }
  },

  save() {
    try {
      localStorage.setItem(this.KEY, JSON.stringify(this._state));
    } catch (e) {
      // localStorage unavailable (private mode, quota)
    }
  },

  toggle(id) {
    this._state[id] = !this._state[id];
    this.save();
    this._applyOne(id);
  },

  expandAll() {
    Object.keys(this.DEFAULTS).forEach(k => this._state[k] = true);
    this.save();
    this._applyAll();
  },

  collapseAll() {
    Object.keys(this.DEFAULTS).forEach(k => this._state[k] = false);
    this.save();
    this._applyAll();
  },

  _applyOne(id) {
    const wrap = document.getElementById('section-' + id);
    if (!wrap) return;
    const body   = document.getElementById('section-' + id + '-body');
    const header = wrap.querySelector('.oc-section-header');
    const expanded = !!this._state[id];

    wrap.dataset.collapsed = expanded ? 'false' : 'true';
    if (body) {
      body.classList.toggle('oc-collapsed', !expanded);
      body.setAttribute('aria-hidden', expanded ? 'false' : 'true');
    }
    if (header) {
      header.setAttribute('aria-expanded', expanded ? 'true' : 'false');
    }
  },

  _applyAll() {
    Object.keys(this.DEFAULTS).forEach(id => this._applyOne(id));
  },

  init() {
    this.load();
    this._applyAll();

    // Click delegation: any .oc-section-header click toggles its section
    document.addEventListener('click', e => {
      const header = e.target.closest('.oc-section-header');
      if (!header) return;
      const section = header.closest('.oc-section');
      if (!section) return;
      const id = section.id.replace('section-', '');
      if (id in this.DEFAULTS) this.toggle(id);
    });

    // Keyboard support on section headers
    document.addEventListener('keydown', e => {
      if (e.key !== 'Enter' && e.key !== ' ') return;
      const header = e.target.closest('.oc-section-header');
      if (!header) return;
      e.preventDefault();
      const section = header.closest('.oc-section');
      if (!section) return;
      const id = section.id.replace('section-', '');
      if (id in this.DEFAULTS) this.toggle(id);
    });
  },
};
```

**Wire into App.init():**
```js
// Add at the start of App.init():
Sections.init();
```

**Expose on OCUI:**
```js
window.OCUI = {
  // ... existing entries ...
  expandAll:  () => Sections.expandAll(),
  collapseAll: () => Sections.collapseAll(),
};
```

### 5.4 JS — Alerts Section Dynamic Visibility

In `Renderer.render()`, the Alerts block update becomes:
```js
if (flags.alerts) {
  const alerts = D.alerts || [];
  const sectionWrap = document.getElementById('section-alerts');
  $('alertsSection').innerHTML = alerts.length