"""Frontend static analysis tests — AC15-AC24.

Uses only re and string operations to validate HTML/JS/shell patterns.
No browser or JS runtime needed.
"""

import os
import re
import unittest

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
INDEX_HTML = os.path.join(REPO, "index.html")
SERVER_PY = os.path.join(REPO, "server.py")
REFRESH_SH = os.path.join(REPO, "refresh.sh")


def read(path):
    with open(path) as f:
        return f.read()


class TestFrontendJS(unittest.TestCase):
    """AC15-AC20: JavaScript function/pattern checks in index.html."""

    @classmethod
    def setUpClass(cls):
        cls.html = read(INDEX_HTML)

    def test_ac15_esc_defined_and_used(self):
        """AC15: esc() function is defined and used on innerHTML positions."""
        # esc must be defined
        self.assertRegex(self.html, r'\besc\s*=\s*', "esc() not defined")
        # esc must be called in template literals (at least several times)
        esc_calls = re.findall(r'\$\{esc\(', self.html)
        self.assertGreater(len(esc_calls), 5, f"esc() called only {len(esc_calls)} times — expected widespread use")

    def test_ac16_safe_color_defined_with_hex_regex(self):
        """AC16: safeColor() defined with hex regex validation."""
        self.assertIn("function safeColor", self.html)
        # Should contain a hex color regex pattern
        self.assertTrue(
            re.search(r'safeColor.*?#\[0-9a-fA-F\]', self.html, re.DOTALL),
            "safeColor missing hex regex"
        )

    def test_ac17_section_changed_uses_state_prev(self):
        """AC17: sectionChanged() is defined in DirtyChecker and uses State.prev."""
        self.assertIn("sectionChanged(keys)", self.html)
        match = re.search(r'sectionChanged\(keys\).*?\{.*?State\.prev', self.html, re.DOTALL)
        self.assertIsNotNone(match, "sectionChanged doesn't reference State.prev")

    def test_ac18_prev_tab_state(self):
        """AC18: State.prevTabs tracks previous tab values."""
        self.assertIn("prevTabs", self.html, "Missing State.prevTabs")
        for tab in ("usage", "subRuns", "subTokens"):
            self.assertIn(f"prevTabs.{tab}", self.html, f"Missing prevTabs.{tab}")

    def test_ac19_request_animation_frame_in_render_now(self):
        """AC19: requestAnimationFrame is used in App.renderNow()."""
        match = re.search(r'renderNow\s*\(\)[^}]*requestAnimationFrame', self.html, re.DOTALL)
        self.assertIsNotNone(match, "requestAnimationFrame not found in renderNow()")

    def test_ac20_snapshot_deep_clones_state(self):
        """AC20: State.snapshot() uses JSON.parse(JSON.stringify(...)) for deep clone."""
        self.assertIn("JSON.parse(JSON.stringify(", self.html)
        # commitPrev receives frozen snapshot — prev is never a live reference
        self.assertIn("commitPrev(snap)", self.html)

    def test_channels_dynamic_render_from_payload_keys(self):
        """Channel cards render dynamically from channelStatus keys + channels array."""
        self.assertIn("const channelStatusMap = AC.channelStatus", self.html)
        self.assertIn("...Object.keys(channelStatusMap)", self.html)
        self.assertIn("...((AC.channels||[])", self.html)
        self.assertIn("$('channelConfigPanel').innerHTML", self.html)

    def test_channels_supports_slack_discord_unknown_generically(self):
        """No hardcoded channel whitelist (slack/discord/unknown are generic keys)."""
        self.assertIn("const channelCards = channelKeys.map((key)=>", self.html)
        self.assertIn("esc(key)", self.html)
        self.assertIn("esc(errorText)", self.html)


    def test_ac25_reconcile_rows_defined(self):
        """AC25: Renderer.reconcileRows() is defined for keyed DOM updates."""
        self.assertIn("reconcileRows(", self.html)
        self.assertIn("dataset.key", self.html)
        self.assertIn("createDocumentFragment", self.html)

    def test_ac26_svg_cache_skip(self):
        """AC26: Renderer caches SVG content to skip identical re-renders."""
        self.assertIn("_svgCache", self.html)
        self.assertIn("patchSvg(", self.html)


class TestRefreshShSafety(unittest.TestCase):
    """AC21-AC22, AC24: refresh.sh safety checks."""

    @classmethod
    def setUpClass(cls):
        cls.sh = read(REFRESH_SH)

    def test_ac21_no_shell_true_in_python(self):
        """AC21: No shell=True in embedded Python inside refresh.sh."""
        # Extract Python heredoc
        match = re.search(r"<<\s*'?PYEOF'?(.*?)PYEOF", self.sh, re.DOTALL)
        self.assertIsNotNone(match, "Python heredoc not found")
        python_code = match.group(1)
        self.assertNotIn("shell=True", python_code, "shell=True found in embedded Python")

    def test_ac22_set_euo_pipefail(self):
        """AC22: set -euo pipefail is in refresh.sh."""
        self.assertIn("set -euo pipefail", self.sh)

    def test_ac24_json_load_uses_context_manager(self):
        """AC24: Python code uses 'with open' for json.load, not bare open()."""
        match = re.search(r"<<\s*'?PYEOF'?(.*?)PYEOF", self.sh, re.DOTALL)
        python_code = match.group(1)
        # Check for bare json.load(open(...)) without with statement
        bare_opens = re.findall(r'json\.load\(open\(', python_code)
        if bare_opens:
            self.fail(f"Found {len(bare_opens)} bare json.load(open(...)) without context manager")

    def test_ac30_negative_costs_are_clamped_in_refresh(self):
        """AC30: refresh.sh clamps negative usage.cost.total to 0.0."""
        self.assertIn("cost_total = max(0.0, raw_cost_total)", self.sh)



class TestServerSafety(unittest.TestCase):
    """AC23: server.py safety checks."""

    @classmethod
    def setUpClass(cls):
        cls.server = read(SERVER_PY)

    def test_ac23_no_cors_wildcard(self):
        """AC23: CORS wildcard Access-Control-Allow-Origin: * is NOT in server.py."""
        # Check there's no literal wildcard CORS
        wildcard_patterns = re.findall(r'''Access-Control-Allow-Origin['"]\s*,\s*['"]\*['"]''', self.server)
        self.assertEqual(len(wildcard_patterns), 0, "Found CORS wildcard * in server.py")
        # Also check no "*, " pattern
        self.assertNotIn('"*"', self.server.split("Access-Control-Allow-Origin")[-1][:50] if "Access-Control-Allow-Origin" in self.server else "")


if __name__ == "__main__":
    unittest.main()
