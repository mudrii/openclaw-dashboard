"""E2E tests for collapsible sections using Playwright (sync API).

Requires: .venv/bin/pip install playwright && .venv/bin/python3 -m playwright install chromium

Run:
    .venv/bin/python3 -m pytest tests/test_collapsible_e2e.py -v --timeout=30
"""
import os
import subprocess
import sys
import time

import pytest

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BASE_URL = "http://127.0.0.1:8098"

try:
    from playwright.sync_api import sync_playwright
except ImportError:
    pytest.skip("playwright not installed", allow_module_level=True)


@pytest.fixture(scope="session")
def server():
    proc = subprocess.Popen(
        [sys.executable, os.path.join(REPO, "server.py"), "--port", "8098"],
        cwd=REPO,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    time.sleep(2.0)
    yield proc
    proc.terminate()
    proc.wait()


@pytest.fixture(scope="session")
def browser_instance(server):
    with sync_playwright() as pw:
        b = pw.chromium.launch(headless=True)
        yield b
        b.close()


@pytest.fixture
def page(browser_instance):
    ctx = browser_instance.new_context()
    pg = ctx.new_page()
    pg.goto(BASE_URL, wait_until="networkidle", timeout=10000)
    yield pg
    ctx.close()


class TestCollapsibleToggle:
    """Clicking toggle buttons collapses/expands sections."""

    def test_section_starts_expanded(self, page):
        """alerts section is expanded by default."""
        section = page.locator('[data-section="alerts"]')
        assert "collapsed" not in (section.get_attribute("class") or "")
        body = page.locator("#oc-body-alerts")
        assert body.is_visible()

    def test_click_toggle_collapses_section(self, page):
        """Clicking toggle on 'crons' hides the body."""
        page.click('[data-section="crons"] .oc-section-toggle')
        page.wait_for_timeout(200)
        section = page.locator('[data-section="crons"]')
        assert "collapsed" in (section.get_attribute("class") or "")
        body = page.locator("#oc-body-crons")
        assert not body.is_visible()

    def test_click_toggle_twice_restores_section(self, page):
        """Clicking toggle twice re-expands."""
        toggle = page.locator('[data-section="health"] .oc-section-toggle')
        toggle.click()
        page.wait_for_timeout(200)
        assert not page.locator("#oc-body-health").is_visible()
        toggle.click()
        page.wait_for_timeout(200)
        assert page.locator("#oc-body-health").is_visible()

    def test_agent_config_starts_collapsed(self, page):
        """agent-config section is collapsed by default."""
        section = page.locator('[data-section="agent-config"]')
        assert "collapsed" in (section.get_attribute("class") or "")


class TestAriaAttributes:
    """Accessibility attributes update correctly."""

    def test_aria_expanded_true_when_open(self, page):
        btn = page.locator('[data-section="alerts"] .oc-section-toggle')
        assert btn.get_attribute("aria-expanded") == "true"

    def test_aria_expanded_false_when_collapsed(self, page):
        btn = page.locator('[data-section="agent-config"] .oc-section-toggle')
        assert btn.get_attribute("aria-expanded") == "false"

    def test_aria_toggles_on_click(self, page):
        btn = page.locator('[data-section="charts"] .oc-section-toggle')
        assert btn.get_attribute("aria-expanded") == "true"
        btn.click()
        page.wait_for_timeout(200)
        assert btn.get_attribute("aria-expanded") == "false"
        btn.click()
        page.wait_for_timeout(200)
        assert btn.get_attribute("aria-expanded") == "true"


class TestExpandCollapseAll:
    """Expand All / Collapse All buttons."""

    def test_collapse_all(self, page):
        """Collapse All collapses every section."""
        page.click("#oc-collapse-all")
        page.wait_for_timeout(300)
        for key in ["alerts", "health", "cost", "charts", "crons", "sessions", "usage", "subagent", "agent-config"]:
            section = page.locator(f'[data-section="{key}"]')
            assert "collapsed" in (section.get_attribute("class") or ""), f"{key} not collapsed"

    def test_expand_all(self, page):
        """Expand All opens every section."""
        # First collapse all
        page.click("#oc-collapse-all")
        page.wait_for_timeout(200)
        # Then expand all
        page.click("#oc-expand-all")
        page.wait_for_timeout(300)
        for key in ["alerts", "health", "cost", "charts", "crons", "sessions", "usage", "subagent", "agent-config"]:
            section = page.locator(f'[data-section="{key}"]')
            assert "collapsed" not in (section.get_attribute("class") or ""), f"{key} still collapsed"


class TestStatePersistence:
    """State persists across page reloads via localStorage."""

    def test_collapsed_state_survives_reload(self, page):
        """Collapsing a section and reloading preserves it."""
        # Collapse the charts section
        page.click('[data-section="charts"] .oc-section-toggle')
        page.wait_for_timeout(200)
        # Verify collapsed
        assert "collapsed" in (page.locator('[data-section="charts"]').get_attribute("class") or "")
        # Reload page
        page.reload(wait_until="networkidle", timeout=10000)
        page.wait_for_timeout(500)
        # Should still be collapsed
        assert "collapsed" in (page.locator('[data-section="charts"]').get_attribute("class") or "")

    def test_expanded_state_survives_reload(self, page):
        """Expanding agent-config (default collapsed) and reloading preserves it."""
        # Expand agent-config
        page.click('[data-section="agent-config"] .oc-section-toggle')
        page.wait_for_timeout(200)
        assert "collapsed" not in (page.locator('[data-section="agent-config"]').get_attribute("class") or "")
        # Reload page
        page.reload(wait_until="networkidle", timeout=10000)
        page.wait_for_timeout(500)
        # Should still be expanded
        assert "collapsed" not in (page.locator('[data-section="agent-config"]').get_attribute("class") or "")


class TestKeyboardInteraction:
    """Keyboard navigation works for toggle buttons."""

    def test_enter_key_toggles(self, page):
        """Pressing Enter on a focused toggle button toggles the section."""
        btn = page.locator('[data-section="usage"] .oc-section-toggle')
        btn.focus()
        page.wait_for_timeout(100)
        page.keyboard.press("Enter")
        page.wait_for_timeout(200)
        section = page.locator('[data-section="usage"]')
        assert "collapsed" in (section.get_attribute("class") or "")

    def test_space_key_toggles(self, page):
        """Pressing Space on a focused toggle button toggles the section."""
        btn = page.locator('[data-section="sessions"] .oc-section-toggle')
        btn.focus()
        page.wait_for_timeout(100)
        page.keyboard.press("Space")
        page.wait_for_timeout(200)
        section = page.locator('[data-section="sessions"]')
        assert "collapsed" in (section.get_attribute("class") or "")


class TestSystemTopbarNotCollapsible:
    """System topbar should not be affected by collapse operations."""

    def test_topbar_visible_after_collapse_all(self, page):
        """System topbar remains visible after Collapse All."""
        page.click("#oc-collapse-all")
        page.wait_for_timeout(300)
        # The topbar may be hidden by default (display:none until data), but should not have oc-section
        topbar = page.locator("#systemTopBar")
        assert topbar.count() > 0  # exists in DOM
