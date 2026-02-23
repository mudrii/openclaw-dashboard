"""Tests for the 'recent finished' buffer in the agent hierarchy.

Static analysis tests (regex over index.html JS) — no browser runtime needed.
Verifies:
  - Recent finished nodes appear within the 5-minute window.
  - Nodes older than the window are pruned and not shown.
  - Live nodes still render with 'live' badge.
  - The recent badge is visually distinct from 'live'.
"""

import os
import re
import unittest

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
INDEX_HTML = os.path.join(REPO, "index.html")


class TestHierarchyRecent(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        with open(INDEX_HTML, encoding="utf-8") as f:
            cls.html = f.read()
        m = re.search(r"<script>([\s\S]*)</script>", cls.html)
        assert m, "<script> block not found"
        cls.js = m.group(1)

    # ── 1. Buffer infrastructure ──────────────────────────────────────────────

    def test_recent_window_constant_defined(self):
        self.assertIn("RECENT_WINDOW_MS", self.js,
            "RECENT_WINDOW_MS constant not found in JS")
        literal_ok = bool(re.search(r"RECENT_WINDOW_MS\s*=\s*300000\b", self.js))
        expr_ok = bool(re.search(
            r"RECENT_WINDOW_MS\s*=\s*5\s*\*\s*60\s*\*\s*1000", self.js))
        self.assertTrue(literal_ok or expr_ok,
            "RECENT_WINDOW_MS must equal 300000 ms (5 min) — "
            "expected either '300000' or '5 * 60 * 1000'")

    def test_recent_finished_map_declared(self):
        self.assertIn("_recentFinished", self.js,
            "_recentFinished buffer not declared")

    def test_prev_active_keys_set_declared(self):
        self.assertIn("_prevActiveKeys", self.js,
            "_prevActiveKeys Set not declared")

    # ── 2. Pruning logic ──────────────────────────────────────────────────────

    def test_prune_uses_window_constant(self):
        self.assertIsNotNone(re.search(r"RECENT_WINDOW_MS", self.js),
            "RECENT_WINDOW_MS not referenced")
        self.assertIsNotNone(re.search(r"delete\s+_recentFinished\[", self.js),
            "Pruning delete statement not found")

    def test_prune_removes_stale_entries(self):
        pattern = (r"now\s*-\s*_recentFinished\[.*?\]\s*\.finishedAt"
                   r"\s*>\s*RECENT_WINDOW_MS")
        self.assertIsNotNone(re.search(pattern, self.js),
            "Expected 'now - _recentFinished[k].finishedAt > RECENT_WINDOW_MS' "
            "comparison not found")

    # ── 3. Recent nodes appear within buffer window ───────────────────────────

    def test_recent_sessions_merged_into_render(self):
        self.assertIsNotNone(
            re.search(r"const\s+recentSessions\s*=", self.js),
            "recentSessions variable not constructed")
        self.assertIsNotNone(
            re.search(r"\.\.\.(activeSessions|recentSessions)", self.js),
            "Active + recent sessions not spread-merged")

    def test_recent_flag_set_on_finished_sessions(self):
        self.assertTrue(
            "_recent:true" in self.js or "_recent: true" in self.js,
            "_recent:true flag not set on recently finished sessions")

    def test_recent_badge_rendered_for_recent_sessions(self):
        self.assertIn("s._recent", self.js,
            "s._recent not checked in nodeCard/render")
        self.assertIsNotNone(re.search(r"tree-badge recent", self.js),
            "tree-badge recent class not used")
        self.assertIsNotNone(re.search(r"✓\s*recent", self.js),
            "✓ recent text not found in badge")

    # ── 4. Live nodes still render as 'live' (no regression) ─────────────────

    def test_live_badge_still_rendered_for_active_sessions(self):
        self.assertIn("tree-badge live", self.js,
            "tree-badge live class missing")
        self.assertIn("● live", self.js,
            "● live text missing from nodeCard")

    def test_live_and_recent_are_distinct_conditions(self):
        self.assertIsNotNone(re.search(r"s\.active", self.js),
            "s.active condition missing")
        self.assertIsNotNone(re.search(r"s\._recent", self.js),
            "s._recent condition missing")
        self.assertIn("tree-badge live", self.js)
        self.assertIn("tree-badge recent", self.js)

    # ── 5. CSS — recent badge is visually distinct ────────────────────────────

    def test_recent_badge_css_defined(self):
        self.assertIn(".tree-badge.recent", self.html,
            ".tree-badge.recent CSS rule missing")
        m = re.search(r"\.tree-badge\.recent\s*\{([^}]+)\}", self.html)
        self.assertIsNotNone(m, ".tree-badge.recent rule body not found")
        rule = m.group(1)
        self.assertIn("color", rule,
            ".tree-badge.recent must set a color")
        self.assertNotIn("var(--green)", rule,
            ".tree-badge.recent must be visually distinct from .tree-badge.live")

    def test_recent_badge_css_has_border(self):
        m = re.search(r"\.tree-badge\.recent\s*\{([^}]+)\}", self.html)
        self.assertIsNotNone(m, ".tree-badge.recent rule body not found")
        self.assertIn("border", m.group(1),
            ".tree-badge.recent should have a border style")

    # ── 6. XSS safety ────────────────────────────────────────────────────────

    def test_recent_buffer_preserves_esc_usage(self):
        m = re.search(r"function nodeCard\(s,col\)\{([\s\S]*?)\n  \}", self.js)
        if not m:
            m = re.search(r"function nodeCard\(s,col\)\s*\{([\s\S]*?)\}", self.js)
        self.assertIsNotNone(m, "nodeCard function not found")
        self.assertIn("esc(", m.group(1), "esc() not used inside nodeCard()")


if __name__ == "__main__":
    unittest.main()
