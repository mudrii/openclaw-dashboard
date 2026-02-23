"""AI chat endpoint tests — AC-CHAT-1 through AC-CHAT-8."""

import http.client
import json
import os
import subprocess
import sys
import tempfile
import threading
import time
import unittest

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CONFIG_FILE = os.path.join(REPO, "config.json")
SERVER_PY = os.path.join(REPO, "server.py")
DATA_FILE = os.path.join(REPO, "data.json")


def _free_port():
    import socket
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


# ── Unit tests (no server needed) ──────────────────────────────────────────

class TestAiConfig(unittest.TestCase):
    """AC-CHAT-1: config.json has ai section with required keys."""

    def test_ac_chat_1_ai_config_has_required_keys(self):
        with open(CONFIG_FILE) as f:
            cfg = json.load(f)
        ai = cfg.get("ai", {})
        self.assertIsInstance(ai, dict)
        self.assertIn("gatewayPort", ai)
        self.assertIn("model", ai)
        self.assertIn("maxHistory", ai)
        self.assertIsInstance(ai["gatewayPort"], int)
        self.assertIsInstance(ai["maxHistory"], int)
        self.assertGreater(ai["maxHistory"], 0)


class TestReadDotenv(unittest.TestCase):
    """AC-CHAT-2: read_dotenv() parses KEY=VALUE files correctly."""

    @classmethod
    def setUpClass(cls):
        sys.path.insert(0, REPO)
        from server import read_dotenv
        cls.read_dotenv = staticmethod(read_dotenv)

    def _write_env(self, content):
        f = tempfile.NamedTemporaryFile(mode='w', suffix='.env', delete=False)
        f.write(content)
        f.flush()
        f.close()
        return f.name

    def test_ac_chat_2a_parses_key_value(self):
        path = self._write_env("FOO=bar\nBAZ=qux\n")
        result = self.read_dotenv(path)
        self.assertEqual(result.get("FOO"), "bar")
        self.assertEqual(result.get("BAZ"), "qux")

    def test_ac_chat_2b_ignores_comments_and_blanks(self):
        path = self._write_env("# comment\n\nKEY=value\n")
        result = self.read_dotenv(path)
        self.assertNotIn("# comment", result)
        self.assertIn("KEY", result)

    def test_ac_chat_2c_missing_file_returns_empty(self):
        result = self.read_dotenv("/nonexistent/path/.env")
        self.assertEqual(result, {})

    def test_ac_chat_2d_values_with_equals_sign(self):
        path = self._write_env("TOKEN=abc=def=ghi\n")
        result = self.read_dotenv(path)
        self.assertEqual(result.get("TOKEN"), "abc=def=ghi")


class TestBuildPrompt(unittest.TestCase):
    """AC-CHAT-3: build_dashboard_prompt() produces structured context."""

    @classmethod
    def setUpClass(cls):
        sys.path.insert(0, REPO)
        from server import build_dashboard_prompt
        cls.build = staticmethod(build_dashboard_prompt)

    def _data(self, **overrides):
        base = {
            "gateway": {"status": "online", "uptime": "2h", "memory": "120 MB", "pid": 1234},
            "totalCostToday": 0.42, "totalCostAllTime": 18.5,
            "projectedMonthly": 12.0, "subagentCostToday": 0.05,
            "sessionCount": 3,
            "sessions": [{"name": "test", "model": "kimi", "type": "dm", "contextPct": 40}],
            "crons": [{"name": "daily", "lastStatus": "ok", "schedule": "0 9 * * *"}],
            "alerts": [], "costBreakdown": [{"model": "kimi", "cost": 0.42}],
            "agentConfig": {"primaryModel": "kimi/k2p5", "fallbacks": []},
            "lastRefresh": "14:32:00",
        }
        base.update(overrides)
        return base

    def test_ac_chat_3a_contains_gateway_status(self):
        self.assertIn("online", self.build(self._data()).lower())

    def test_ac_chat_3b_contains_cost_today(self):
        self.assertIn("0.42", self.build(self._data()))

    def test_ac_chat_3c_contains_alerts_section(self):
        self.assertIn("ALERTS", self.build(self._data(alerts=[])).upper())

    def test_ac_chat_3d_handles_empty_data(self):
        try:
            prompt = self.build({})
            self.assertIsInstance(prompt, str)
        except KeyError as e:
            self.fail(f"build_dashboard_prompt crashed on empty data: {e}")

    def test_ac_chat_3e_failed_cron_highlighted(self):
        data = self._data(crons=[
            {"name": "backup", "lastStatus": "error",
             "schedule": "0 1 * * *", "lastError": "timeout"}
        ])
        prompt = self.build(data)
        self.assertIn("error", prompt.lower())
        self.assertIn("backup", prompt)


class TestCallGateway(unittest.TestCase):
    """AC-CHAT-4: call_gateway() handles unreachable gateway gracefully."""

    @classmethod
    def setUpClass(cls):
        sys.path.insert(0, REPO)
        from server import call_gateway
        cls.call = staticmethod(call_gateway)

    def test_ac_chat_4a_unreachable_returns_error(self):
        result = self.call(
            system="You are a helper.", history=[], question="hi",
            port=19999, token="fake", model="test",
        )
        self.assertIn("error", result)
        self.assertIsInstance(result["error"], str)

    def test_ac_chat_4b_always_returns_dict(self):
        result = self.call(
            system="You are a helper.", history=[], question="hi",
            port=19999, token="fake", model="test",
        )
        self.assertIsInstance(result, dict)
        self.assertTrue("answer" in result or "error" in result)


# ── Integration tests (real server subprocess) ─────────────────────────────

class ChatServerBase(unittest.TestCase):
    """Start server.py on a random port for chat endpoint tests."""

    port = None
    proc = None

    @classmethod
    def setUpClass(cls):
        cls.port = _free_port()
        if not os.path.exists(DATA_FILE):
            with open(DATA_FILE, "w") as f:
                json.dump({
                    "gateway": {"status": "online", "uptime": "1h",
                                "memory": "100 MB", "pid": 1},
                    "totalCostToday": 0.50, "totalCostAllTime": 10.0,
                    "projectedMonthly": 15.0, "subagentCostToday": 0.0,
                    "sessionCount": 1, "sessions": [], "crons": [],
                    "alerts": [], "costBreakdown": [],
                    "agentConfig": {}, "lastRefresh": "now",
                }, f)
        cls.proc = subprocess.Popen(
            [sys.executable, SERVER_PY, "-p", str(cls.port)],
            cwd=REPO, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
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

    def _post(self, path, body):
        conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=15)
        encoded = json.dumps(body).encode() if isinstance(body, dict) else body
        conn.request("POST", path, body=encoded,
                     headers={"Content-Type": "application/json",
                              "Content-Length": str(len(encoded))})
        resp = conn.getresponse()
        raw = resp.read().decode()
        conn.close()
        return resp, raw


class TestChatEndpoint(ChatServerBase):
    """AC-CHAT-5 through AC-CHAT-8: /api/chat HTTP endpoint tests."""

    def test_ac_chat_5_returns_200_with_answer_or_error(self):
        resp, body = self._post("/api/chat", {"question": "What is today's cost?"})
        self.assertEqual(resp.status, 200)
        data = json.loads(body)
        self.assertTrue("answer" in data or "error" in data,
                        f"Missing answer/error key: {data}")

    def test_ac_chat_6_empty_question_returns_400(self):
        resp, _ = self._post("/api/chat", {"question": ""})
        self.assertEqual(resp.status, 400)

    def test_ac_chat_7_missing_question_key_returns_400(self):
        resp, _ = self._post("/api/chat", {"msg": "hi"})
        self.assertEqual(resp.status, 400)

    def test_ac_chat_8_bad_json_returns_400(self):
        conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=5)
        conn.request("POST", "/api/chat", body=b"not json",
                     headers={"Content-Type": "application/json",
                              "Content-Length": "8"})
        resp = conn.getresponse()
        conn.close()
        self.assertEqual(resp.status, 400)


if __name__ == "__main__":
    unittest.main()
