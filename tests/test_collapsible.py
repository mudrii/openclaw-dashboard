"""Collapsible sections tests — ATDD/TDD approach.

Tests cover:
  - HTML structure (section wrappers, toggle buttons, body containers)
  - CSS rules for collapsed state
  - FOUC bootstrap script in <head>
  - Sections JS module (init, toggle, expand/collapse all, persistence)
  - Accessibility (aria-expanded, aria-controls, keyboard)
  - localStorage fallback
  - Agent-config default collapsed
  - System topbar non-collapsible

Uses static analysis on index.html (no browser needed for most tests).
"""

import os
import re
import unittest

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
INDEX_HTML = os.path.join(REPO, "index.html")


def read(path):
    with open(path) as f:
        return f.read()


# ─── Section inventory (from final-v3 plan) ───
SECTION_KEYS = [
    "alerts", "health", "cost", "charts",
    "crons", "sessions", "usage", "subagent", "agent-config",
]


class TestSectionStructure(unittest.TestCase):
    """Each collapsible section has proper wrapper, toggle button, and body."""

    @classmethod
    def setUpClass(cls):
        cls.html = read(INDEX_HTML)

    def test_each_section_has_wrapper(self):
        """Each section key has a <section class='oc-section' data-section='KEY'>."""
        for key in SECTION_KEYS:
            pattern = rf'<section[^>]*class="[^"]*oc-section[^"]*"[^>]*data-section="{re.escape(key)}"'
            self.assertRegex(
                self.html, pattern,
                f"Missing oc-section wrapper for '{key}'"
            )

    def test_each_section_has_toggle_button(self):
        """Each section has a <button class='oc-section-toggle'> inside its header."""
        for key in SECTION_KEYS:
            # Find the section block
            pattern = rf'data-section="{re.escape(key)}".*?<button[^>]*class="[^"]*oc-section-toggle[^"]*"'
            match = re.search(pattern, self.html, re.DOTALL)
            self.assertIsNotNone(
                match,
                f"Missing oc-section-toggle button for '{key}'"
            )

    def test_each_section_has_body_container(self):
        """Each section has a <div class='oc-section-body' id='oc-body-KEY'>."""
        for key in SECTION_KEYS:
            body_id = f"oc-body-{key}"
            pattern = rf'<div[^>]*class="[^"]*oc-section-body[^"]*"[^>]*id="{re.escape(body_id)}"'
            self.assertRegex(
                self.html, pattern,
                f"Missing oc-section-body with id '{body_id}'"
            )

    def test_toggle_button_has_aria_expanded(self):
        """Each toggle button has aria-expanded attribute."""
        for key in SECTION_KEYS:
            pattern = rf'data-section="{re.escape(key)}".*?<button[^>]*aria-expanded="(true|false)"'
            match = re.search(pattern, self.html, re.DOTALL)
            self.assertIsNotNone(
                match,
                f"Toggle button for '{key}' missing aria-expanded"
            )

    def test_toggle_button_has_aria_controls(self):
        """Each toggle button has aria-controls pointing to the body id."""
        for key in SECTION_KEYS:
            body_id = f"oc-body-{key}"
            pattern = rf'data-section="{re.escape(key)}".*?<button[^>]*aria-controls="{re.escape(body_id)}"'
            match = re.search(pattern, self.html, re.DOTALL)
            self.assertIsNotNone(
                match,
                f"Toggle button for '{key}' missing aria-controls='{body_id}'"
            )

    def test_system_topbar_not_collapsible(self):
        """System topbar must NOT be wrapped in oc-section."""
        # systemTopBar should not be inside an oc-section with data-section
        self.assertNotRegex(
            self.html,
            r'data-section="system-topbar"',
            "System topbar should NOT be collapsible"
        )


class TestGlobalControls(unittest.TestCase):
    """Expand All / Collapse All controls exist."""

    @classmethod
    def setUpClass(cls):
        cls.html = read(INDEX_HTML)

    def test_expand_all_button_exists(self):
        """There is an Expand All button."""
        self.assertRegex(
            self.html,
            r'id="oc-expand-all"',
            "Missing Expand All button (#oc-expand-all)"
        )

    def test_collapse_all_button_exists(self):
        """There is a Collapse All button."""
        self.assertRegex(
            self.html,
            r'id="oc-collapse-all"',
            "Missing Collapse All button (#oc-collapse-all)"
        )


class TestCSS(unittest.TestCase):
    """CSS rules for collapsible sections."""

    @classmethod
    def setUpClass(cls):
        cls.html = read(INDEX_HTML)

    def test_collapsed_body_hidden(self):
        """CSS rule: .oc-section.collapsed .oc-section-body { display:none }."""
        pattern = r'\.oc-section\.collapsed\s+\.oc-section-body\s*\{\s*display\s*:\s*none'
        self.assertRegex(
            self.html, pattern,
            "Missing CSS rule for hiding collapsed section body"
        )

    def test_chevron_class_exists(self):
        """CSS defines .oc-chevron for toggle indicator."""
        self.assertIn(".oc-chevron", self.html)

    def test_focus_visible_on_toggle(self):
        """Focus-visible style exists for toggle button."""
        self.assertRegex(
            self.html,
            r'\.oc-section-toggle.*?:focus-visible',
            "Missing focus-visible styles for toggle button"
        )

    def test_reduced_motion_media_query(self):
        """@media (prefers-reduced-motion: reduce) disables chevron transition."""
        self.assertIn("prefers-reduced-motion", self.html)


class TestBootstrapScript(unittest.TestCase):
    """FOUC prevention bootstrap script in <head>."""

    @classmethod
    def setUpClass(cls):
        cls.html = read(INDEX_HTML)

    def test_bootstrap_script_in_head(self):
        """Bootstrap script exists inside <head> before </head>."""
        head_match = re.search(r'<head>(.*?)</head>', self.html, re.DOTALL)
        self.assertIsNotNone(head_match, "No <head> found")
        head_content = head_match.group(1)
        self.assertIn("oc-collapse-bootstrap", head_content,
                       "Bootstrap script not found in <head>")

    def test_bootstrap_reads_localstorage(self):
        """Bootstrap script reads localStorage with try/catch."""
        head_match = re.search(r'<head>(.*?)</head>', self.html, re.DOTALL)
        head_content = head_match.group(1)
        self.assertIn("localStorage", head_content)
        self.assertIn("try", head_content)
        self.assertIn("catch", head_content)

    def test_bootstrap_injects_style_tag(self):
        """Bootstrap creates a <style id='oc-collapse-bootstrap'>."""
        head_match = re.search(r'<head>(.*?)</head>', self.html, re.DOTALL)
        head_content = head_match.group(1)
        self.assertIn("oc-collapse-bootstrap", head_content)

    def test_bootstrap_sets_window_init_state(self):
        """Bootstrap stores merged state on window.__ocCollapsedInit."""
        head_match = re.search(r'<head>(.*?)</head>', self.html, re.DOTALL)
        head_content = head_match.group(1)
        self.assertIn("__ocCollapsedInit", head_content)

    def test_bootstrap_default_agent_config_collapsed(self):
        """Bootstrap defaults agent-config to collapsed (true)."""
        head_match = re.search(r'<head>(.*?)</head>', self.html, re.DOTALL)
        head_content = head_match.group(1)
        # Should have agent-config defaulting to true
        self.assertRegex(
            head_content,
            r'["\']agent-config["\']\s*:\s*true',
            "agent-config should default to collapsed (true)"
        )


class TestSectionsModule(unittest.TestCase):
    """Sections JS module exists with required functions."""

    @classmethod
    def setUpClass(cls):
        cls.html = read(INDEX_HTML)

    def test_section_keys_constant_defined(self):
        """SECTION_KEYS constant is defined in JS."""
        self.assertIn("SECTION_KEYS", self.html)

    def test_section_keys_contains_all_keys(self):
        """SECTION_KEYS contains all 9 section keys."""
        for key in SECTION_KEYS:
            self.assertRegex(
                self.html,
                rf'["\']{ re.escape(key) }["\']',
                f"Section key '{key}' not found in JS"
            )

    def test_sections_module_has_init(self):
        """Sections module has init() function."""
        self.assertRegex(self.html, r'Sections\s*=\s*\{', "No Sections object")
        self.assertIn("Sections.init()", self.html)

    def test_sections_module_has_toggle(self):
        """Sections module has toggle() function."""
        self.assertRegex(self.html, r'toggle\s*\(\s*key\s*\)', "No toggle(key) in Sections")

    def test_sections_module_has_set_all(self):
        """Sections module has setAll() function."""
        self.assertRegex(self.html, r'setAll\s*\(\s*collapsed\s*\)', "No setAll(collapsed)")

    def test_sections_module_has_load_state(self):
        """Sections module has loadState() function."""
        self.assertIn("loadState", self.html)

    def test_sections_module_has_save_state(self):
        """Sections module has saveState() function."""
        self.assertIn("saveState", self.html)

    def test_sections_init_called_in_app_init(self):
        """Sections.init() is called inside App.init()."""
        # Find App.init() body and check for Sections.init()
        pattern = r'App\s*=\s*\{[^}]*init\s*\(\s*\)\s*\{[^}]*Sections\.init\(\)'
        match = re.search(pattern, self.html, re.DOTALL)
        self.assertIsNotNone(match, "Sections.init() not called in App.init()")

    def test_storage_key_defined(self):
        """Storage key 'ocDash:collapse:v1' is used."""
        self.assertIn("ocDash:collapse:v1", self.html)

    def test_localstorage_try_catch(self):
        """localStorage access wrapped in try/catch for private mode fallback."""
        # At least the Sections module should have try/catch around localStorage
        # Count try blocks that mention localStorage
        try_blocks = re.findall(r'try\s*\{[^}]*localStorage', self.html)
        self.assertGreaterEqual(
            len(try_blocks), 2,
            "Need at least 2 try/catch blocks around localStorage (bootstrap + module)"
        )


class TestAccessibility(unittest.TestCase):
    """Accessibility requirements."""

    @classmethod
    def setUpClass(cls):
        cls.html = read(INDEX_HTML)

    def test_toggle_is_button_element(self):
        """Toggle controls are <button> elements (not divs/spans)."""
        toggles = re.findall(r'<button[^>]*class="[^"]*oc-section-toggle', self.html)
        self.assertEqual(
            len(toggles), len(SECTION_KEYS),
            f"Expected {len(SECTION_KEYS)} toggle buttons, found {len(toggles)}"
        )

    def test_toggle_buttons_have_aria_label(self):
        """Toggle buttons have aria-label or visible text for screen readers."""
        for key in SECTION_KEYS:
            pattern = rf'data-section="{re.escape(key)}".*?<button[^>]*oc-section-toggle[^>]*>'
            match = re.search(pattern, self.html, re.DOTALL)
            self.assertIsNotNone(match, f"No toggle button found for {key}")
            btn_tag = match.group(0)
            # Must have aria-label or contain visible text
            has_aria = "aria-label" in btn_tag
            # Buttons with chevrons are fine if they have aria-label
            self.assertTrue(has_aria or "Toggle" in btn_tag,
                           f"Toggle for '{key}' lacks aria-label")


class TestNoRegression(unittest.TestCase):
    """Verify existing elements still exist (no accidental deletion)."""

    @classmethod
    def setUpClass(cls):
        cls.html = read(INDEX_HTML)

    def test_system_topbar_still_exists(self):
        self.assertIn('id="systemTopBar"', self.html)

    def test_alerts_section_still_exists(self):
        self.assertIn('id="alertsSection"', self.html)

    def test_health_row_still_exists(self):
        self.assertIn('id="healthRow"', self.html)

    def test_cron_table_still_exists(self):
        self.assertIn('id="cronTable"', self.html)

    def test_sessions_body_still_exists(self):
        self.assertIn('id="sessBody"', self.html)

    def test_usage_body_still_exists(self):
        self.assertIn('id="uBody"', self.html)

    def test_subagent_runs_body_still_exists(self):
        self.assertIn('id="srBody"', self.html)

    def test_agent_table_still_exists(self):
        self.assertIn('id="agentTable"', self.html)

    def test_charts_container_still_exists(self):
        self.assertIn('id="chartsContainer"', self.html)

    def test_models_grid_still_exists(self):
        self.assertIn('id="modelsGrid"', self.html)

    def test_skills_grid_still_exists(self):
        self.assertIn('id="skillsGrid"', self.html)

    def test_git_panel_still_exists(self):
        self.assertIn('id="gitPanel"', self.html)

    def test_chat_panel_still_exists(self):
        self.assertIn('id="chatPanel"', self.html)


if __name__ == "__main__":
    unittest.main()
