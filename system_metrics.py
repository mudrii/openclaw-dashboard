"""
system_metrics.py — host metrics collector for openclaw-dashboard.

Cross-platform (macOS/darwin + Linux).
No external dependencies — stdlib + subprocess only.
"""

import json
import logging
import os
import platform
import re
import shutil
import subprocess
import threading
import time
import urllib.request
from typing import Optional

_log = logging.getLogger(__name__)

# ── version (set by server.py before use) ──────────────────────────────────
_dashboard_version: str = "unknown"

# ── metrics cache — mutable container avoids globals() anti-pattern ────────
class _MetricsState:
    lock = threading.Lock()
    payload: Optional[bytes] = None
    at: float = 0.0
    refreshing: bool = False

class _VersionsState:
    lock = threading.Lock()
    cache: Optional[dict] = None
    at: float = 0.0
    refreshing: bool = False  # True while a thread is calling _collect_versions

_ms = _MetricsState()
_vs = _VersionsState()

# ── config (set by server.py at startup) ──────────────────────────────────
_cfg: dict = {
    "enabled": True,
    "pollSeconds": 5,
    "metricsTtlSeconds": 5,
    "versionsTtlSeconds": 300,
    "gatewayTimeoutMs": 5000,
    "diskPath": "/",
    "warnPercent": 70,
    "criticalPercent": 85,
}


def configure(cfg: dict) -> None:
    """Called by server.py at startup with config.system values."""
    global _cfg, _dashboard_version
    _cfg.update(cfg)


def set_version(version: str) -> None:
    global _dashboard_version
    _dashboard_version = version


# ── public API ─────────────────────────────────────────────────────────────

def get_payload() -> tuple[int, bytes]:
    """
    Returns (http_status_code, json_bytes).
    Uses TTL cache with stale-serving semantics:
      - Fresh → 200 (cached)
      - Stale → 200 stale=true + background refresh
      - No cache + hard fail → 503
    """
    if not _cfg.get("enabled", True):
        body = json.dumps({"ok": False, "error": "system metrics disabled"}).encode()
        return 503, body

    ttl = _cfg.get("metricsTtlSeconds", 5)
    now = time.monotonic()

    with _ms.lock:
        payload = _ms.payload
        at = _ms.at
        refreshing = _ms.refreshing
        fresh = payload is not None and (now - at) < ttl
        has_stale = payload is not None

    if fresh:
        return 200, payload

    if has_stale:
        # Return stale immediately; trigger background refresh if not already running
        should_start = False
        if not refreshing:
            with _ms.lock:
                if not _ms.refreshing:
                    _ms.refreshing = True
                    should_start = True
        if should_start:
            threading.Thread(target=_bg_refresh, daemon=True).start()
        # B2 fix: safe JSON parse/modify/serialize instead of fragile byte replacement.
        # The old byte-level approach silently served stale data as fresh when the
        # exact byte pattern didn't match (e.g. different json.dumps spacing).
        try:
            stale_data = json.loads(payload)
            stale_data["stale"] = True
            stale_payload = json.dumps(stale_data).encode()
        except (json.JSONDecodeError, TypeError):
            # Last resort: serve original payload unchanged (still stale but marked fresh)
            _log.warning("[system] failed to parse cached payload for stale injection")
            stale_payload = payload
        return 200, stale_payload

    # No cache — collect synchronously
    data = _collect_all()
    if data is None:
        body = json.dumps({"ok": False, "degraded": True, "error": "system metrics unavailable"}).encode()
        return 503, body
    return 200, data


# ── internal ───────────────────────────────────────────────────────────────

def _bg_refresh() -> None:
    try:
        _collect_all()
    except Exception:
        _log.exception("[system] background refresh failed")
    finally:
        with _ms.lock:
            _ms.refreshing = False


def _collect_all() -> Optional[bytes]:

    sys_name = platform.system().lower()
    errors = []

    # ── CPU ──
    cpu = _collect_cpu(sys_name)
    if cpu.get("error"):
        errors.append("cpu: " + cpu["error"])

    # ── RAM ──
    ram = _collect_ram(sys_name)
    if ram.get("error"):
        errors.append("ram: " + ram["error"])

    # ── Swap ──
    swap = _collect_swap(sys_name)
    if swap.get("error"):
        errors.append("swap: " + swap["error"])

    # ── Disk ──
    disk = _collect_disk(_cfg.get("diskPath", "/"))
    if disk.get("error"):
        errors.append("disk: " + disk["error"])

    # ── Versions + OpenClaw runtime observability ──
    versions = _get_versions_cached()
    openclaw = _collect_openclaw_runtime(_resolve_openclaw_bin(), versions)
    if openclaw.get("errors"):
        errors.extend([f"openclaw: {e}" for e in openclaw.get("errors", [])])

    degraded = bool(errors)

    def _threshold(key: str, default_warn: float = 80, default_crit: float = 95) -> dict:
        """Return per-metric thresholds: per-metric config → per-metric defaults (80/95).
        Global warnPercent/criticalPercent is NOT used as fallback to keep defaults sane.
        Clamp to valid values: 1 <= warn <= 99 and warn < critical <= 100."""
        per = _cfg.get(key, {})
        try:
            w = float(per.get("warn") or default_warn) if isinstance(per, dict) else default_warn
        except (ValueError, TypeError):
            w = default_warn
        try:
            c = float(per.get("critical") or default_crit) if isinstance(per, dict) else default_crit
        except (ValueError, TypeError):
            c = default_crit

        w = max(1.0, min(99.0, w))
        c = max(1.0, min(100.0, c))
        if c <= w:
            c = min(100.0, w + 15.0)

        return {
            "warn": w,
            "critical": c,
        }

    all_failed = all(x.get("error") for x in [cpu, ram, swap, disk])
    resp = {
        "ok": not all_failed,
        "degraded": degraded,
        "stale": False,
        "collectedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "pollSeconds": _cfg.get("pollSeconds", 5),
        "thresholds": {
            "cpu":  _threshold("cpu"),
            "ram":  _threshold("ram"),
            "swap": _threshold("swap"),
            "disk": _threshold("disk"),
        },
        "cpu": cpu,
        "ram": ram,
        "swap": swap,
        "disk": disk,
        "versions": versions,
        "openclaw": openclaw,
    }
    if errors:
        resp["errors"] = errors

    try:
        b = json.dumps(resp).encode()
    except Exception:
        return None

    with _ms.lock:
        _ms.payload = b
        _ms.at = time.monotonic()
    return b


# ── CPU ────────────────────────────────────────────────────────────────────

def _collect_cpu(sys_name: str) -> dict:
    try:
        if sys_name == "darwin":
            return _collect_cpu_darwin()
        elif sys_name == "linux":
            return _collect_cpu_linux()
        else:
            return {"percent": 0.0, "cores": os.cpu_count() or 1, "error": f"unsupported platform: {sys_name}"}
    except Exception as e:
        return {"percent": 0.0, "cores": os.cpu_count() or 1, "error": str(e)}


def _collect_cpu_darwin() -> dict:
    result = subprocess.run(
        ["/usr/bin/top", "-l", "2", "-n", "0", "-s", "1"],
        capture_output=True, text=True, timeout=6
    )
    return parse_top_cpu(result.stdout, os.cpu_count() or 1)


def _collect_cpu_linux() -> dict:
    cores = os.cpu_count() or 1
    s1 = _read_proc_stat()
    time.sleep(0.2)   # 200ms sample — parity with Go; provides stable readings
    s2 = _read_proc_stat()
    if s1 is None or s2 is None:
        return {"percent": 0.0, "cores": cores, "error": "could not read /proc/stat"}
    total1 = sum(s1.values())
    total2 = sum(s2.values())
    dtotal = total2 - total1
    didle = s2.get("idle", 0) - s1.get("idle", 0)
    if dtotal == 0:
        return {"percent": 0.0, "cores": cores}
    pct = round((dtotal - didle) / dtotal * 100, 1)
    return {"percent": pct, "cores": cores, "error": None}


def _read_proc_stat() -> Optional[dict]:
    try:
        with open("/proc/stat") as f:
            line = f.readline()
        fields = line.split()
        if not fields or fields[0] != "cpu":
            return None
        keys = ["user", "nice", "system", "idle", "iowait", "irq", "softirq", "steal"]
        return {keys[i]: int(fields[i + 1]) for i in range(min(len(keys), len(fields) - 1))}
    except Exception:
        return None


# ── RAM ────────────────────────────────────────────────────────────────────

def _collect_ram(sys_name: str) -> dict:
    try:
        if sys_name == "darwin":
            return _collect_ram_darwin()
        elif sys_name == "linux":
            return _collect_ram_linux()
        else:
            return {"usedBytes": 0, "totalBytes": 0, "percent": 0.0, "error": f"unsupported: {sys_name}"}
    except Exception as e:
        return {"usedBytes": 0, "totalBytes": 0, "percent": 0.0, "error": str(e)}


def _collect_ram_darwin() -> dict:
    r = subprocess.run(["/usr/sbin/sysctl", "-n", "hw.memsize"], capture_output=True, text=True, timeout=2)
    total = int(r.stdout.strip())
    vm = subprocess.run(["/usr/bin/vm_stat"], capture_output=True, text=True, timeout=2)
    return parse_vm_stat(vm.stdout, total)


def _collect_ram_linux() -> dict:
    with open("/proc/meminfo") as f:
        content = f.read()
    info = parse_proc_meminfo(content)
    total_kb = info.get("MemTotal", 0)
    avail_kb = info.get("MemAvailable", info.get("MemFree", 0))
    used_kb = total_kb - avail_kb
    total_bytes = total_kb * 1024
    used_bytes = used_kb * 1024
    pct = round(used_bytes / total_bytes * 100, 1) if total_bytes > 0 else 0.0
    return {"usedBytes": used_bytes, "totalBytes": total_bytes, "percent": pct, "error": None}


# ── Swap ───────────────────────────────────────────────────────────────────

def _collect_swap(sys_name: str) -> dict:
    try:
        if sys_name == "darwin":
            r = subprocess.run(["/usr/sbin/sysctl", "vm.swapusage"], capture_output=True, text=True, timeout=2)
            return parse_swap_usage_darwin(r.stdout)
        elif sys_name == "linux":
            with open("/proc/meminfo") as f:
                content = f.read()
            info = parse_proc_meminfo(content)
            total_kb = info.get("SwapTotal", 0)
            free_kb = info.get("SwapFree", 0)
            used_kb = total_kb - free_kb
            total_bytes = total_kb * 1024
            used_bytes = used_kb * 1024
            pct = round(used_bytes / total_bytes * 100, 1) if total_bytes > 0 else 0.0
            return {"usedBytes": used_bytes, "totalBytes": total_bytes, "percent": pct, "error": None}
        else:
            return {"usedBytes": 0, "totalBytes": 0, "percent": 0.0, "error": f"unsupported: {sys_name}"}
    except Exception as e:
        return {"usedBytes": 0, "totalBytes": 0, "percent": 0.0, "error": str(e)}


# ── Disk ───────────────────────────────────────────────────────────────────

def collect_disk(path: str = "/") -> dict:
    """Public — used in tests."""
    return _collect_disk(path)


def _collect_disk(path: str) -> dict:
    try:
        st = os.statvfs(path)
        total = st.f_blocks * st.f_frsize
        free = st.f_bavail * st.f_frsize
        used = total - free
        pct = round(used / total * 100, 1) if total > 0 else 0.0
        return {"path": path, "usedBytes": used, "totalBytes": total, "percent": pct, "error": None}
    except Exception as e:
        return {"path": path, "usedBytes": 0, "totalBytes": 0, "percent": 0.0, "error": str(e)}


# ── Versions ───────────────────────────────────────────────────────────────

def _get_versions_cached() -> dict:
    """Return cached versions, collecting fresh data when the TTL has expired.

    Uses a double-checked lock with a ``refreshing`` flag (parity with Go's
    ``getVersionsCached``) to prevent a thundering herd: when multiple threads
    arrive simultaneously after cache expiry only ONE calls ``_collect_versions``;
    the others return the previous stale data immediately.
    """
    ttl = _cfg.get("versionsTtlSeconds", 300)
    now = time.monotonic()

    with _vs.lock:
        # Fast path: cache hit
        if _vs.cache is not None and (now - _vs.at) < ttl:
            return dict(_vs.cache)
        # Thundering-herd guard: another thread is already refreshing — return stale
        if _vs.refreshing:
            return dict(_vs.cache) if _vs.cache is not None else {}
        # Claim the refresh slot before releasing the lock
        _vs.refreshing = True

    # Collect outside the lock (subprocess calls — must not hold lock during I/O)
    try:
        v = _collect_versions()
    except Exception:
        _log.exception("[system] _collect_versions failed")
        v = None

    with _vs.lock:
        if v is not None:
            _vs.cache = v
            _vs.at = time.monotonic()
        _vs.refreshing = False

    return v if v is not None else (dict(_vs.cache) if _vs.cache else {})


def _versionish_sort_key(value: str):
    key = []
    for token in re.findall(r"\d+|[A-Za-z]+", value):
        if token.isdigit():
            key.append((1, int(token)))
        else:
            key.append((0, token.lower()))
    return tuple(key)


def _resolve_openclaw_bin() -> str:
    """Find openclaw binary — asdf shims may not be in server's PATH."""
    if shutil.which("openclaw"):
        return "openclaw"
    home = os.path.expanduser("~")
    candidates = [
        os.path.join(home, ".asdf", "shims", "openclaw"),
    ]
    # Probe all asdf nodejs installs dynamically, highest version first.
    node_dir = os.path.join(home, ".asdf", "installs", "nodejs")
    if os.path.isdir(node_dir):
        versions = sorted(os.listdir(node_dir), key=_versionish_sort_key, reverse=True)
        candidates.extend(os.path.join(node_dir, ver, "bin", "openclaw") for ver in versions)
    candidates.extend([
        "/usr/local/bin/openclaw",
        "/opt/homebrew/bin/openclaw",
    ])
    for c in candidates:
        if os.path.isfile(c) and os.access(c, os.X_OK):
            return c
    return "openclaw"  # last resort


def _collect_versions() -> dict:
    timeout_s = _cfg.get("gatewayTimeoutMs", 1500) / 1000
    oc_bin = _resolve_openclaw_bin()

    # OpenClaw version
    openclaw = "unknown"
    try:
        r = subprocess.run([oc_bin, "--version"], capture_output=True, text=True, timeout=timeout_s)
        val = r.stdout.strip()
        if val:
            openclaw = val.removeprefix("openclaw ").strip()
    except Exception:
        pass

    # Gateway status — try `openclaw gateway status --json` first for PID/uptime/memory,
    # fall back to HTTP HEAD probe when the CLI output is unusable.
    # I2 fix: parse stdout even on non-zero exit — the Go version returns stdout alongside
    # the error, and many CLIs emit valid JSON to stdout while exiting non-zero (e.g. when
    # the gateway is offline but status was successfully queried).
    gw = {"version": "", "status": "unknown", "error": None}
    need_fallback = True
    try:
        r = subprocess.run(
            [oc_bin, "gateway", "status", "--json"],
            capture_output=True, text=True, timeout=max(timeout_s, 5.0)
        )
        raw = r.stdout.strip()
        start = raw.find("{")
        if start >= 0:
            try:
                parsed = json.loads(raw[start:])
            except json.JSONDecodeError:
                parsed = None
            if isinstance(parsed, dict):
                svc = parsed.get("service") if isinstance(parsed, dict) else {}
                svc = svc if isinstance(svc, dict) else {}
                runtime = svc.get("runtime") if isinstance(svc, dict) else {}
                runtime = runtime if isinstance(runtime, dict) else {}
                runtime_status = str(runtime.get("status", "")).lower()
                gw["status"] = "online" if runtime_status == "running" or svc.get("loaded") else "offline"
                version = parsed.get("version", "") if isinstance(parsed, dict) else ""
                if isinstance(version, str):
                    gw["version"] = version
                pid = runtime.get("pid")
                if pid:
                    gw["pid"] = pid
                    # Get uptime + memory from ps
                    try:
                        ps = subprocess.run(
                            ["ps", "-o", "etime=,rss=", "-p", str(pid)],
                            capture_output=True, text=True, timeout=2
                        )
                        fields = ps.stdout.strip().split()
                        if len(fields) >= 1:
                            gw["uptime"] = fields[0]
                        if len(fields) >= 2:
                            rss_kb = int(fields[1])
                            rss_bytes = rss_kb * 1024
                            if rss_bytes >= 1024**3:
                                gw["memory"] = f"{rss_bytes/1024**3:.1f}GB"
                            elif rss_bytes >= 1024**2:
                                gw["memory"] = f"{rss_bytes/1024**2:.1f}MB"
                            else:
                                gw["memory"] = f"{rss_kb}KB"
                    except Exception:
                        pass
                need_fallback = False
    except Exception:
        pass

    if need_fallback:
        try:
            gw_port = _cfg.get("gatewayPort", 18789)
            req = urllib.request.Request(f"http://127.0.0.1:{gw_port}/", method="HEAD")
            with urllib.request.urlopen(req, timeout=timeout_s) as resp:
                gw["status"] = "online" if resp.status < 500 else "offline"
                gw["error"] = None
        except Exception as e:
            gw["status"] = "offline"
            gw["error"] = str(e)

    # Latest version from npm registry (best-effort)
    latest = ""
    try:
        import urllib.request as _ur
        req = _ur.Request("https://registry.npmjs.org/openclaw/latest")
        req.add_header("Accept", "application/json")
        with _ur.urlopen(req, timeout=min(timeout_s, 3)) as resp:
            import json as _json
            latest = _json.loads(resp.read()).get("version", "")
    except Exception:
        pass

    return {
        "dashboard": _dashboard_version,
        "openclaw": openclaw,
        "latest": latest,
        "gateway": gw,
    }


def _collect_openclaw_runtime(oc_bin: str, versions: dict) -> dict:
    """Collect live OpenClaw runtime observability for /api/system.openclaw.
    Partial failures are expected and reported in openclaw.errors.

    Both subcollectors (gateway probes, status) run in parallel via
    ThreadPoolExecutor — parity with Go's sync.WaitGroup approach.
    """
    from concurrent.futures import ThreadPoolExecutor
    timeout_s = max(_cfg.get("gatewayTimeoutMs", 1500) / 1000, 0.2)
    gw_port = int(_cfg.get("gatewayPort", 18789) or 18789)

    # Results accumulated from parallel subcollectors
    errors: list[str] = []
    gateway = {
        "live": False,
        "ready": False,
        "uptimeMs": 0,
        "healthEndpointOk": False,
        "readyEndpointOk": False,
    }
    freshness: dict[str, str] = {}
    status: dict = {}

    # ── Subcollector 1: Gateway /healthz + /readyz probes ──
    def _probe_gateway():
        nonlocal gateway, freshness
        gw_errors = []
        try:
            health = _fetch_json_url(f"http://127.0.0.1:{gw_port}/healthz", timeout_s)
            gateway["healthEndpointOk"] = True
            gateway["live"] = bool(health.get("ok") or str(health.get("status", "")).lower() == "live")
            freshness["gateway"] = _now_rfc3339()
        except Exception as e:
            gw_errors.append(f"gateway /healthz: {e}")

        try:
            # readyz returns 503 when not ready — but the body still contains
            # useful JSON (ready, failing, uptimeMs). Parse on both 200 and 503.
            ready = _fetch_json_url_allow_status(
                f"http://127.0.0.1:{gw_port}/readyz", timeout_s, {200, 503}
            )
            gateway["readyEndpointOk"] = True
            gateway["ready"] = bool(ready.get("ready"))
            gateway["uptimeMs"] = int(ready.get("uptimeMs") or 0)
            failing = [str(x) for x in (ready.get("failing") or []) if str(x)]
            gateway["failing"] = failing
            freshness["gateway"] = _now_rfc3339()
        except Exception as e:
            gw_errors.append(f"gateway /readyz: {e}")
        return gw_errors

    # ── Subcollector 2: openclaw status --json ──
    def _probe_status():
        nonlocal status, freshness
        try:
            r = subprocess.run([oc_bin, "status", "--json"], capture_output=True, text=True, timeout=timeout_s)
            # I2 fix: attempt to parse stdout even on non-zero exit — matching Go behavior
            # where runWithTimeout returns stdout alongside the error. Many CLIs emit valid
            # JSON status data to stdout while exiting non-zero.
            parse_errors: list[str] = []
            if r.returncode != 0:
                stderr_msg = (r.stderr or "").strip()
                parse_errors.append(
                    f"status --json: exit code {r.returncode}" + (f": {stderr_msg}" if stderr_msg else "")
                )
            # Try to extract useful data from stdout regardless of exit code
            stdout_text = (r.stdout or "").strip()
            if stdout_text and "{" in stdout_text:
                try:
                    parsed = _parse_json_object_fragment(stdout_text)
                    current = parsed.get("currentVersion") or parsed.get("version")
                    if isinstance(current, str) and current:
                        status["currentVersion"] = current
                    latest = parsed.get("latestVersion")
                    if isinstance(latest, str) and latest:
                        status["latestVersion"] = latest
                    if isinstance(parsed.get("connectLatencyMs"), (int, float)):
                        status["connectLatencyMs"] = int(parsed.get("connectLatencyMs"))
                    security = parsed.get("security")
                    if isinstance(security, dict):
                        status["security"] = security
                    freshness["status"] = _now_rfc3339()
                except (ValueError, json.JSONDecodeError):
                    # stdout wasn't valid JSON — fall through to report the exit code error
                    pass
            elif r.returncode == 0:
                # Zero exit but no JSON → unexpected
                parse_errors.append("status --json: empty/non-JSON stdout")
            return parse_errors
        except Exception as e:
            return [f"status --json: {e}"]

    # Run runtime subcollectors in parallel — gateway + status only.
    with ThreadPoolExecutor(max_workers=2) as pool:
        futures = [
            pool.submit(_probe_gateway),
            pool.submit(_probe_status),
        ]
        for f in futures:
            result_errors = f.result()
            if result_errors:
                errors.extend(result_errors)

    # Build status dict with omitempty semantics
    status_out: dict = {}
    cv = status.get("currentVersion") or versions.get("openclaw", "")
    if cv:
        status_out["currentVersion"] = cv
    lv = status.get("latestVersion") or versions.get("latest", "")
    if lv:
        status_out["latestVersion"] = lv
    if status.get("connectLatencyMs"):
        status_out["connectLatencyMs"] = status["connectLatencyMs"]
    if status.get("security"):
        status_out["security"] = status["security"]

    runtime: dict = {
        "gateway": gateway,
        "status": status_out,
        "freshness": freshness,
    }
    # Only include errors if non-empty (Go omitempty on slice)
    if errors:
        runtime["errors"] = errors

    return runtime


def _fetch_json_url_allow_status(url: str, timeout_s: float, allowed: set[int]) -> dict:
    """Fetch JSON from a URL, accepting specific HTTP status codes.

    readyz returns 503 when not ready but still has a useful JSON body
    (ready, failing, uptimeMs). This variant allows parsing on non-2xx.
    """
    req = urllib.request.Request(url)
    try:
        with urllib.request.urlopen(req, timeout=timeout_s) as resp:
            if resp.status not in allowed:
                raise ValueError(f"status {resp.status}")
            payload = resp.read()
    except urllib.error.HTTPError as e:
        if e.code in allowed:
            payload = e.read()
        else:
            raise ValueError(f"status {e.code}") from e
    data = json.loads(payload.decode("utf-8", errors="replace"))
    if not isinstance(data, dict):
        raise ValueError("expected JSON object")
    return data


def _fetch_json_url(url: str, timeout_s: float) -> dict:
    """Fetch a JSON object from a URL. Raises on any non-2xx status — parity with Go's fetchJSONMap (I1 fix)."""
    req = urllib.request.Request(url)
    with urllib.request.urlopen(req, timeout=timeout_s) as resp:
        # Reject any non-2xx status code — both 4xx and 5xx indicate the endpoint
        # did not return a valid JSON payload we should trust.
        if resp.status < 200 or resp.status >= 300:
            raise ValueError(f"status {resp.status}")
        payload = resp.read()
    data = json.loads(payload.decode("utf-8", errors="replace"))
    if not isinstance(data, dict):
        raise ValueError("expected JSON object")
    return data


def _parse_json_object_fragment(text: str) -> dict:
    start = text.find("{")
    if start < 0:
        raise ValueError("json object not found")
    data = json.loads(text[start:])
    if not isinstance(data, dict):
        raise ValueError("expected JSON object")
    return data


def _now_rfc3339() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


# ── Parsers (exported for unit tests) ─────────────────────────────────────

def parse_top_cpu(output: str, cores: int = 1) -> dict:
    """Parse macOS `top -l 2` output → cpu dict.
    Uses the LAST CPU usage line (current delta, not boot average).
    Handles both '84.21% idle' and '100% idle' (integer idle)."""
    last_match = None
    for line in output.splitlines():
        if "CPU usage" in line or "cpu usage" in line.lower():
            m = re.search(r"(\d+(?:\.\d+)?)%\s*idle", line, re.IGNORECASE)
            if m:
                last_match = m
    if last_match:
        idle = float(last_match.group(1))
        pct = round(100 - idle, 1)
        return {"percent": pct, "cores": cores, "error": None}
    return {"percent": 0.0, "cores": cores, "error": "CPU usage line not found in top output"}


_RE_VM_PAGE_SIZE = re.compile(r"page size of (\d+) bytes")
_RE_VM_ACTIVE    = re.compile(r"^Pages active:\s+(\d+)", re.MULTILINE)
_RE_VM_WIRED     = re.compile(r"^Pages wired down:\s+(\d+)", re.MULTILINE)
_RE_VM_COMPRESS  = re.compile(r"^Pages occupied by compressor:\s+(\d+)", re.MULTILINE)


def parse_vm_stat(output: str, total_bytes: int) -> dict:
    """Parse macOS vm_stat output → ram dict. Uses pre-compiled regexes."""
    page_size = 4096
    m = _RE_VM_PAGE_SIZE.search(output)
    if m:
        page_size = int(m.group(1))

    def get_pages(pat: re.Pattern) -> int:
        mm = pat.search(output)
        return int(mm.group(1)) if mm else 0

    active = get_pages(_RE_VM_ACTIVE)
    wired = get_pages(_RE_VM_WIRED)
    compressed = get_pages(_RE_VM_COMPRESS)
    used_bytes = (active + wired + compressed) * page_size
    pct = round(used_bytes / total_bytes * 100, 1) if total_bytes > 0 else 0.0
    err = None if (active + wired + compressed) > 0 else "could not parse vm_stat pages"
    return {"usedBytes": used_bytes, "totalBytes": total_bytes, "percent": pct, "error": err}


def parse_swap_usage_darwin(output: str) -> dict:
    """Parse macOS `sysctl vm.swapusage` → swap dict."""
    m = re.search(r"total\s*=\s*([\d.]+)([MGT])\s+used\s*=\s*([\d.]+)([MGT])", output, re.IGNORECASE)
    if not m:
        return {"usedBytes": 0, "totalBytes": 0, "percent": 0.0, "error": f"could not parse vm.swapusage: {output!r}"}

    def to_bytes(val: str, unit: str) -> int:
        v = float(val)
        unit = unit.upper()
        if unit == "G":
            return int(v * 1024 ** 3)
        if unit == "T":
            return int(v * 1024 ** 4)
        return int(v * 1024 ** 2)  # M default

    total = to_bytes(m.group(1), m.group(2))
    used = to_bytes(m.group(3), m.group(4))
    pct = round(used / total * 100, 1) if total > 0 else 0.0
    return {"usedBytes": used, "totalBytes": total, "percent": pct, "error": None}


def parse_proc_meminfo(content: str) -> dict:
    """Parse /proc/meminfo → dict of key→kB values."""
    result = {}
    for line in content.splitlines():
        parts = line.split(":", 1)
        if len(parts) != 2:
            continue
        key = parts[0].strip()
        fields = parts[1].split()
        if fields:
            try:
                result[key] = int(fields[0])
            except ValueError:
                pass
    return result
