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

    def _get(self, path, headers=None):
        conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=10)
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


class TestAddressInUse(unittest.TestCase):
    """Port-conflict handling: error on explicit -p, auto-advance otherwise."""

    def _occupy_port(self):
        """Bind a random port and return (socket, port)."""
        import socket as _socket
        sock = _socket.socket(_socket.AF_INET, _socket.SOCK_STREAM)
        sock.setsockopt(_socket.SOL_SOCKET, _socket.SO_REUSEADDR, 1)
        sock.bind(("127.0.0.1", 0))
        sock.listen(1)
        return sock, sock.getsockname()[1]

    def test_explicit_port_exits_with_error(self):
        """When -p is passed explicitly and port is taken, exit 1."""
        sock, port = self._occupy_port()
        try:
            proc = subprocess.run(
                [sys.executable, SERVER_PY, "-b", "127.0.0.1", "-p", str(port)],
                cwd=REPO,
                capture_output=True,
                text=True,
                timeout=10,
            )
            self.assertEqual(proc.returncode, 1)
            self.assertIn("already in use", proc.stderr)
        finally:
            sock.close()

    def test_default_port_auto_advances(self):
        """When no -p given, server advances to next available port."""
        sock, port = self._occupy_port()
        try:
            env = os.environ.copy()
            env["DASHBOARD_PORT"] = str(port)
            env["DASHBOARD_BIND"] = "127.0.0.1"
            proc = subprocess.Popen(
                [sys.executable, "-u", SERVER_PY],
                cwd=REPO,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                env=env,
            )
            next_port = port + 1
            for _ in range(30):
                try:
                    conn = http.client.HTTPConnection("127.0.0.1", next_port, timeout=1)
                    conn.request("GET", "/")
                    conn.getresponse()
                    conn.close()
                    break
                except Exception:
                    time.sleep(0.2)
            else:
                proc.terminate()
                proc.wait(timeout=5)
                self.fail("Server didn't start on next port")
            proc.terminate()
            proc.wait(timeout=5)
            stdout = proc.stdout.read().decode()
            self.assertIn(f"Port {port} in use", stdout)
            self.assertIn(f":{next_port}/", stdout)
        finally:
            sock.close()


if __name__ == "__main__":
    unittest.main()
