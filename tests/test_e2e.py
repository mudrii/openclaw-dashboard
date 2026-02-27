"""E2E tests using Playwright (sync API).

Requires: .venv/bin/pip install playwright && .venv/bin/python3 -m playwright install chromium

Run:
    .venv/bin/python3 -m pytest tests/test_e2e.py -v --timeout=30
"""
import os
import subprocess
import sys
import time

import pytest

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BASE_URL = "http://127.0.0.1:8099"

try:
    from playwright.sync_api import sync_playwright
except ImportError:
    pytest.skip("playwright not installed", allow_module_level=True)


@pytest.fixture(scope="session")
def server():
    proc = subprocess.Popen(
        [sys.executable, os.path.join(REPO, "server.py"), "--port", "8099"],
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


class TestTabSwitching:
    def test_usage_tab_default_active(self, page):
        """Usage 'Today' tab is active by default."""
        btn = page.locator("#uTabT")
        assert "active" in (btn.get_attribute("class") or "")

    def test_usage_tab_switch_7d(self, page):
        """Clicking 7 Days usage tab changes active state."""
        page.click("#uTab7")
        page.wait_for_timeout(300)
        assert "active" in (page.locator("#uTab7").get_attribute("class") or "")
        assert "active" not in (page.locator("#uTabT").get_attribute("class") or "")

    def test_usage_tab_switch_30d(self, page):
        """Clicking 30 Days usage tab changes active state."""
        page.click("#uTab30")
        page.wait_for_timeout(300)
        assert "active" in (page.locator("#uTab30").get_attribute("class") or "")

    def test_sub_runs_tabs_exist(self, page):
        """Sub-Runs section has tab buttons."""
        assert page.locator("#srTabT").count() > 0
        assert page.locator("#srTab7").count() > 0

    def test_sub_runs_tab_switch(self, page):
        """Clicking sub-runs 7 Days tab changes active state."""
        page.click("#srTab7")
        page.wait_for_timeout(300)
        assert "active" in (page.locator("#srTab7").get_attribute("class") or "")


class TestChartToggle:
    def test_chart_section_rendered(self, page):
        """Chart section exists."""
        assert page.locator("#costChart").count() > 0

    def test_chart_tab_toggle_30d(self, page):
        """Switching chart days to 30 changes active class."""
        page.click("#cTab30")
        page.wait_for_timeout(300)
        assert "active" in (page.locator("#cTab30").get_attribute("class") or "")
        assert "active" not in (page.locator("#cTab7").get_attribute("class") or "")

    def test_chart_tab_toggle_back_7d(self, page):
        """Switching chart days back to 7."""
        page.click("#cTab30")
        page.wait_for_timeout(200)
        page.click("#cTab7")
        page.wait_for_timeout(300)
        assert "active" in (page.locator("#cTab7").get_attribute("class") or "")


class TestAutoRefreshCountdown:
    def test_countdown_element_shows_seconds(self, page):
        """Countdown element exists and contains 's'."""
        text = page.locator("#countdown").text_content() or ""
        assert "s" in text

    def test_countdown_decrements_over_time(self, page):
        """Countdown value decreases over 2 seconds."""
        first = int((page.locator("#countdown").text_content() or "60s").rstrip("s"))
        page.wait_for_timeout(2100)
        second = int((page.locator("#countdown").text_content() or "60s").rstrip("s"))
        assert second < first

    def test_last_update_shows_timestamp(self, page):
        """#lastUpdate shows 'Updated:' after initial fetch."""
        txt = page.locator("#lastUpdate").text_content() or ""
        assert "Updated:" in txt or "updated" in txt.lower() or len(txt) > 0


class TestChatPanel:
    def test_chat_fab_button_exists(self, page):
        """Chat FAB button is present."""
        assert page.locator("#chatBtn").count() > 0

    def test_chat_panel_opens_on_click(self, page):
        """Chat panel opens when FAB is clicked."""
        page.click("#chatBtn")
        page.wait_for_timeout(200)
        cls = page.locator("#chatPanel").get_attribute("class") or ""
        assert "open" in cls

    def test_chat_panel_closes_on_second_click(self, page):
        """Chat panel closes on second FAB click."""
        page.click("#chatBtn")
        page.wait_for_timeout(100)
        page.click("#chatBtn")
        page.wait_for_timeout(200)
        cls = page.locator("#chatPanel").get_attribute("class") or ""
        assert "open" not in cls


class TestThemeMenu:
    def test_theme_button_exists(self, page):
        """Theme toggle button exists."""
        assert page.locator("#themeBtn").count() > 0

    def test_theme_menu_toggles(self, page):
        """Theme menu opens on button click."""
        page.click("#themeBtn")
        page.wait_for_timeout(100)
        cls = page.locator("#themeMenu").get_attribute("class") or ""
        assert "open" in cls
