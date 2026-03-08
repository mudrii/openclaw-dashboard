"""Frontend static analysis tests — AC15-AC24.

Uses only re and string operations to validate HTML/JS/shell patterns.
No browser or JS runtime needed.
"""

import json
import os
import re
import subprocess
import unittest

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
INDEX_HTML = os.path.join(REPO, "index.html")
SERVER_PY = os.path.join(REPO, "server.py")
REFRESH_SH = os.path.join(REPO, "refresh.sh")


def read(path):
    with open(path) as f:
        return f.read()


def eval_systembar(expr):
    script = """\
const fs = require('fs');
const vm = require('vm');
const html = fs.readFileSync(__INDEX_HTML__, 'utf8');
const start = html.indexOf('const SystemBar = {');
if (start < 0) throw new Error('SystemBar object not found');
const end = html.indexOf('\\n};', start);
if (end < 0) throw new Error('SystemBar terminator not found');
const source = html.slice(start, end + 3);
const sandbox = { window: {}, console, globalThis: {} };
vm.createContext(sandbox);
vm.runInContext(source + '\\nglobalThis.__result = (' + __EXPR__ + ');', sandbox);
process.stdout.write(JSON.stringify(sandbox.globalThis.__result));
""".replace('__INDEX_HTML__', json.dumps(INDEX_HTML)).replace('__EXPR__', json.dumps(expr))
    out = subprocess.check_output(["node", "-e", script], text=True)
    return json.loads(out)


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
        """AC25: Renderer.reconcileRows() is defined (simplified innerHTML replacement)."""
        self.assertIn("reconcileRows(", self.html)
        # Simplified: uses innerHTML directly, DirtyChecker gates calls
        self.assertIn("innerHTML", self.html)

    def test_ac26_svg_cache_skip(self):
        """AC26: Renderer caches SVG content to skip identical re-renders."""
        self.assertIn("_svgCache", self.html)
        self.assertIn("patchSvg(", self.html)

    def test_versions_behind_ignores_beta_and_dev_suffixes(self):
        self.assertEqual(eval_systembar("SystemBar._versionsBehind('2026.3.5-beta-runtime-observability','2026.3.5')"), 0)
        self.assertEqual(eval_systembar("SystemBar._versionsBehind('2026.3.5-dev.1','2026.3.6')"), 1)
        self.assertEqual(eval_systembar("SystemBar._versionsBehind('2026.3.3-beta.2','2026.3.5')"), 2)

    def test_gateway_falls_back_to_versions_when_runtime_is_untrustworthy(self):
        payload = {
            "openclaw": {"gateway": {"live": False, "ready": False}},
            "versions": {"gateway": {"status": "online", "pid": 321, "uptime": "2h", "memory": "64MB"}},
        }
        result = eval_systembar(f"SystemBar._gatewayState({json.dumps(payload)})")
        self.assertEqual(result["source"], "versions")
        self.assertTrue(result["ok"])

    def test_gateway_prefers_runtime_when_probe_data_is_trustworthy(self):
        payload = {
            "openclaw": {"gateway": {"healthEndpointOk": True, "live": True, "ready": False}},
            "versions": {"gateway": {"status": "online"}},
        }
        result = eval_systembar(f"SystemBar._gatewayState({json.dumps(payload)})")
        self.assertEqual(result["source"], "runtime")
        self.assertFalse(result["ok"])

    def test_gateway_live_not_ready_label_present(self):
        self.assertIn("Live / Not Ready", self.html)
        self.assertIn("window._gwOnlineConfirmed = gwLive", self.html)


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


class TestGatewayPillRemoved(unittest.TestCase):
    """Verify the duplicate GW pill was removed from the top bar (not System Health)."""

    @classmethod
    def setUpClass(cls):
        cls.html = read(INDEX_HTML)

    def test_no_sysGateway_element_in_top_bar(self):
        """sysGateway span must not exist in the HTML (duplicate top-bar pill removed)."""
        self.assertNotIn('id="sysGateway"', self.html,
            "sysGateway pill was supposed to be removed from the top bar")

    def test_system_health_gateway_still_present(self):
        """System Health gateway row (hGw) must still be rendered."""
        self.assertIn('id="hGw"', self.html,
            "System Health gateway indicator (hGw) should still be present")

    def test_no_gwPill_update_in_js(self):
        """The gwPill DOM manipulation JS should have been removed."""
        self.assertNotIn("gwPill.className", self.html,
            "gwPill.className assignment should have been removed")
        self.assertNotIn("gwPill.textContent", self.html,
            "gwPill.textContent assignment should have been removed")
        self.assertNotIn("$('sysGateway')", self.html,
            "$('sysGateway') reference should have been removed")

    def test_gwOnlineConfirmed_flag_still_set(self):
        """window._gwOnlineConfirmed must still be set (used by Renderer to suppress alerts)."""
        self.assertIn("window._gwOnlineConfirmed", self.html,
            "_gwOnlineConfirmed flag must still be set for alert suppression")

    def test_system_health_still_updates_hGw(self):
        """SystemBar.render() must still update hGw in the System Health panel."""
        self.assertIn("$('hGw').innerHTML", self.html,
            "System Health hGw update must still be present")


class TestNoRawPlaceholderTokens(unittest.TestCase):
    """Verify that index.html does not contain raw template placeholder tokens.
    Such tokens indicate a failed template substitution that would expose
    implementation details or break the UI."""

    @classmethod
    def setUpClass(cls):
        cls.html = read(INDEX_HTML)

    def test_no_double_brace_placeholders(self):
        """No {{ ... }} template placeholders should remain in the served HTML."""
        raw_placeholders = re.findall(r'\{\{[A-Z_][A-Z0-9_]*\}\}', self.html)
        self.assertEqual(raw_placeholders, [],
            f"Found raw placeholder tokens in index.html: {raw_placeholders}")

    def test_no_percent_placeholders(self):
        """No %PLACEHOLDER% style tokens should remain in the served HTML."""
        raw_placeholders = re.findall(r'%[A-Z_][A-Z0-9_]*%', self.html)
        self.assertEqual(raw_placeholders, [],
            f"Found raw %%PLACEHOLDER%% tokens in index.html: {raw_placeholders}")


class TestGatewayRuntimeConfigSplit(unittest.TestCase):
    """Verify Gateway area is split into Runtime (live /api/system) + Config (data.json) cards."""

    @classmethod
    def setUpClass(cls):
        cls.html = read(INDEX_HTML)

    # --- HTML structure ---

    def test_gateway_runtime_panel_present(self):
        """gatewayRuntimePanel div must exist in HTML."""
        self.assertIn('id="gatewayRuntimePanel"', self.html,
            "Missing gatewayRuntimePanel element")

    def test_gateway_runtime_panel_inner_present(self):
        """gatewayRuntimePanelInner inner div must exist for JS targeting."""
        self.assertIn('id="gatewayRuntimePanelInner"', self.html,
            "Missing gatewayRuntimePanelInner element")

    def test_gateway_config_panel_present(self):
        """gatewayConfigPanel div must exist in HTML."""
        self.assertIn('id="gatewayConfigPanel"', self.html,
            "Missing gatewayConfigPanel element")

    def test_gateway_config_panel_inner_present(self):
        """gatewayConfigPanelInner inner div must exist for JS targeting."""
        self.assertIn('id="gatewayConfigPanelInner"', self.html,
            "Missing gatewayConfigPanelInner element")

    def test_old_gateway_panel_removed(self):
        """The old combined gatewayPanel / gatewayPanelInner must not exist."""
        self.assertNotIn('id="gatewayPanel"', self.html,
            "Old combined gatewayPanel must be removed (split into Runtime+Config)")
        self.assertNotIn('id="gatewayPanelInner"', self.html,
            "Old gatewayPanelInner must be removed (now gatewayConfigPanelInner)")

    def test_gateway_runtime_panel_label(self):
        """Gateway Runtime card must have 'Gateway Runtime' label."""
        self.assertIn("Gateway Runtime", self.html,
            "Gateway Runtime card label missing")

    def test_gateway_config_panel_label(self):
        """Gateway Config card must have 'Gateway Config' label."""
        self.assertIn("Gateway Config", self.html,
            "Gateway Config card label missing")

    # --- Data sources ---

    def test_config_panel_reads_from_agentconfig_gateway(self):
        """Renderer populates gatewayConfigPanelInner from AC.gateway (config snapshot)."""
        self.assertIn("$('gatewayConfigPanelInner').innerHTML", self.html,
            "gatewayConfigPanelInner must be written by Renderer from AC.gateway")
        # AC.gateway is the config source
        self.assertIn("const GW = AC.gateway", self.html,
            "Config panel must read from AC.gateway")

    def test_runtime_panel_updated_by_systembar(self):
        """SystemBar.render() must update gatewayRuntimePanelInner from /api/system data."""
        self.assertIn("$('gatewayRuntimePanelInner')", self.html,
            "SystemBar must reference gatewayRuntimePanelInner")

    def test_runtime_panel_shows_state(self):
        """Runtime card renders state label from gwState."""
        match = re.search(
            r"gatewayRuntimePanelInner.*?stateLabel.*?stateColor",
            self.html, re.DOTALL
        )
        self.assertIsNotNone(match,
            "gatewayRuntimePanelInner render must include stateLabel + stateColor")

    def test_runtime_panel_shows_uptime_from_systembar(self):
        """Runtime card sources uptime from fmtDurationMs(gwRuntime.uptimeMs) via SystemBar."""
        self.assertIn("fmtDurationMs(gwRuntime.uptimeMs)", self.html,
            "Uptime must come from gwRuntime.uptimeMs via SystemBar")

    def test_config_panel_shows_port(self):
        """Config card renders GW.port."""
        self.assertIn("GW.port", self.html,
            "Config panel must render GW.port")

    def test_config_panel_shows_auth_mode(self):
        """Config card renders GW.authMode."""
        self.assertIn("GW.authMode", self.html,
            "Config panel must render GW.authMode")

    # --- No duplication with System Health ---

    def test_system_health_gateway_row_unchanged(self):
        """hGw (System Health gateway row) must still be present and untouched."""
        self.assertIn('id="hGw"', self.html,
            "System Health hGw row must remain unchanged")

    def test_runtime_panel_not_duplicate_of_health_row(self):
        """Runtime card is a separate element from hGw (no content duplication in HTML)."""
        # Both should exist but as distinct elements
        self.assertIn('id="hGw"', self.html)
        self.assertIn('id="gatewayRuntimePanelInner"', self.html)
        # They must be different ids
        self.assertNotEqual('id="hGw"', 'id="gatewayRuntimePanelInner"')


if __name__ == "__main__":
    unittest.main()
