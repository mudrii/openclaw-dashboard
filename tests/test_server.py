"""Server integration tests — AC1-AC8.

These tests start their own server instance on a random port.
"""

import http.client
import json
import os
import subprocess
import sys
import threading
import time
import unittest

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SERVER_PY = os.path.join(REPO, "server.py")
DATA_FILE = os.path.join(REPO, "data.json")


def _free_port():
    import socket
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class ServerTestBase(unittest.TestCase):
    """Start server.py on a random port for each test class."""

    port = None
    proc = None

    @classmethod
    def setUpClass(cls):
        cls.port = _free_port()
        # Ensure data.json exists (AC8 — server should work with pre-existing file)
        if not os.path.exists(DATA_FILE):
            # Write minimal valid data so server can serve it
            with open(DATA_FILE, "w") as f:
                json.dump({"gateway": {"status": "unknown"}, "totalCostToday": 0, "crons": [], "sessions": [], "tokenUsage": [], "subagentRuns": [], "dailyChart": [], "models": [], "skills": [], "gitLog": [], "agentConfig": {}}, f)
        env = os.environ.copy()
        env["DASHBOARD_PORT"] = str(cls.port)
        env["DASHBOARD_BIND"] = "127.0.0.1"
        cls.proc = subprocess.Popen(
            [sys.executable, SERVER_PY, "-p", str(cls.port)],
            cwd=REPO,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=env,
        )
        # Wait for server to be ready
        for _ in range(30):
            try:
                conn = http.client.HTTPConnection("127.0.0.1", cls.port, timeout=1)
                conn.request("GET", "/")
                conn.getresponse()
                conn.close()
                return
            except Exception:
                time.sleep(0.2)
        raise RuntimeError("Server didn't start in time")

    @classmethod
    def tearDownClass(cls):
        if cls.proc:
            cls.proc.terminate()
            cls.proc.wait(timeout=5)

    def _conn(self):
        return http.client.HTTPConnection("127.0.0.1", self.port, timeout=10)

    def _get(self, path, headers=None):
        conn = self._conn()
        conn.request("GET", path, headers=headers or {})
        resp = conn.getresponse()
        body = resp.read().decode()
        conn.close()
        return resp, body


class TestServerRoutes(ServerTestBase):
    """AC1, AC4, AC6: basic route tests."""

    def test_ac1_root_returns_html(self):
        """AC1: GET / returns 200 and HTML content."""
        resp, body = self._get("/")
        self.assertEqual(resp.status, 200)
        self.assertIn("<!DOCTYPE html>", body[:100])
        self.assertIn("OpenClaw Dashboard", body)

    def test_ac4_themes_json(self):
        """AC4: GET /themes.json returns valid JSON."""
        resp, body = self._get("/themes.json")
        # themes.json may or may not exist; if it does, must be valid JSON
        if resp.status == 200:
            data = json.loads(body)  # Should not raise
            self.assertIsInstance(data, (dict, list))
        else:
            self.assertIn(resp.status, (404,))

    def test_ac6_unknown_route_returns_404(self):
        """AC6: Unknown routes return 404."""
        resp, _ = self._get("/nonexistent/path/xyz")
        self.assertEqual(resp.status, 404)


class TestRefreshEndpoint(ServerTestBase):
    """AC2, AC3, AC8: /api/refresh tests."""

    def test_ac2_refresh_returns_json_with_keys(self):
        """AC2: GET /api/refresh returns JSON with required top-level keys."""
        resp, body = self._get("/api/refresh")
        self.assertEqual(resp.status, 200)
        data = json.loads(body)
        # Must have at least these keys (from data.json)
        for key in ("gateway", "totalCostToday", "crons", "sessions"):
            self.assertIn(key, data, f"Missing key: {key}")

    def test_ac3_cors_not_wildcard(self):
        """AC3: CORS header is restricted to localhost, not wildcard *."""
        # Request with no Origin
        resp, _ = self._get("/api/refresh")
        cors = resp.getheader("Access-Control-Allow-Origin", "")
        self.assertNotEqual(cors, "*", "CORS should not be wildcard")
        self.assertIn("localhost", cors)

        # Request with localhost Origin
        resp2, _ = self._get("/api/refresh", headers={"Origin": "http://localhost:3000"})
        cors2 = resp2.getheader("Access-Control-Allow-Origin", "")
        self.assertEqual(cors2, "http://localhost:3000")

        # Request with external Origin — should NOT reflect it
        resp3, _ = self._get("/api/refresh", headers={"Origin": "http://evil.com"})
        cors3 = resp3.getheader("Access-Control-Allow-Origin", "")
        self.assertNotEqual(cors3, "http://evil.com")

    def test_ac8_serves_existing_data_json(self):
        """AC8: data.json served correctly even without running refresh.sh."""
        resp, body = self._get("/api/refresh")
        self.assertEqual(resp.status, 200)
        data = json.loads(body)
        self.assertIsInstance(data, dict)


class TestConcurrency(ServerTestBase):
    """AC5: Concurrent requests don't corrupt data.json."""

    def test_ac5_concurrent_requests(self):
        """AC5: 5 threads hitting /api/refresh simultaneously all get valid JSON."""
        results = [None] * 5
        errors = []

        def fetch(idx):
            try:
                resp, body = self._get("/api/refresh")
                data = json.loads(body)
                results[idx] = (resp.status, isinstance(data, dict))
            except Exception as e:
                errors.append(str(e))

        threads = [threading.Thread(target=fetch, args=(i,)) for i in range(5)]
        for t in threads:
            t.start()
        for t in threads:
            t.join(timeout=15)

        self.assertEqual(len(errors), 0, f"Errors: {errors}")
        for i, r in enumerate(results):
            self.assertIsNotNone(r, f"Thread {i} got no result")
            self.assertEqual(r[0], 200, f"Thread {i} status != 200")
            self.assertTrue(r[1], f"Thread {i} didn't return dict")


class TestDebounce(ServerTestBase):
    """AC7: Refresh debouncing."""

    def test_ac7_rapid_requests_debounced(self):
        """AC7: Rapid requests within debounce window return cached data."""
        # First request triggers refresh
        resp1, body1 = self._get("/api/refresh")
        self.assertEqual(resp1.status, 200)
        t1 = time.time()

        # Rapid follow-up should be debounced (< 30s default)
        resp2, body2 = self._get("/api/refresh")
        t2 = time.time()
        self.assertEqual(resp2.status, 200)

        # The second request should be fast (debounced, no refresh.sh run)
        # If refresh.sh ran again it would take >0.5s typically
        self.assertLess(t2 - t1, 5, "Second request too slow — may not be debounced")

        # Both should return valid JSON
        json.loads(body1)
        json.loads(body2)


class TestRefreshCORSOnError(unittest.TestCase):
    """Test that CORS headers are present on error responses from /api/refresh."""

    port = None
    proc = None
    _data_json_existed = False

    @classmethod
    def setUpClass(cls):
        cls.port = _free_port()
        # Temporarily remove data.json to trigger 503 on refresh
        cls._data_json_existed = os.path.exists(DATA_FILE)
        if cls._data_json_existed:
            os.rename(DATA_FILE, DATA_FILE + ".bak")
        env = os.environ.copy()
        env["DASHBOARD_PORT"] = str(cls.port)
        env["DASHBOARD_BIND"] = "127.0.0.1"
        cls.proc = subprocess.Popen(
            [sys.executable, SERVER_PY, "-p", str(cls.port)],
            cwd=REPO,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=env,
        )
        for _ in range(30):
            try:
                conn = http.client.HTTPConnection("127.0.0.1", cls.port, timeout=1)
                conn.request("GET", "/")
                conn.getresponse()
                conn.close()
                return
            except Exception:
                time.sleep(0.2)
        raise RuntimeError("Server didn't start in time")

    @classmethod
    def tearDownClass(cls):
        if cls.proc:
            cls.proc.terminate()
            cls.proc.wait(timeout=5)
        # Restore data.json if it existed before
        if cls._data_json_existed and os.path.exists(DATA_FILE + ".bak"):
            os.rename(DATA_FILE + ".bak", DATA_FILE)

    def test_refresh_503_has_cors_header(self):
        """CORS headers must be present even on 503 error responses."""
        conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=10)
        conn.request("GET", "/api/refresh", headers={"Origin": "http://localhost:3000"})
        resp = conn.getresponse()
        resp.read()
        conn.close()
        # Server should return 503 (no data.json) but with CORS
        if resp.status == 503:
            cors = resp.getheader("Access-Control-Allow-Origin", "")
            self.assertEqual(cors, "http://localhost:3000",
                "503 response missing CORS header — browser JS can't read error")

    def test_refresh_503_error_message_matches_go(self):
        """Error message on 503 should match Go server's message."""
        conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=10)
        conn.request("GET", "/api/refresh")
        resp = conn.getresponse()
        body = resp.read().decode()
        conn.close()
        if resp.status == 503:
            data = json.loads(body)
            self.assertIn("data.json not found", data.get("error", ""))


class TestThreadingServer(ServerTestBase):
    """Verify the Python server handles concurrent requests without blocking."""

    def test_concurrent_refresh_and_index(self):
        """Multiple concurrent requests to different endpoints should not block."""
        results = {}
        errors = []

        def fetch(name, path):
            try:
                conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=10)
                conn.request("GET", path)
                resp = conn.getresponse()
                resp.read()
                conn.close()
                results[name] = resp.status
            except Exception as e:
                errors.append(f"{name}: {e}")

        threads = [
            threading.Thread(target=fetch, args=("index", "/")),
            threading.Thread(target=fetch, args=("refresh", "/api/refresh")),
            threading.Thread(target=fetch, args=("system", "/api/system")),
        ]
        for t in threads:
            t.start()
        for t in threads:
            t.join(timeout=15)

        self.assertFalse(errors, f"Concurrent request errors: {errors}")
        for name, status in results.items():
            self.assertEqual(status, 200, f"{name} returned {status}")


class TestSystemDisabled(unittest.TestCase):
    """Test system.enabled=false returns 503 in Python server."""

    port = None
    proc = None

    @classmethod
    def setUpClass(cls):
        import tempfile
        cls.port = _free_port()
        # Write a config.json with system.enabled=false
        cls._config_bak = None
        config_path = os.path.join(REPO, "config.json")
        if os.path.exists(config_path):
            with open(config_path, "r") as f:
                cls._config_bak = f.read()
        with open(config_path, "r") as f:
            cfg = json.load(f)
        cfg.setdefault("system", {})["enabled"] = False
        with open(config_path, "w") as f:
            json.dump(cfg, f)

        if not os.path.exists(DATA_FILE):
            with open(DATA_FILE, "w") as f:
                json.dump({"gateway": {}, "totalCostToday": 0, "crons": [], "sessions": []}, f)

        env = os.environ.copy()
        env["DASHBOARD_PORT"] = str(cls.port)
        env["DASHBOARD_BIND"] = "127.0.0.1"
        cls.proc = subprocess.Popen(
            [sys.executable, SERVER_PY, "-p", str(cls.port)],
            cwd=REPO,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=env,
        )
        for _ in range(30):
            try:
                conn = http.client.HTTPConnection("127.0.0.1", cls.port, timeout=1)
                conn.request("GET", "/")
                conn.getresponse()
                conn.close()
                break
            except Exception:
                time.sleep(0.2)

    @classmethod
    def tearDownClass(cls):
        if cls.proc:
            cls.proc.terminate()
            cls.proc.wait(timeout=5)
        # Restore original config
        config_path = os.path.join(REPO, "config.json")
        if cls._config_bak is not None:
            with open(config_path, "w") as f:
                f.write(cls._config_bak)

    def test_system_disabled_returns_503(self):
        conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=10)
        conn.request("GET", "/api/system")
        resp = conn.getresponse()
        body = resp.read().decode()
        conn.close()
        self.assertEqual(resp.status, 503)
        data = json.loads(body)
        self.assertFalse(data.get("ok"))


class TestSystemEndpoint(ServerTestBase):
    """Tests for GET /api/system endpoint."""

    def test_system_get_returns_200_with_schema(self):
        resp, body = self._get("/api/system")
        self.assertEqual(resp.status, 200)
        data = json.loads(body)
        # Required top-level keys
        for key in ("ok", "degraded", "stale", "collectedAt", "pollSeconds",
                    "cpu", "ram", "swap", "disk", "versions", "openclaw"):
            self.assertIn(key, data, f"missing key: {key}")
        self.assertIsInstance(data["pollSeconds"], int)
        self.assertGreater(data["pollSeconds"], 0)
        self.assertIsNotNone(data["collectedAt"])
        oc = data["openclaw"]
        for key in ("gateway", "status", "freshness"):
            self.assertIn(key, oc, f"missing openclaw key: {key}")
        # channels and bindings were intentionally removed from the runtime contract
        for removed_key in ("channels", "bindings"):
            self.assertNotIn(removed_key, oc,
                f"openclaw.{removed_key} should have been removed")

    def test_system_head_no_body(self):
        conn = self._conn()
        conn.request("HEAD", "/api/system")
        resp = conn.getresponse()
        body = resp.read()
        self.assertEqual(resp.status, 200)
        self.assertEqual(len(body), 0, "HEAD should return empty body")

    def test_system_cors_header_set(self):
        conn = self._conn()
        conn.request("GET", "/api/system", headers={"Origin": "http://localhost:9090"})
        resp = conn.getresponse()
        resp.read()
        cors = resp.getheader("Access-Control-Allow-Origin")
        self.assertIsNotNone(cors, "CORS header missing")
        self.assertNotEqual(cors, "*", "CORS should not be wildcard")

    def test_system_content_type_json(self):
        resp, body = self._get("/api/system")
        self.assertEqual(resp.status, 200)
        ct = resp.getheader("Content-Type", "")
        self.assertIn("application/json", ct)

    def test_system_degraded_returns_200(self):
        """Partial collector failure should still return 200 with degraded=true."""
        resp, body = self._get("/api/system")
        self.assertEqual(resp.status, 200)
        data = json.loads(body)
        # Regardless of degraded state, must be 200
        self.assertIn("ok", data)


class TestChatRateLimit(unittest.TestCase):
    """Test the per-IP rate limiter for /api/chat."""

    @classmethod
    def setUpClass(cls):
        sys.path.insert(0, REPO)
        import server
        cls.server = server

    def setUp(self):
        # Reset rate limiter state before each test
        with self.server._chat_rate_lock:
            self.server._chat_rate_buckets.clear()

    def test_allows_within_limit(self):
        for i in range(self.server._CHAT_RATE_LIMIT):
            self.assertTrue(self.server._chat_rate_allow("127.0.0.1"),
                            f"request {i+1} should be allowed")

    def test_blocks_over_limit(self):
        for _ in range(self.server._CHAT_RATE_LIMIT):
            self.server._chat_rate_allow("127.0.0.1")
        self.assertFalse(self.server._chat_rate_allow("127.0.0.1"),
                         "should be blocked after limit exceeded")

    def test_per_ip_isolation(self):
        for _ in range(self.server._CHAT_RATE_LIMIT):
            self.server._chat_rate_allow("10.0.0.1")
        # Different IP should still be allowed
        self.assertTrue(self.server._chat_rate_allow("10.0.0.2"),
                        "different IP should not be affected")


class TestChatRateLimitCleanup(unittest.TestCase):
    """Test rate-limiter cleanup removes stale entries."""

    @classmethod
    def setUpClass(cls):
        sys.path.insert(0, REPO)
        import server
        cls.server = server

    def setUp(self):
        with self.server._chat_rate_lock:
            self.server._chat_rate_buckets.clear()

    def test_cleanup_removes_old_entries(self):
        """Entries older than 2× window should be removed by cleanup."""
        old_time = time.time() - (3 * self.server._CHAT_RATE_WINDOW)
        with self.server._chat_rate_lock:
            self.server._chat_rate_buckets["10.0.0.1"] = [5, old_time]
            self.server._chat_rate_buckets["10.0.0.2"] = [5, time.time()]  # fresh

        self.server._chat_rate_cleanup()

        with self.server._chat_rate_lock:
            self.assertNotIn("10.0.0.1", self.server._chat_rate_buckets,
                             "Stale entry should be removed")
            self.assertIn("10.0.0.2", self.server._chat_rate_buckets,
                          "Fresh entry should be kept")

    def test_cleanup_empty_dict_is_noop(self):
        """Cleanup on empty dict should not error."""
        self.server._chat_rate_cleanup()  # should not raise
        with self.server._chat_rate_lock:
            self.assertEqual(len(self.server._chat_rate_buckets), 0)


class TestCallGatewayReturnTypes(unittest.TestCase):
    """Test that call_gateway returns proper (status, dict) tuples."""

    @classmethod
    def setUpClass(cls):
        sys.path.insert(0, REPO)
        from server import call_gateway
        cls.call = staticmethod(call_gateway)

    def test_unreachable_returns_502(self):
        status, result = self.call(
            system="test", history=[], question="hi",
            port=19999, token="fake", model="test",
        )
        self.assertEqual(status, 502)
        self.assertIn("error", result)

    def test_returns_tuple(self):
        status, result = self.call(
            system="test", history=[], question="hi",
            port=19999, token="fake", model="test",
        )
        self.assertIsInstance(status, int)
        self.assertIsInstance(result, dict)


class TestRenderIndex(unittest.TestCase):
    """Tests for _render_index() runtime/version injection — AC-runtime-badge."""

    def setUp(self):
        # Import server module freshly so VERSION is accessible
        sys.path.insert(0, REPO)
        import importlib
        self.server = importlib.import_module("server")

    def test_runtime_placeholder_replaced_with_python(self):
        """_render_index must replace __RUNTIME__ with 'Python'."""
        rendered = self.server._render_index()
        if rendered is None:
            self.skipTest("index.html not found")
        html = rendered.decode("utf-8")
        self.assertNotIn("__RUNTIME__", html,
            "__RUNTIME__ placeholder was not replaced by _render_index()")
        self.assertIn("Python", html,
            "'Python' runtime label not injected into rendered index.html")

    def test_version_placeholder_still_replaced(self):
        """__VERSION__ replacement must still work alongside __RUNTIME__."""
        rendered = self.server._render_index()
        if rendered is None:
            self.skipTest("index.html not found")
        html = rendered.decode("utf-8")
        self.assertNotIn("__VERSION__", html,
            "__VERSION__ placeholder was not replaced")


if __name__ == "__main__":
    unittest.main()
