import http.client
import json
import os
import re
import socket
import subprocess
import sys
import threading
import time
import unittest

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
INDEX_HTML = os.path.join(REPO, "index.html")
SERVER_PY = os.path.join(REPO, "server.py")
REFRESH_SH = os.path.join(REPO, "refresh.sh")
DATA_JSON = os.path.join(REPO, "data.json")


def _read(path):
    with open(path, "r", encoding="utf-8") as f:
        return f.read()


def _extract_script(html):
    m = re.search(r"<script>([\s\S]*)</script>", html)
    assert m, "<script> block not found in index.html"
    return m.group(1)


def _extract_render_body(js):
    m = re.search(r"render\s*\(\s*snap\s*,\s*flags\s*\)\s*\{([\s\S]*?)\n  \},", js)
    if not m:
        m = re.search(r"render\s*\(\s*snap\s*,\s*flags\s*\)\s*\{([\s\S]*?)\n\}", js)
    assert m, "Renderer.render(snap, flags) not found"
    return m.group(1)


def _extract_render_now_body(js):
    m = re.search(r"renderNow\s*\(\)\s*\{([\s\S]*?)\n\s*\}", js)
    assert m, "App.renderNow() not found"
    return m.group(1)


def _innerhtml_template_literals(js):
    return re.findall(r"innerHTML\s*=\s*`([\s\S]*?)`", js)


def _free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _request(port, method, path):
    conn = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    conn.request(method, path)
    resp = conn.getresponse()
    body = resp.read()
    headers = dict(resp.getheaders())
    status = resp.status
    conn.close()
    return status, headers, body


# ----------------------------
# Section 1 — Dirty-check logic
# ----------------------------

class TestDirtyCheck(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        cls.js = _extract_script(_read(INDEX_HTML))

    def test_tc1_section_changed_calls_use_non_empty_keys(self):
        calls = re.findall(r"sectionChanged\s*\(\s*\[(.*?)\]\s*\)", self.js, re.S)
        self.assertTrue(calls, "No sectionChanged([...]) calls found")
        for arr in calls:
            keys = re.findall(r"['\"]([^'\"]+)['\"]", arr)
            self.assertGreater(len(keys), 0,
                f"Found empty guarded key list: sectionChanged([{arr.strip()}])")

    def test_tc2_state_snapshot_deep_clones(self):
        self.assertIn("JSON.parse(JSON.stringify(", self.js)
        # commitPrev sets prev from frozen snapshot, not a live reference
        self.assertIn("this.prev = snap.data", self.js)
        self.assertNotIn("this.prev = this.data;", self.js)

    def test_tc3_stable_snapshot_exists_for_volatile_sections(self):
        self.assertIn("stableSnapshot", self.js)
        self.assertRegex(self.js, r"stableSnapshot\(D\.crons,",
            "crons stableSnapshot guard missing")
        self.assertRegex(self.js, r"stableSnapshot\(D\.sessions,",
            "sessions stableSnapshot guard missing")

    def test_tc4_commit_prev_called_after_render_inside_raf(self):
        render_now = _extract_render_now_body(self.js)
        i_render = render_now.find("Renderer.render(snap")
        i_commit = render_now.find("State.commitPrev(snap)")
        self.assertNotEqual(i_render, -1, "Renderer.render not found in renderNow")
        self.assertNotEqual(i_commit, -1, "State.commitPrev not found in renderNow")
        self.assertLess(i_render, i_commit,
            "commitPrev must be called after Renderer.render inside rAF")
        self.assertIn("requestAnimationFrame", render_now,
            "renderNow must use requestAnimationFrame")

    def test_tc5_render_now_uses_request_animation_frame(self):
        render_now = _extract_render_now_body(self.js)
        self.assertIn("requestAnimationFrame(", render_now)
        self.assertIn("Renderer.render(snap", render_now)
        self.assertIn("State.commitPrev(snap)", render_now)


# ----------------------------
# Section 2 — XSS coverage
# ----------------------------

class TestXSSCoverage(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        cls.js = _extract_script(_read(INDEX_HTML))

    def test_tc6_innerhtml_templates_include_esc_usage(self):
        templates = _innerhtml_template_literals(self.js)
        self.assertTrue(templates, "No innerHTML template-literal assignments found")
        risky_fields = r"(name|model|type|task|message|icon|severity|provider|id|hash|schedule|lastRun|nextRun|status|subject|label)"
        for tpl in templates:
            if "${" not in tpl:
                continue
            exprs = re.findall(r"\$\{([^}]*)\}", tpl)
            has_risky = any(re.search(rf"\.[ \t]*{risky_fields}\b", e.strip()) for e in exprs)
            if has_risky:
                self.assertIn("esc(", tpl,
                    "innerHTML template with risky interpolation missing esc()")

    def test_tc7_no_raw_unescaped_user_string_interpolation_in_innerhtml_templates(self):
        templates = _innerhtml_template_literals(self.js)
        suspicious = []
        risky_fields = r"(name|model|type|task|message|icon|severity|provider|id|hash|schedule|lastRun|nextRun|status|subject|label)"
        for tpl in templates:
            for expr in re.findall(r"\$\{([^}]*)\}", tpl):
                e = expr.strip()
                if "esc(" in e or "safeColor(" in e:
                    continue
                if re.search(rf"\.[ \t]*{risky_fields}\b", e):
                    suspicious.append(e)
        self.assertFalse(suspicious,
            f"Found potentially unsafe raw interpolations: {suspicious}")

    def test_tc8_safecolor_regex_accepts_only_hex_colors(self):
        m = re.search(r"return\s*/\^#\[0-9a-fA-F\]\{3,8\}\$/\.test", self.js)
        self.assertIsNotNone(m, "safeColor hex regex implementation not found")
        hex_re = re.compile(r"^#[0-9a-fA-F]{3,8}$")
        for v in ["#fff", "#ffffff", "#FFFFFF", "#12345678"]:
            self.assertRegex(v, hex_re, f"Expected valid hex color: {v}")
        for v in ["red", "#xyz", "#gg0000", "url(evil)"]:
            self.assertNotRegex(v, hex_re, f"Expected invalid color to be rejected: {v}")


# ----------------------------
# Section 3 — Server robustness
# ----------------------------

class TestServerRobustness(unittest.TestCase):

    port = None
    proc = None

    @classmethod
    def setUpClass(cls):
        cls.port = _free_port()
        env = os.environ.copy()
        env["DASHBOARD_BIND"] = "127.0.0.1"
        env["DASHBOARD_PORT"] = str(cls.port)
        cls.proc = subprocess.Popen(
            [sys.executable, SERVER_PY, "-b", "127.0.0.1", "-p", str(cls.port)],
            cwd=REPO,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            env=env,
        )
        started = False
        for _ in range(60):
            try:
                conn = http.client.HTTPConnection("127.0.0.1", cls.port, timeout=1)
                conn.request("GET", "/")
                r = conn.getresponse()
                r.read()
                conn.close()
                started = True
                break
            except Exception:
                time.sleep(0.1)
        if not started:
            cls.proc.terminate()
            raise RuntimeError("Server failed to start for tests")

    @classmethod
    def tearDownClass(cls):
        if cls.proc:
            cls.proc.terminate()
            try:
                cls.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                cls.proc.kill()

    def test_tc9_refresh_returns_valid_json(self):
        status, headers, body = _request(self.port, "GET", "/api/refresh")
        self.assertEqual(status, 200)
        self.assertTrue(body, "refresh response is empty")
        txt = body.decode("utf-8", errors="replace").strip()
        self.assertFalse(txt.lower().startswith("<!doctype html"),
            "refresh returned HTML, expected JSON")
        parsed = json.loads(txt)
        self.assertIsInstance(parsed, dict)
        self.assertTrue(headers.get("Content-Type", "").startswith("application/json"))

    def test_tc10_data_endpoint_has_numeric_total_cost_today(self):
        status, _, body = _request(self.port, "GET", "/api/data")
        if status != 200:
            status, _, body = _request(self.port, "GET", "/api/refresh")
        self.assertEqual(status, 200)
        data = json.loads(body.decode("utf-8", errors="replace"))
        self.assertIsInstance(data.get("totalCostToday"), (int, float),
            "totalCostToday must be numeric")

    def test_tc11_head_root_returns_200(self):
        status, _, _ = _request(self.port, "HEAD", "/")
        self.assertEqual(status, 200)

    def test_tc12_concurrent_load_no_500(self):
        port = self.port
        statuses = []
        errors = []
        lock = threading.Lock()

        def worker():
            local = []
            for _ in range(3):
                try:
                    st, _, _ = _request(port, "GET", "/")
                    local.append(st)
                except Exception as e:
                    with lock:
                        errors.append(str(e))
            with lock:
                statuses.extend(local)

        threads = [threading.Thread(target=worker) for _ in range(10)]
        for t in threads:
            t.start()
        for t in threads:
            t.join(timeout=15)

        self.assertFalse(errors, f"Request errors occurred: {errors}")
        self.assertEqual(len(statuses), 30,
            f"Expected 30 responses, got {len(statuses)}")
        self.assertTrue(all(s != 500 for s in statuses),
            f"Observed 500 under concurrent load: {statuses}")


# ----------------------------
# Section 4 — Data integrity
# ----------------------------

@unittest.skipUnless(os.path.exists(DATA_JSON), "data.json not found yet")
class TestDataIntegrity(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        with open(DATA_JSON, "r", encoding="utf-8") as f:
            cls.data = json.load(f)

    def test_tc13_projected_monthly_vs_today_with_outlier_tolerance(self):
        today = float(self.data.get("totalCostToday", 0) or 0)
        projected = float(self.data.get("projectedMonthly", 0) or 0)
        if projected >= today:
            return
        self.assertGreaterEqual(projected, today / 10.0,
            f"projectedMonthly ({projected}) is too low vs totalCostToday ({today})")

    def test_tc14_cron_schedule_field_shape_is_valid_expression(self):
        crons = self.data.get("crons", [])
        cron_re = re.compile(
            r"^(\*/\d+|\d{1,2})\s+(\*|\*/\d+|\d{1,2})\s+\*\s+\*\s+(\*|\d{1,2}(?:,\d{1,2})*)$")
        iso_at_re = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$")
        for c in crons:
            sched = str(c.get("schedule", "")).strip()
            ok = bool(
                cron_re.match(sched)
                or sched.startswith("Every ")
                or sched.startswith("At ")
                or iso_at_re.match(sched)
            )
            self.assertTrue(ok, f"Invalid cron schedule format: {sched!r}")

    def test_tc15_sessions_count_reasonable_vs_active_sessions_field(self):
        sessions = self.data.get("sessions", [])
        active_count_field = self.data.get(
            "activeSessions", self.data.get("sessionCount", len(sessions)))
        self.assertIsInstance(active_count_field, int)
        self.assertGreaterEqual(active_count_field, 0)
        self.assertIsInstance(sessions, list)
        if active_count_field > 0:
            self.assertGreater(len(sessions), 0,
                "activeSessions > 0 but sessions list is empty")

    def test_tc16_daily_chart_is_chronological(self):
        chart = self.data.get("dailyChart", [])
        prev = None
        for entry in chart:
            d = entry.get("date")
            self.assertIsNotNone(re.match(r"^\d{4}-\d{2}-\d{2}$", str(d or "")),
                f"Bad date: {d!r}")
            if prev is not None:
                self.assertGreaterEqual(d, prev,
                    f"dailyChart not chronological: {d} < {prev}")
            prev = d


# ----------------------------
# Section 5 — refresh.sh safety
# ----------------------------

class TestRefreshScript(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        cls.sh = _read(REFRESH_SH)
        m = re.search(
            r"<<\s*'\s*PYEOF\s*'\s*>\s*\"\$DIR/data\.json\.tmp\"\n([\s\S]*?)\nPYEOF",
            cls.sh)
        if not m:
            m = re.search(r"<<\s*'?PYEOF'?([\s\S]*?)\nPYEOF", cls.sh)
        assert m, "Embedded Python heredoc not found in refresh.sh"
        cls.py = m.group(1)

    def test_tc17_uses_pgrep_not_ps_aux_grep_antipattern(self):
        self.assertIn("pgrep", self.sh, "Expected pgrep usage for process detection")
        anti = re.search(r"ps\s+aux\s*\|\s*grep[\s\S]*grep\s+-v\s+grep", self.sh)
        self.assertIsNone(anti, "Found ps aux | grep ... | grep -v grep anti-pattern")

    def test_tc18_with_open_uses_as_keyword_in_embedded_python(self):
        with_open_lines = re.findall(r"^\s*with\s+open\([^\n]*$", self.py, re.M)
        self.assertTrue(with_open_lines, "No with open(...) usages found")
        for line in with_open_lines:
            self.assertIn(" as ", line,
                f"with open missing 'as' context binding: {line.strip()}")

    def test_tc19_no_import_star_in_embedded_python(self):
        self.assertIsNone(
            re.search(r"^\s*from\s+\S+\s+import\s+\*", self.py, re.M))

    def test_tc20_embedded_python_imports_os_for_getpid(self):
        self.assertIsNotNone(
            re.search(r"^\s*import\s+.*\bos\b", self.py, re.M),
            "import os missing in embedded Python")
        self.assertIn("os.getpid()", self.py,
            "os.getpid() usage expected for self PID exclusion")


if __name__ == "__main__":
    unittest.main()
