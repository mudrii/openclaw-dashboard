# OpenClaw Dashboard — Architecture Refactor Plan

> Status (as of 2026-02-27): **implemented**. All modules live in a single `<script>` tag.
>
> Constraints: single-file frontend, zero dependencies, no build step.

## Current State (~750 lines JS)

**Previous problems (now resolved):**
- 11 loose globals → consolidated into `State` object
- Monolithic `render()` → `Renderer.render(snap, flags)` dispatches by dirty flags
- No separation → State / DataLayer / DirtyChecker / Renderer / Theme / Chat / App
- Inline handlers → `window.OCUI` namespace
- Theme globals → `Theme` object

## Proposed Module Structure

Seven plain JS objects inside a single `<script>` tag. No classes, no frameworks.

```
┌─────────────────────────────────────────────────┐
│                    App.init()                    │
│         (wires everything, starts timer)         │
└────────┬──────────────┬──────────────┬──────────┘
         │              │              │
    ┌────▼────┐   ┌─────▼─────┐  ┌────▼─────┐
    │  State  │◄──│ DataLayer │  │  Theme   │
    │ (truth) │   │  (fetch)  │  │ (colors) │
    └────┬────┘   └───────────┘  └──────────┘
         │
    ┌────▼────────────┐
    │  DirtyChecker   │
    │ (what changed?) │
    └────┬────────────┘
         │
    ┌────▼────┐
    │Renderer │
    │ (DOM)   │
    └─────────┘
```

### Data Flow

```
Timer tick / manual refresh
  → DataLayer.fetch()
  → State.update(newData)
  → App.renderNow():
      snap = State.snapshot()         // deep-frozen clone
      flags = DirtyChecker.diff(snap) // 13 boolean dirty flags
      requestAnimationFrame(() => {
        Renderer.render(snap, flags)  // pure DOM effects
        State.commitPrev(snap)        // prev = snap (inside rAF)
      })
```

---

## Module Breakdown

### 1. `State` — Single Source of Truth (~40 lines)

**Owns:**
- `data` — the fetched API response (currently `D`)
- `prev` — previous snapshot (currently `prevD`)
- `tabs` — `{ usage: 'today', subRuns: 'today', subTokens: 'today' }` (replaces `uTab`, `srTab`, `stTab`)
- `prevTabs` — `{ usage, subRuns, subTokens }` (replaces `prevUTab`, `prevSrTab`, `prevStTab`)
- `chartDays` — `7 | 30`
- `prevChartDays`
- `countdown` — seconds until next refresh

**Methods:**
| Method | Description |
|--------|-------------|
| `update(newData)` | Sets `data`, called after fetch |
| `setTab(group, value)` | Sets `tabs[group]` — e.g., `State.setTab('usage', '7d')` |
| `setChartDays(n)` | Sets `chartDays` |
| `commitPrev(snap)` | Sets `prev = snap.data`, `prevTabs = snap.tabs`, `prevChartDays = snap.chartDays` — called inside rAF after Renderer.render() |
| `resetCountdown()` | Sets `countdown = 60` |
| `snapshot()` | Returns deep-frozen copy of current state for render cycle use |
| `tick()` | Decrements `countdown`, returns `true` if hit 0 |

**Depends on:** Nothing.

### 2. `DataLayer` — Fetching & Refresh (~25 lines)

**Owns:** Nothing (stateless).

**Methods:**
| Method | Description |
|--------|-------------|
| `fetch()` | `GET /api/refresh?t=...`, returns parsed JSON |

**Depends on:** Nothing. Caller (`App`) writes result into `State`.

### 3. `DirtyChecker` — All Comparison Logic (~50 lines)

**Owns:** Nothing (pure functions).

**Methods:**
| Method | Description |
|--------|-------------|
| `sectionChanged(keys)` | Compares `State.data[key]` vs `State.prev[key]` via JSON stringify |
| `stableChanged(arrKey, fields)` | Like `sectionChanged` but uses `stableSnapshot()` — strips volatile timestamps |
| `tabChanged(group)` | `State.tabs[group] !== State.prevTabs[group]` |
| `chartDaysChanged()` | `State.chartDays !== State.prevChartDays` |
| `diff(snap)` | Returns `{ alerts, health, cost, crons, sessions, usage, subRuns, subTokens, charts, models, skills, git, agentConfig }` — 13 boolean flags. Accepts frozen snapshot, reads `State.prev` / `State.prevTabs` for comparison. |

**Depends on:** `State` (reads `.data`, `.prev`, `.tabs`, `.prevTabs`).

**Migration note:** `sectionChanged()`, `stableSnapshot()`, and all the `if (!prevD || ...)` conditionals live here. `App.renderNow()` calls `DirtyChecker.diff(snap)` and passes the result into `Renderer.render(snap, flags)` — the Renderer never computes dirty flags itself.

### 4. `Renderer` — All DOM Updates (~500 lines)

**Owns:** Nothing persistent. Pure DOM side-effects.

**Top-level method:**
| Method | Description |
|--------|-------------|
| `render(snap, dirtyFlags)` | Dispatches to section renderers based on flags. Receives frozen snapshot and pre-computed dirty flags from `App`. |

**Section renderers** (one function each):
| Function | DOM targets | Dirty flag |
|----------|------------|------------|
| `renderHeader()` | `#botName`, `#botEmoji`, `#statusDot`, `#statusText` | always |
| `renderAlerts()` | `#alertsSection` | `alerts` |
| `renderHealth()` | `#hGw`, `#hPid`, `#hUp`, `#hMem`, `#hComp`, `#hSess` | always (volatile) |
| `renderCost()` | `#cToday`, `#cAll`, `#cProj`, `#donut`, `#donutLegend` | `cost` |
| `renderCrons()` | `#cronBody`, `#cronCount` | `crons` |
| `renderSessions()` | `#sessBody`, `#sessCount`, `#agentTree` | `sessions` |
| `renderUsageTable()` | `#uBody` + tab buttons | `usage` |
| `renderSubRuns()` | `#srBody`, `#subCostLbl`, `#srEmpty` + tab buttons | `subRuns` |
| `renderSubTokens()` | `#stBody` + tab buttons | `subTokens` |
| `renderCharts()` | `#costChart`, `#modelChart`, `#subagentChart` + tab buttons | `charts` |
| `renderModels()` | `#modelsGrid` | `models` |
| `renderSkills()` | `#skillsGrid` | `skills` |
| `renderGit()` | `#gitPanel` | `git` |
| `renderAgentConfig()` | all agent config panels | `agentConfig` |

**Sub-renderers within `renderAgentConfig()`:**
- `renderAgentCards()`
- `renderModelRouting()`
- `renderRuntimeConfig()`
- `renderSearchPanel()`
- `renderGatewayPanel()`
- `renderHooksPanel()`
- `renderPluginsPanel()`
- `renderBindings()`
- `renderSubagentConfig()`
- `renderAgentTable()`

**Helper functions (stay in Renderer):**
- `renderTokenTbl(bodyId, data, accentColor)` — shared by usage + subTokens
- `renderAgentTree()` — called from `renderSessions()`
- `renderCostChart(id, data)`
- `renderModelChart(id, data)`
- `renderSubagentChart(id, data)`
- `setTabCls4(prefix, tab, cls)` — tab button class toggler

**Depends on:** `State` (reads `.data`, `.tabs`, `.chartDays`).

### 5. `Theme` — Theme Engine (~80 lines, mostly unchanged)

**Owns:**
- `themes` — loaded theme definitions (currently `THEMES`)
- `current` — current theme ID

**Methods:**
| Method | Description |
|--------|-------------|
| `load()` | Fetch `/themes.json`, apply saved theme |
| `apply(id)` | Set CSS variables, save to localStorage |
| `renderMenu()` | Populate `#themeMenu` |
| `toggle()` | Open/close menu |

**Depends on:** Nothing. Self-contained.

### 6. `Chat` — AI Chat Panel (~50 lines)

**Owns:**
- `history` — chat message history
- `MAX_HISTORY` — max history length

**Methods:**
| Method | Description |
|--------|-------------|
| `toggle()` | Open/close chat panel |
| `appendMessage(role, text)` | Add a chat bubble |
| `send(prefill)` | Send message to `/api/chat` |

**Depends on:** Nothing.

### 7. `App` — Initialization & Wiring (~40 lines)

**Owns:** The `setInterval` timer reference.

**Methods:**
| Method | Description |
|--------|-------------|
| `init()` | Called on load. Starts theme, first fetch, timer |
| `refresh()` | Manual refresh (button click) |
| `renderNow()` | Captures snapshot, computes dirty flags, schedules render via rAF |
| `onTick()` | Decrement countdown, trigger fetch at 0 |

**`renderNow()` implementation:**
```js
renderNow() {
  const snap = State.snapshot();
  const flags = DirtyChecker.diff(snap);
  requestAnimationFrame(() => {
    Renderer.render(snap, flags);
    State.commitPrev(snap);  // INSIDE rAF — prevents race with next fetch
  });
}
```

**Depends on:** All other modules.

---

## Utility Functions (top-level, ~20 lines)

Keep these as plain top-level functions (they're used everywhere):

- `$(id)` — `document.getElementById`
- `esc(s)` — HTML escape
- `safeColor(v)` — validate hex color
- `relTime(ts)` — relative timestamp
- `COLORS` — palette constant

---

## Inline `onclick` Handler Strategy

**Current:** `onclick="uTab='today';render()"` — references globals.

**After:** Expose a thin `OCUI` namespace on `window` for HTML bindings:

```js
// At the end of <script>, expose for inline handlers
window.OCUI = {
  setUsageTab:    v => { State.setTab('usage', v); App.renderNow(); },
  setSubRunsTab:  v => { State.setTab('subRuns', v); App.renderNow(); },
  setSubTokensTab:v => { State.setTab('subTokens', v); App.renderNow(); },
  setChartDays:   n => { State.setChartDays(n); App.renderNow(); },
  refresh:        () => App.refresh(),
  toggleTheme:    () => Theme.toggle(),
  applyTheme:     id => Theme.apply(id),
};
```

HTML becomes: `onclick="OCUI.setUsageTab('today')"` — clean, traceable, no globals.

---

## Migration Notes for Codex

### Step-by-step (do in order):

1. **Create `State` object.** Move `D` → `State.data`, `prevD` → `State.prev`, all tab variables → `State.tabs`, `timer` → `State.countdown`. Delete the old globals.

2. **Create `DataLayer` object.** Extract the fetch logic from `loadData()`. It should return data, not set globals.

3. **Create `DirtyChecker` object.** Move `sectionChanged()` and `stableSnapshot()` here. Add `computeDirtyFlags()` that returns all dirty booleans.

4. **Create `Renderer` object.** Split the monolithic `render()` into section functions. Each section function should:
   - Accept no arguments (reads from `State` directly)
   - Only be called when its dirty flag is true (except always-update sections like header/health)
   - `Renderer.render(snap, dirtyFlags)` receives pre-computed flags from `App.renderNow()` and dispatches

5. **Create `Theme` object.** Rename `THEMES` → `Theme.themes`, `currentTheme` → `Theme.current`. Move `loadThemes()`, `applyTheme()`, `renderThemeMenu()`, `toggleThemeMenu()`.

6. **Create `App` object.** Wire `init()` to call `Theme.load()`, `DataLayer.fetch()`, start `setInterval`. Wire `refresh()` for button.

7. **Create `window.OCUI` namespace.** Update all inline `onclick` handlers in HTML.

8. **Delete all loose globals** — nothing should remain outside the module objects except utilities (`$`, `esc`, `safeColor`, `relTime`, `COLORS`).

---

## Non-Functional Guarantees

### Scroll Preservation
`renderCrons()` and `renderSessions()` save/restore `scrollTop` before/after `innerHTML` replacement using `closest('[style*="overflow"]')` container detection.

### requestAnimationFrame Batching
All renders triggered by `App.renderNow()` (auto-refresh path) are wrapped in `requestAnimationFrame`. Tab changes and manual refresh call `App.renderNow()` which also uses rAF.

### Error Handling
If `DataLayer.fetch()` fails: `State.data` is NOT updated (stale data stays displayed). `App` catches the error, logs it, and resets the countdown without calling `Renderer.render()`. Future: add visual stale-data indicator.

### Out-of-Order Fetch Protection
If two fetches race (manual refresh + timer overlap), the second fetch result is discarded if a newer fetch is already in flight. Use a `DataLayer._reqId` counter: increment on each fetch, ignore responses where `reqId !== DataLayer._reqId`.

### Renames:
| Old | New |
|-----|-----|
| `D` | `State.data` |
| `prevD` | `State.prev` |
| `uTab` | `State.tabs.usage` |
| `srTab` | `State.tabs.subRuns` |
| `stTab` | `State.tabs.subTokens` |
| `prevUTab` | `State.prevTabs.usage` |
| `prevSrTab` | `State.prevTabs.subRuns` |
| `prevStTab` | `State.prevTabs.subTokens` |
| `chartDays` | `State.chartDays` |
| `prevChartDays` | `State.prevChartDays` |
| `timer` | `State.countdown` |
| `THEMES` | `Theme.themes` |
| `currentTheme` | `Theme.current` |
| `loadData()` | `App.refresh()` → `DataLayer.fetch()` |
| `render()` | `Renderer.render()` |
| `renderCharts()` | `Renderer.renderCharts()` |
| `renderAgentTree()` | `Renderer.renderAgentTree()` |

---

## Line Count Estimate

| Module | Lines |
|--------|-------|
| Utilities (`$`, `esc`, etc.) | ~20 |
| `State` | ~40 |
| `DataLayer` | ~25 |
| `DirtyChecker` | ~50 |
| `Theme` | ~80 |
| `Renderer` (all sections) | ~480 |
| `App` | ~35 |
| `window.OCUI` | ~15 |
| **Total JS** | **~745** |
| CSS (unchanged) | ~250 |
| HTML (unchanged) | ~200 |
| **Total file** | **~1195** |

Current total: ~1200 lines. Refactored: similar or slightly less.

---

## What Does NOT Change

- All CSS stays identical
- All HTML structure stays identical (only `onclick` values change)
- All visual behavior stays identical
- Theme engine logic stays the same (just reorganized)
- Chart SVG rendering stays the same (just moved into `Renderer`)
- `stableSnapshot` / dirty-check logic stays the same (just moved into `DirtyChecker`)
