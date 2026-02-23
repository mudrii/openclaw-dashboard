"""Tests for the 'recent finished' buffer in the agent hierarchy.

These are static analysis tests (regex over index.html JS) — no browser runtime needed.
They verify:
  - Recent finished nodes can appear within the 5-minute window.
  - Nodes older than the window are pruned and not shown.
  - Live nodes still render with 'live' badge.
  - The recent badge is visually distinct from 'live'.
"""

import os
import re
import pytest

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
INDEX_HTML = os.path.join(REPO, "index.html")


def read_html():
    with open(INDEX_HTML, encoding="utf-8") as f:
        return f.read()


def extract_script(html):
    m = re.search(r"<script>([\s\S]*)</script>", html)
    assert m, "<script> block not found"
    return m.group(1)


@pytest.fixture(scope="module")
def html():
    return read_html()


@pytest.fixture(scope="module")
def js(html):
    return extract_script(html)


# ──────────────────────────────────────────────────────────────────────────────
# 1. Buffer infrastructure exists
# ──────────────────────────────────────────────────────────────────────────────


def test_recent_window_constant_defined(js):
    """RECENT_WINDOW_MS must be defined as 5-minute window (300000 ms or equivalent expression)."""
    # Accept either literal 300000 or the expression form 5 * 60 * 1000
    assert "RECENT_WINDOW_MS" in js, "RECENT_WINDOW_MS constant not found in JS"
    # Check for the literal value OR the idiomatic minutes expression
    literal_ok = bool(re.search(r"RECENT_WINDOW_MS\s*=\s*300000\b", js))
    expr_ok = bool(re.search(r"RECENT_WINDOW_MS\s*=\s*5\s*\*\s*60\s*\*\s*1000", js))
    assert literal_ok or expr_ok, (
        "RECENT_WINDOW_MS must equal 300000 ms (5 min) — "
        "expected either '300000' or '5 * 60 * 1000'"
    )


def test_recent_finished_map_declared(js):
    """_recentFinished buffer object must be declared."""
    assert "_recentFinished" in js, "_recentFinished buffer not declared"


def test_prev_active_keys_set_declared(js):
    """_prevActiveKeys tracking Set must be declared."""
    assert "_prevActiveKeys" in js, "_prevActiveKeys Set not declared"


# ──────────────────────────────────────────────────────────────────────────────
# 2. Pruning logic — nodes older than window are removed
# ──────────────────────────────────────────────────────────────────────────────


def test_prune_uses_window_constant(js):
    """Pruning logic must reference RECENT_WINDOW_MS for cutoff."""
    # Should compare finishedAt against now and RECENT_WINDOW_MS
    assert re.search(r"RECENT_WINDOW_MS", js), "RECENT_WINDOW_MS not referenced"
    # Should delete old entries
    assert re.search(r"delete\s+_recentFinished\[", js), "Pruning delete statement not found"


def test_prune_removes_stale_entries(js):
    """Pruning block must compare now - finishedAt > RECENT_WINDOW_MS."""
    pattern = r"now\s*-\s*_recentFinished\[.*?\]\s*\.finishedAt\s*>\s*RECENT_WINDOW_MS"
    assert re.search(pattern, js), (
        "Expected 'now - _recentFinished[k].finishedAt > RECENT_WINDOW_MS' comparison not found"
    )


# ──────────────────────────────────────────────────────────────────────────────
# 3. Recent nodes appear within the buffer window
# ──────────────────────────────────────────────────────────────────────────────


def test_recent_sessions_merged_into_render(js):
    """Recently finished sessions must be merged with active sessions before rendering."""
    # recentSessions array should be built from _recentFinished and spread into sessions
    assert re.search(r"const\s+recentSessions\s*=", js), "recentSessions variable not constructed"
    assert re.search(r"\.\.\.(activeSessions|recentSessions)", js), (
        "Active + recent sessions not spread-merged"
    )


def test_recent_flag_set_on_finished_sessions(js):
    """Finished sessions added to buffer must be marked _recent:true."""
    assert "_recent:true" in js or "_recent: true" in js, (
        "_recent:true flag not set on recently finished sessions"
    )


def test_recent_badge_rendered_for_recent_sessions(js):
    """nodeCard() must render a 'recent' badge when s._recent is true."""
    assert "s._recent" in js, "s._recent not checked in nodeCard/render"
    assert "recent" in js, "'recent' badge class/text not found"
    # Should have the badge class 'recent' in the template
    assert re.search(r"tree-badge recent", js), "tree-badge recent class not used"
    assert re.search(r"✓\s*recent", js), "✓ recent text not found in badge"


# ──────────────────────────────────────────────────────────────────────────────
# 4. Live nodes still render as 'live' (no regression)
# ──────────────────────────────────────────────────────────────────────────────


def test_live_badge_still_rendered_for_active_sessions(js):
    """Active sessions must still render with '● live' badge."""
    assert "tree-badge live" in js, "tree-badge live class missing"
    assert "● live" in js, "● live text missing from nodeCard"


def test_live_and_recent_are_distinct_conditions(js):
    """live and recent badges must be rendered via distinct conditions."""
    # active → live, _recent → recent, otherwise → idle
    # Expect a ternary or if-else chain covering s.active, s._recent
    assert re.search(r"s\.active", js), "s.active condition missing"
    assert re.search(r"s\._recent", js), "s._recent condition missing"
    # Both badge class names must exist separately
    assert "tree-badge live" in js and "tree-badge recent" in js, (
        "Both 'live' and 'recent' badge classes must be present"
    )


# ──────────────────────────────────────────────────────────────────────────────
# 5. CSS — recent badge is visually distinct
# ──────────────────────────────────────────────────────────────────────────────


def test_recent_badge_css_defined(html):
    """CSS for .tree-badge.recent must exist with a distinct color."""
    assert ".tree-badge.recent" in html, ".tree-badge.recent CSS rule missing"
    # Should have a different color from 'live' (green) — expect yellow or similar
    m = re.search(r"\.tree-badge\.recent\s*\{([^}]+)\}", html)
    assert m, ".tree-badge.recent rule body not found"
    rule = m.group(1)
    # Must set a color (not inherit the green of .live)
    assert "color" in rule, ".tree-badge.recent must set a color"
    # Should not use the same green as .live
    assert "var(--green)" not in rule, (
        ".tree-badge.recent must be visually distinct from .tree-badge.live (not green)"
    )


def test_recent_badge_css_has_border(html):
    """CSS for .tree-badge.recent should define a border for visual distinction."""
    m = re.search(r"\.tree-badge\.recent\s*\{([^}]+)\}", html)
    assert m, ".tree-badge.recent rule body not found"
    rule = m.group(1)
    assert "border" in rule, ".tree-badge.recent should have a border style"


# ──────────────────────────────────────────────────────────────────────────────
# 6. XSS safety — no raw interpolation added by the buffer code
# ──────────────────────────────────────────────────────────────────────────────


def test_recent_buffer_preserves_esc_usage(js):
    """nodeCard() must still use esc() for name, model, trigger interpolations."""
    # Find the nodeCard function body
    m = re.search(r"function nodeCard\(s,col\)\{([\s\S]*?)\n  \}", js)
    if not m:
        # Try alternate whitespace
        m = re.search(r"function nodeCard\(s,col\)\s*\{([\s\S]*?)\}", js)
    assert m, "nodeCard function not found"
    body = m.group(1)
    # name, model, trigger must all be escaped
    assert "esc(" in body, "esc() not used inside nodeCard()"
