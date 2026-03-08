"""
tests/test_system_metrics.py — unit tests for system_metrics.py collectors/parsers.
"""
import json
import sys
import os
import time
import unittest
import subprocess
from unittest.mock import patch, MagicMock

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import system_metrics


class TestParsers(unittest.TestCase):

    def test_parse_top_cpu_valid(self):
        sample = (
            "Processes: 500 total, 3 running\n"
            "CPU usage: 12.34% user, 5.00% sys, 82.66% idle\n"
        )
        r = system_metrics.parse_top_cpu(sample, cores=8)
        self.assertIsNone(r["error"])
        self.assertAlmostEqual(r["percent"], 17.3, places=0)
        self.assertEqual(r["cores"], 8)

    def test_parse_top_cpu_no_line(self):
        r = system_metrics.parse_top_cpu("no useful output", cores=4)
        self.assertIsNotNone(r["error"])
        self.assertEqual(r["percent"], 0.0)

    def test_parse_vm_stat_valid(self):
        sample = (
            "Mach Virtual Memory Statistics: (page size of 16384 bytes)\n"
            "Pages free:                               12345.\n"
            "Pages active:                             50000.\n"
            "Pages inactive:                           20000.\n"
            "Pages wired down:                         30000.\n"
            "Pages occupied by compressor:             10000.\n"
        )
        total = 17179869184  # 16 GB
        r = system_metrics.parse_vm_stat(sample, total)
        self.assertIsNone(r["error"])
        self.assertEqual(r["totalBytes"], total)
        expected_used = (50000 + 30000 + 10000) * 16384
        self.assertEqual(r["usedBytes"], expected_used)
        self.assertGreater(r["percent"], 0)

    def test_parse_vm_stat_no_pages(self):
        r = system_metrics.parse_vm_stat("no pages here", total_bytes=1024)
        self.assertIsNotNone(r["error"])

    def test_parse_swap_usage_darwin_megabytes(self):
        sample = "vm.swapusage: total = 4096.00M  used = 512.00M  free = 3584.00M"
        r = system_metrics.parse_swap_usage_darwin(sample)
        self.assertIsNone(r["error"])
        self.assertEqual(r["totalBytes"], 4096 * 1024 * 1024)
        self.assertEqual(r["usedBytes"], 512 * 1024 * 1024)
        self.assertAlmostEqual(r["percent"], 12.5, places=1)

    def test_parse_swap_usage_darwin_gigabytes(self):
        sample = "vm.swapusage: total = 8.00G  used = 2.00G  free = 6.00G"
        r = system_metrics.parse_swap_usage_darwin(sample)
        self.assertIsNone(r["error"])
        self.assertEqual(r["totalBytes"], 8 * 1024 ** 3)
        self.assertEqual(r["usedBytes"], 2 * 1024 ** 3)

    def test_parse_swap_usage_darwin_invalid(self):
        r = system_metrics.parse_swap_usage_darwin("nothing useful here")
        self.assertIsNotNone(r["error"])

    def test_parse_proc_meminfo(self):
        sample = (
            "MemTotal:       16384000 kB\n"
            "MemFree:         2048000 kB\n"
            "MemAvailable:    8192000 kB\n"
            "SwapTotal:       4096000 kB\n"
            "SwapFree:        2048000 kB\n"
        )
        info = system_metrics.parse_proc_meminfo(sample)
        self.assertEqual(info["MemTotal"], 16384000)
        self.assertEqual(info["MemAvailable"], 8192000)
        self.assertEqual(info["SwapTotal"], 4096000)
        self.assertEqual(info["SwapFree"], 2048000)


class TestDiskCollector(unittest.TestCase):

    def test_collect_disk_root_returns_valid(self):
        r = system_metrics.collect_disk("/")
        self.assertIsNone(r["error"])
        self.assertGreater(r["totalBytes"], 0)
        self.assertGreaterEqual(r["usedBytes"], 0)
        self.assertGreaterEqual(r["percent"], 0)
        self.assertLessEqual(r["percent"], 100)

    def test_collect_disk_invalid_path(self):
        r = system_metrics.collect_disk("/nonexistent-path-xyz-abc")
        self.assertIsNotNone(r["error"])
        self.assertEqual(r["totalBytes"], 0)


class TestCache(unittest.TestCase):

    def _reset_state(self):
        import system_metrics as sm
        sm._ms.payload = None
        sm._ms.at = 0.0
        sm._ms.refreshing = False
        sm._vs.cache = None
        sm._vs.at = 0.0
        sm._cfg = {
            "enabled": True,
            "pollSeconds": 5,
            "metricsTtlSeconds": 5,
            "versionsTtlSeconds": 300,
            "gatewayTimeoutMs": 1500,
            "diskPath": "/",
            "warnPercent": 70,
            "criticalPercent": 85,
        }

    def setUp(self):
        # Reset cache state before each test
        self._reset_state()

    def tearDown(self):
        # Keep tests isolated if one mutates module state unexpectedly
        self._reset_state()

    def test_cache_returns_same_within_ttl(self):
        status1, body1 = system_metrics.get_payload()
        status2, body2 = system_metrics.get_payload()
        self.assertEqual(status1, 200)
        self.assertEqual(status2, 200)
        self.assertEqual(body1, body2)

    def test_cache_refreshes_after_ttl(self):
        import system_metrics as sm
        sm._cfg["metricsTtlSeconds"] = 0  # expire immediately
        status1, body1 = system_metrics.get_payload()
        time.sleep(0.05)
        # Force cache expiry
        sm._ms.at = 0
        status2, body2 = system_metrics.get_payload()
        self.assertEqual(status1, 200)
        self.assertEqual(status2, 200)

    def test_disabled_returns_503(self):
        import system_metrics as sm
        sm._cfg["enabled"] = False
        status, body = system_metrics.get_payload()
        self.assertEqual(status, 503)
        import json
        data = json.loads(body)
        self.assertFalse(data["ok"])

    def test_payload_includes_openclaw_contract(self):
        status, body = system_metrics.get_payload()
        self.assertEqual(status, 200)
        import json
        data = json.loads(body)
        self.assertIn("openclaw", data)
        oc = data["openclaw"]
        for key in ("gateway", "status", "freshness"):
            self.assertIn(key, oc)
        self.assertNotIn("channels", oc)
        self.assertNotIn("bindings", oc)


class TestThresholdClamping(unittest.TestCase):
    """Parity with Go's TestSystemConfig_PerMetricThresholdClamping — ensures per-metric
    thresholds are clamped to valid ranges and critical > warn."""

    def _reset_state(self):
        import system_metrics as sm
        sm._ms.payload = None
        sm._ms.at = 0.0
        sm._ms.refreshing = False

    def setUp(self):
        self._reset_state()

    def tearDown(self):
        self._reset_state()

    def _get_thresholds(self, cfg_overrides):
        import system_metrics as sm
        saved = dict(sm._cfg)
        sm._cfg.update(cfg_overrides)
        sm._ms.payload = None
        sm._ms.at = 0.0
        try:
            status, body = sm.get_payload()
            self.assertEqual(status, 200)
            import json
            data = json.loads(body)
            return data.get("thresholds", {})
        finally:
            sm._cfg = saved

    def test_valid_thresholds_unchanged(self):
        t = self._get_thresholds({"cpu": {"warn": 75, "critical": 90}})
        self.assertEqual(t["cpu"]["warn"], 75)
        self.assertEqual(t["cpu"]["critical"], 90)

    def test_critical_less_than_warn_clamped(self):
        t = self._get_thresholds({"cpu": {"warn": 80, "critical": 60}})
        self.assertEqual(t["cpu"]["warn"], 80)
        self.assertGreater(t["cpu"]["critical"], t["cpu"]["warn"],
            "critical must be > warn after clamping")

    def test_critical_exceeds_100_clamped(self):
        t = self._get_thresholds({"swap": {"warn": 85, "critical": 105}})
        self.assertEqual(t["swap"]["warn"], 85)
        self.assertLessEqual(t["swap"]["critical"], 100)
        self.assertGreater(t["swap"]["critical"], t["swap"]["warn"])

    def test_defaults_when_absent(self):
        t = self._get_thresholds({})
        # Default: 80/95
        self.assertEqual(t["disk"]["warn"], 80)
        self.assertEqual(t["disk"]["critical"], 95)

    def test_warn_clamped_to_1_min(self):
        t = self._get_thresholds({"ram": {"warn": -5, "critical": 50}})
        self.assertGreaterEqual(t["ram"]["warn"], 1)

    def test_warn_clamped_to_99_max(self):
        t = self._get_thresholds({"ram": {"warn": 150, "critical": 200}})
        self.assertLessEqual(t["ram"]["warn"], 99)
        self.assertGreater(t["ram"]["critical"], t["ram"]["warn"])


class TestStaleInjection(unittest.TestCase):
    """Test that stale flag is correctly injected via byte replacement."""

    def _reset_state(self):
        import system_metrics as sm
        sm._ms.payload = None
        sm._ms.at = 0.0
        sm._ms.refreshing = False

    def setUp(self):
        self._reset_state()

    def tearDown(self):
        self._reset_state()

    def test_stale_injection_works(self):
        import system_metrics as sm
        import json
        # Collect fresh data first
        status, body = sm.get_payload()
        self.assertEqual(status, 200)
        data = json.loads(body)
        self.assertFalse(data.get("stale", True))

        # Expire the cache but keep payload for stale serving
        sm._ms.at = 0
        sm._cfg["metricsTtlSeconds"] = 0

        # This should return stale=true
        status2, body2 = sm.get_payload()
        self.assertEqual(status2, 200)
        data2 = json.loads(body2)
        self.assertTrue(data2.get("stale"),
            "Expected stale=true in expired cache response")


class TestOpenclawRuntime(unittest.TestCase):

    @patch("system_metrics.urllib.request.urlopen")
    @patch("system_metrics.subprocess.run")
    def test_openclaw_runtime_contract_excludes_channels_and_bindings(self, mock_run, mock_urlopen):
        import system_metrics as sm

        class _Resp:
            def __init__(self, payload):
                self._payload = payload
                self.status = 200
            def read(self):
                return self._payload
            def __enter__(self):
                return self
            def __exit__(self, exc_type, exc, tb):
                return False

        def _urlopen(req, timeout=None):
            url = req.full_url
            if url.endswith("/healthz"):
                return _Resp(b'{"ok":true,"status":"live"}')
            if url.endswith("/readyz"):
                return _Resp(b'{"ready":true,"failing":[],"uptimeMs":111}')
            raise RuntimeError("unexpected URL")

        def _run(cmd, capture_output=True, text=True, timeout=None):
            m = MagicMock()
            if cmd[1:] == ["status", "--json"]:
                m.stdout = '{"connectLatencyMs": 42, "security": {"mode":"strict"}}\n'
                m.stderr = ""
                m.returncode = 0
                return m
            raise AssertionError(f"unexpected cmd: {cmd}")

        mock_urlopen.side_effect = _urlopen
        mock_run.side_effect = _run

        oc = sm._collect_openclaw_runtime("openclaw", {"openclaw": "1.0.0", "latest": "1.1.0"})
        self.assertTrue(oc["gateway"]["live"])
        self.assertTrue(oc["gateway"]["ready"])
        self.assertEqual(oc["status"]["currentVersion"], "1.0.0")
        self.assertEqual(oc["status"]["latestVersion"], "1.1.0")
        self.assertNotIn("channels", oc)
        self.assertNotIn("bindings", oc)
        self.assertNotIn("errors", oc)

    @patch("system_metrics.urllib.request.urlopen")
    @patch("system_metrics.subprocess.run")
    def test_openclaw_runtime_preserves_status_version_fields(self, mock_run, mock_urlopen):
        import system_metrics as sm

        class _Resp:
            def __init__(self, payload):
                self._payload = payload
                self.status = 200
            def read(self):
                return self._payload
            def __enter__(self):
                return self
            def __exit__(self, exc_type, exc, tb):
                return False

        def _urlopen(req, timeout=None):
            url = req.full_url
            if url.endswith("/healthz"):
                return _Resp(b'{"ok":true,"status":"live"}')
            if url.endswith("/readyz"):
                return _Resp(b'{"ready":true,"uptimeMs":100}')
            raise RuntimeError("unexpected URL")

        def _run(cmd, capture_output=True, text=True, timeout=None):
            m = MagicMock()
            m.returncode = 0
            if cmd[1:] == ["status", "--json"]:
                m.stdout = '{"currentVersion":"2026.3.5-beta-runtime-observability","latestVersion":"2026.3.5","connectLatencyMs":10}\n'
                return m
            if cmd[1:] == ["channels", "status", "--probe", "--json"]:
                m.stdout = '{"channelLabels":{},"channels":{}}\n'
                return m
            if cmd[1:] == ["agents", "bindings", "--json"]:
                m.stdout = '[]\n'
                return m
            raise AssertionError(f"unexpected cmd: {cmd}")

        mock_urlopen.side_effect = _urlopen
        mock_run.side_effect = _run

        oc = sm._collect_openclaw_runtime("openclaw", {"openclaw": "2026.3.4", "latest": "2026.3.6"})
        self.assertEqual(oc["status"]["currentVersion"], "2026.3.5-beta-runtime-observability")
        self.assertEqual(oc["status"]["latestVersion"], "2026.3.5")
        self.assertEqual(oc["status"]["connectLatencyMs"], 10)

    @patch("system_metrics.urllib.request.urlopen")
    @patch("system_metrics.subprocess.run")
    def test_openclaw_runtime_runs_in_parallel(self, mock_run, mock_urlopen):
        """BLOCKER fix: subcollectors must run concurrently, not sequentially.
        Each subcollector sleeps 0.3s — sequential would take >1.2s, parallel <0.8s."""
        import system_metrics as sm

        class _Resp:
            def __init__(self, payload):
                self._payload = payload
                self.status = 200
            def read(self):
                return self._payload
            def __enter__(self):
                return self
            def __exit__(self, exc_type, exc, tb):
                return False

        def _urlopen(req, timeout=None):
            time.sleep(0.3)
            url = req.full_url
            if url.endswith("/healthz"):
                return _Resp(b'{"ok":true,"status":"live"}')
            if url.endswith("/readyz"):
                return _Resp(b'{"ready":true,"uptimeMs":100}')
            raise RuntimeError("unexpected URL")

        def _run(cmd, capture_output=True, text=True, timeout=None):
            time.sleep(0.3)
            m = MagicMock()
            m.returncode = 0
            if cmd[1:] == ["status", "--json"]:
                m.stdout = '{"connectLatencyMs": 10}\n'
                return m
            if cmd[1:] == ["channels", "status", "--probe", "--json"]:
                m.stdout = '{"channelLabels":{},"channels":{}}\n'
                return m
            if cmd[1:] == ["agents", "bindings", "--json"]:
                m.stdout = '[]\n'
                return m
            m.returncode = 1
            m.stdout = ''
            return m

        mock_urlopen.side_effect = _urlopen
        mock_run.side_effect = _run

        start = time.monotonic()
        oc = sm._collect_openclaw_runtime("openclaw", {"openclaw": "1.0.0", "latest": "1.1.0"})
        elapsed = time.monotonic() - start

        self.assertLess(elapsed, 1.0,
            f"Subcollectors took {elapsed:.2f}s — should be <1.0s if parallel (4×0.3s sequential = 1.2s+)")
        # Verify the results are still correct
        self.assertTrue(oc["gateway"]["live"])

    @patch("system_metrics.urllib.request.urlopen")
    @patch("system_metrics.subprocess.run")
    def test_subprocess_nonzero_returncode_treated_as_error(self, mock_run, mock_urlopen):
        """IMPORTANT fix: non-zero return code from subprocess must report an error."""
        import system_metrics as sm

        class _Resp:
            def __init__(self, payload):
                self._payload = payload
                self.status = 200
            def read(self):
                return self._payload
            def __enter__(self):
                return self
            def __exit__(self, exc_type, exc, tb):
                return False

        def _urlopen(req, timeout=None):
            url = req.full_url
            if url.endswith("/healthz"):
                return _Resp(b'{"ok":true,"status":"live"}')
            if url.endswith("/readyz"):
                return _Resp(b'{"ready":true,"uptimeMs":100}')
            raise RuntimeError("unexpected URL")

        def _run(cmd, capture_output=True, text=True, timeout=None):
            m = MagicMock()
            if cmd[1:] == ["status", "--json"]:
                # Returns valid JSON but with non-zero exit code
                m.stdout = '{"connectLatencyMs": 10}\n'
                m.stderr = "some warning"
                m.returncode = 1
                return m
            if cmd[1:] == ["channels", "status", "--probe", "--json"]:
                m.stdout = '{"channelLabels":{},"channels":{}}\n'
                m.stderr = ""
                m.returncode = 0
                return m
            if cmd[1:] == ["agents", "bindings", "--json"]:
                m.stdout = '[]\n'
                m.stderr = ""
                m.returncode = 0
                return m
            m.returncode = 1
            m.stdout = ''
            return m

        mock_urlopen.side_effect = _urlopen
        mock_run.side_effect = _run

        oc = sm._collect_openclaw_runtime("openclaw", {"openclaw": "1.0.0", "latest": "1.1.0"})
        # The status --json returned non-zero, so an error should be recorded
        self.assertTrue(any("status --json" in e for e in oc["errors"]),
            f"Expected error for non-zero returncode on 'status --json', got: {oc['errors']}")

    @patch("system_metrics.urllib.request.urlopen")
    @patch("system_metrics.subprocess.run")
    def test_runtime_collector_no_longer_calls_channels_or_bindings(self, mock_run, mock_urlopen):
        """Channels/bindings runtime collection was removed and should not be invoked."""
        import system_metrics as sm

        class _Resp:
            def __init__(self, payload):
                self._payload = payload
                self.status = 200
            def read(self):
                return self._payload
            def __enter__(self):
                return self
            def __exit__(self, exc_type, exc, tb):
                return False

        def _urlopen(req, timeout=None):
            url = req.full_url
            if url.endswith("/healthz"):
                return _Resp(b'{"ok":true}')
            if url.endswith("/readyz"):
                return _Resp(b'{"ready":true}')
            raise RuntimeError("unexpected URL")

        def _run(cmd, capture_output=True, text=True, timeout=None):
            m = MagicMock()
            m.returncode = 0
            if cmd[1:] == ["status", "--json"]:
                m.stdout = '{}\n'
                return m
            raise AssertionError(f"unexpected cmd: {cmd}")

        mock_urlopen.side_effect = _urlopen
        mock_run.side_effect = _run

        oc = sm._collect_openclaw_runtime("openclaw", {"openclaw": "1.0.0", "latest": "1.1.0"})
        self.assertIn("gateway", oc)
        self.assertIn("status", oc)
        self.assertNotIn("channels", oc)
        self.assertNotIn("bindings", oc)

    @patch("system_metrics.urllib.request.urlopen")
    @patch("system_metrics.subprocess.run")
    def test_omit_empty_fields_parity_with_go(self, mock_run, mock_urlopen):
        """IMPORTANT fix: empty/null optional fields should be omitted (Go omitempty parity)."""
        import system_metrics as sm

        class _Resp:
            def __init__(self, payload):
                self._payload = payload
                self.status = 200
            def read(self):
                return self._payload
            def __enter__(self):
                return self
            def __exit__(self, exc_type, exc, tb):
                return False

        def _urlopen(req, timeout=None):
            url = req.full_url
            if url.endswith("/healthz"):
                return _Resp(b'{"ok":true}')
            if url.endswith("/readyz"):
                return _Resp(b'{"ready":true,"uptimeMs":0}')
            raise RuntimeError("unexpected URL")

        def _run(cmd, capture_output=True, text=True, timeout=None):
            m = MagicMock()
            m.returncode = 0
            if cmd[1:] == ["status", "--json"]:
                m.stdout = '{}\n'
                return m
            m.returncode = 1
            m.stdout = ''
            return m

        mock_urlopen.side_effect = _urlopen
        mock_run.side_effect = _run

        oc = sm._collect_openclaw_runtime("openclaw", {"openclaw": "", "latest": ""})

        # Status with empty versions should omit them
        status = oc["status"]
        if status.get("currentVersion") == "":
            self.fail("Empty currentVersion should be omitted (Go omitempty parity)")

        # Freshness with empty strings should omit them
        freshness = oc["freshness"]
        for key in ("status",):
            if key in freshness and freshness[key] == "":
                self.fail(f"Empty freshness.{key} should be omitted")

        # Removed fields should stay absent
        self.assertNotIn("channels", oc)
        self.assertNotIn("bindings", oc)

        # errors list should be omitted when empty (Go omitempty on slice)
        if "errors" in oc and len(oc["errors"]) == 0:
            self.fail("Empty errors list should be omitted (Go omitempty parity)")

    @patch("system_metrics.urllib.request.urlopen")
    def test_fetch_json_url_rejects_5xx(self, mock_urlopen):
        """IMPORTANT fix: _fetch_json_url should reject HTTP 5xx like Go's fetchJSONMap."""
        import system_metrics as sm

        class _5xxResp:
            def __init__(self):
                self.status = 500
            def read(self):
                return b'{"error":"internal"}'
            def __enter__(self):
                return self
            def __exit__(self, exc_type, exc, tb):
                return False

        mock_urlopen.return_value = _5xxResp()
        with self.assertRaises(Exception) as ctx:
            sm._fetch_json_url("http://127.0.0.1:18789/healthz", 1.0)
        self.assertIn("500", str(ctx.exception) + str(type(ctx.exception)),
            "Should raise on HTTP 5xx status")

    @patch("system_metrics.urllib.request.urlopen")
    @patch("system_metrics.subprocess.run")
    def test_probe_status_parses_stdout_on_nonzero_exit(self, mock_run, mock_urlopen):
        """I2 fix: _probe_status should parse useful stdout even on non-zero exit,
        matching Go's runWithTimeout behavior that returns stdout + error."""
        import system_metrics as sm

        class _Resp:
            def __init__(self, payload):
                self._payload = payload
                self.status = 200
            def read(self):
                return self._payload
            def __enter__(self):
                return self
            def __exit__(self, exc_type, exc, tb):
                return False

        def _urlopen(req, timeout=None):
            url = req.full_url
            if url.endswith("/healthz"):
                return _Resp(b'{"ok":true}')
            if url.endswith("/readyz"):
                return _Resp(b'{"ready":true}')
            raise RuntimeError("unexpected URL")

        def _run(cmd, capture_output=True, text=True, timeout=None):
            m = MagicMock()
            if cmd[1:] == ["status", "--json"]:
                # Non-zero exit but valid JSON stdout with useful data
                m.stdout = '{"currentVersion":"2026.3.5","connectLatencyMs":42}\n'
                m.stderr = "partial error"
                m.returncode = 1
                return m
            raise AssertionError(f"unexpected cmd: {cmd}")

        mock_urlopen.side_effect = _urlopen
        mock_run.side_effect = _run

        oc = sm._collect_openclaw_runtime("openclaw", {"openclaw": "1.0.0", "latest": "1.1.0"})

        # Should still report the error (non-zero exit)
        self.assertTrue(any("status --json" in e and "exit code 1" in e for e in oc.get("errors", [])),
            f"Expected error for non-zero exit, got: {oc.get('errors', [])}")

        # BUT should also have parsed the useful stdout data
        self.assertEqual(oc["status"].get("currentVersion"), "2026.3.5",
            "Should parse currentVersion from stdout despite non-zero exit")
        self.assertEqual(oc["status"].get("connectLatencyMs"), 42,
            "Should parse connectLatencyMs from stdout despite non-zero exit")

    @patch("system_metrics.urllib.request.urlopen")
    @patch("system_metrics.subprocess.run")
    def test_probe_status_no_stdout_on_nonzero_exit(self, mock_run, mock_urlopen):
        """I2: when non-zero exit and no useful stdout, only report error."""
        import system_metrics as sm

        class _Resp:
            def __init__(self, payload):
                self._payload = payload
                self.status = 200
            def read(self):
                return self._payload
            def __enter__(self):
                return self
            def __exit__(self, exc_type, exc, tb):
                return False

        def _urlopen(req, timeout=None):
            url = req.full_url
            if url.endswith("/healthz"):
                return _Resp(b'{"ok":true}')
            if url.endswith("/readyz"):
                return _Resp(b'{"ready":true}')
            raise RuntimeError("unexpected URL")

        def _run(cmd, capture_output=True, text=True, timeout=None):
            m = MagicMock()
            if cmd[1:] == ["status", "--json"]:
                m.stdout = ''  # no useful stdout
                m.stderr = "fatal error"
                m.returncode = 1
                return m
            raise AssertionError(f"unexpected cmd: {cmd}")

        mock_urlopen.side_effect = _urlopen
        mock_run.side_effect = _run

        oc = sm._collect_openclaw_runtime("openclaw", {"openclaw": "1.0.0", "latest": "1.1.0"})
        self.assertTrue(any("exit code 1" in e for e in oc.get("errors", [])),
            f"Expected exit code error, got: {oc.get('errors', [])}")
        # Versions should fall back to caller-provided values
        self.assertEqual(oc["status"].get("currentVersion"), "1.0.0")


class TestStaleInjectionSafe(unittest.TestCase):
    """B2 fix: stale injection uses safe JSON parse/modify/serialize,
    not fragile byte-level replacement."""

    def _reset_state(self):
        import system_metrics as sm
        sm._ms.payload = None
        sm._ms.at = 0.0
        sm._ms.refreshing = False
        sm._vs.cache = None
        sm._vs.at = 0.0

    def setUp(self):
        self._reset_state()

    def tearDown(self):
        self._reset_state()

    def test_stale_injection_works_with_compact_json(self):
        """Stale injection must work regardless of JSON formatting."""
        import system_metrics as sm
        # Manually cache a compact JSON payload (no spaces after colons)
        compact = json.dumps({"ok": True, "stale": False, "data": 123}, separators=(',', ':')).encode()
        sm._ms.payload = compact
        sm._ms.at = 0  # force stale
        sm._cfg["metricsTtlSeconds"] = 0

        status, body = sm.get_payload()
        self.assertEqual(status, 200)
        data = json.loads(body)
        self.assertTrue(data["stale"], "stale should be True after safe JSON injection")

    def test_stale_injection_works_with_pretty_json(self):
        """Stale injection must work with pretty-printed JSON too."""
        import system_metrics as sm
        pretty = json.dumps({"ok": True, "stale": False, "data": 456}, indent=2).encode()
        sm._ms.payload = pretty
        sm._ms.at = 0
        sm._cfg["metricsTtlSeconds"] = 0

        status, body = sm.get_payload()
        self.assertEqual(status, 200)
        data = json.loads(body)
        self.assertTrue(data["stale"], "stale should be True after safe JSON injection")

    def test_stale_injection_with_missing_stale_key(self):
        """If the cached payload somehow lacks a 'stale' key, injection should add it."""
        import system_metrics as sm
        no_stale = json.dumps({"ok": True, "data": 789}).encode()
        sm._ms.payload = no_stale
        sm._ms.at = 0
        sm._cfg["metricsTtlSeconds"] = 0

        status, body = sm.get_payload()
        self.assertEqual(status, 200)
        data = json.loads(body)
        self.assertTrue(data["stale"], "stale key should be injected even if absent")


class TestVersionCollectionI2(unittest.TestCase):
    """I2 fix: _collect_versions should parse useful stdout on non-zero exit."""

    def setUp(self):
        import system_metrics as sm
        sm._vs.cache = None
        sm._vs.at = 0.0
        sm._cfg["gatewayTimeoutMs"] = 1500
        sm._cfg["gatewayPort"] = 18789

    @patch("system_metrics.subprocess.run")
    def test_gateway_status_nonzero_but_valid_json_parsed(self, mock_run):
        """When gateway CLI exits non-zero but stdout has valid JSON, parse it."""
        import system_metrics as sm

        def _run(cmd, capture_output=True, text=True, timeout=None):
            m = MagicMock()
            if cmd[1:] == ["--version"]:
                m.stdout = "openclaw 2026.3.5\n"
                m.returncode = 0
                return m
            if cmd[1:] == ["gateway", "status", "--json"]:
                # Non-zero exit BUT valid JSON stdout
                m.stdout = '{"service":{"loaded":true,"runtime":{"status":"running","pid":1234}},"version":"2026.3.5"}\n'
                m.stderr = "warning: something"
                m.returncode = 1
                return m
            if "ps" in str(cmd):
                m.stdout = "01:23 2048\n"
                m.returncode = 0
                return m
            raise AssertionError(f"unexpected cmd: {cmd}")

        mock_run.side_effect = _run

        got = sm._collect_versions()
        self.assertEqual(got["gateway"]["status"], "online",
            "Should parse gateway status from stdout even on non-zero exit")
        self.assertEqual(got["gateway"]["version"], "2026.3.5",
            "Should parse version from stdout even on non-zero exit")
        self.assertEqual(got["gateway"]["pid"], 1234,
            "Should parse PID from stdout even on non-zero exit")


class TestVersionCollection(unittest.TestCase):

    def setUp(self):
        import system_metrics as sm
        sm._vs.cache = None
        sm._vs.at = 0.0
        sm._cfg["gatewayTimeoutMs"] = 1500
        sm._cfg["gatewayPort"] = 18789

    @patch("urllib.request.urlopen")
    @patch("system_metrics.subprocess.run")
    def test_gateway_status_nonzero_with_valid_stdout_uses_stdout(self, mock_run, mock_urlopen):
        """I2 fix: when CLI exits non-zero but stdout has valid JSON, parse it
        instead of falling back to HTTP probe. This matches Go behavior."""
        class _Resp:
            status = 204
            def __enter__(self):
                return self
            def __exit__(self, exc_type, exc, tb):
                return False

        def _run(cmd, capture_output=True, text=True, timeout=None):
            m = MagicMock()
            if cmd[1:] == ["--version"]:
                m.stdout = "openclaw 2026.3.5\n"
                m.stderr = ""
                m.returncode = 0
                return m
            if cmd[1:] == ["gateway", "status", "--json"]:
                # Non-zero exit but valid JSON stdout — should be parsed (I2 fix)
                m.stdout = '{"service":{"loaded":false,"runtime":{"status":"running","pid":999}},"version":"2026.3.5"}\n'
                m.stderr = "gateway cli failed"
                m.returncode = 1
                return m
            raise AssertionError(f"unexpected cmd: {cmd}")

        mock_run.side_effect = _run
        mock_urlopen.return_value = _Resp()

        got = system_metrics._collect_versions()
        self.assertEqual(got["openclaw"], "2026.3.5")
        self.assertEqual(got["gateway"]["status"], "online",
            "Valid JSON stdout should be parsed even on non-zero exit")
        self.assertIsNone(got["gateway"].get("error"), got["gateway"])
        # I2 fix: version is now parsed from stdout even on non-zero exit
        self.assertEqual(got["gateway"]["version"], "2026.3.5",
            "Version should be extracted from valid stdout on non-zero exit")

    @patch("urllib.request.urlopen")
    @patch("system_metrics.subprocess.run")
    def test_gateway_status_nonzero_no_json_falls_back_to_http(self, mock_run, mock_urlopen):
        """When CLI exits non-zero AND stdout has no valid JSON, fall back to HTTP probe."""
        class _Resp:
            status = 204
            def __enter__(self):
                return self
            def __exit__(self, exc_type, exc, tb):
                return False

        def _run(cmd, capture_output=True, text=True, timeout=None):
            m = MagicMock()
            if cmd[1:] == ["--version"]:
                m.stdout = "openclaw 2026.3.5\n"
                m.returncode = 0
                return m
            if cmd[1:] == ["gateway", "status", "--json"]:
                m.stdout = "Error: gateway not configured\n"  # no JSON
                m.stderr = "gateway cli failed"
                m.returncode = 1
                return m
            raise AssertionError(f"unexpected cmd: {cmd}")

        mock_run.side_effect = _run
        mock_urlopen.return_value = _Resp()

        got = system_metrics._collect_versions()
        self.assertEqual(got["gateway"]["status"], "online",
            "Should fall back to HTTP probe when no JSON in stdout")

    @patch("system_metrics.subprocess.run")
    def test_gateway_runtime_running_is_authoritative(self, mock_run):
        def _run(cmd, capture_output=True, text=True, timeout=None):
            m = MagicMock()
            if cmd[1:] == ["--version"]:
                m.stdout = "openclaw 2026.3.5\n"
                m.stderr = ""
                m.returncode = 0
                return m
            if cmd[1:] == ["gateway", "status", "--json"]:
                m.stdout = '{"service":{"loaded":false,"runtime":{"status":"running","pid":1234}},"version":"2026.3.5"}\n'
                m.stderr = ""
                m.returncode = 0
                return m
            if cmd[:3] == ["ps", "-o", "etime=,rss="]:
                m.stdout = "01:23 2048\n"
                m.stderr = ""
                m.returncode = 0
                return m
            raise AssertionError(f"unexpected cmd: {cmd}")

        mock_run.side_effect = _run

        got = system_metrics._collect_versions()
        self.assertEqual(got["gateway"]["status"], "online")
        self.assertEqual(got["gateway"]["pid"], 1234)
        self.assertEqual(got["gateway"]["uptime"], "01:23")

    @patch("shutil.which", return_value=None)
    def test_resolve_openclaw_bin_prefers_highest_asdf_version(self, _mock_which):
        import tempfile
        tmp_home = tempfile.mkdtemp(prefix="ocdash-home-")
        old_home = os.environ.get("HOME")
        try:
            os.environ["HOME"] = tmp_home
            versions = ["9.9.9", "10.0.0", "25.7.0", "25.8.0"]
            expected = None
            for ver in versions:
                bindir = os.path.join(tmp_home, ".asdf", "installs", "nodejs", ver, "bin")
                os.makedirs(bindir, exist_ok=True)
                target = os.path.join(bindir, "openclaw")
                with open(target, "w") as f:
                    f.write("#!/bin/sh\n")
                os.chmod(target, 0o755)
                if ver == "25.8.0":
                    expected = target
            self.assertEqual(system_metrics._resolve_openclaw_bin(), expected)
        finally:
            if old_home is None:
                os.environ.pop("HOME", None)
            else:
                os.environ["HOME"] = old_home


if __name__ == "__main__":
    unittest.main()
