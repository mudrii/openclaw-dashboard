"""
system_metrics.py — host metrics collector for openclaw-dashboard.

Cross-platform (macOS/darwin + Linux).
No external dependencies — stdlib + subprocess only.
"""

import json
import os
import platform
import re
import subprocess
import threading
import time
from typing import Optional

# ── version (set by server.py before use) ──────────────────────────────────
_dashboard_version: str = "unknown"

# ── metrics cache ──────────────────────────────────────────────────────────
_metrics_lock = threading.Lock()
_metrics_payload: Optional[bytes] = None
_metrics_at: float = 0.0
_metrics_refreshing: bool = False

# ── versions cache (longer TTL) ────────────────────────────────────────────
_versions_lock = threading.Lock()
_versions_cache: Optional[dict] = None
_versions_at: float = 0.0

# ── config (set by server.py at startup) ──────────────────────────────────
_cfg: dict = {
    "enabled": True,
    "pollSeconds": 5,
    "metricsTtlSeconds": 5,
    "versionsTtlSeconds": 300,
    "gatewayTimeoutMs": 1500,
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

    with _metrics_lock:
        payload = _metrics_payload
        at = _metrics_at
        refreshing = _metrics_refreshing
        fresh = payload is not None and (now - at) < ttl
        has_stale = payload is not None

    if fresh:
        return 200, payload

    if has_stale:
        # Return stale immediately; trigger background refresh if not already running
        if not refreshing:
            with _metrics_lock:
                if not _metrics_refreshing:
                    globals()["_metrics_refreshing"] = True
            t = threading.Thread(target=_bg_refresh, daemon=True)
            t.start()
        # Inject stale flag
        try:
            data = json.loads(payload)
            data["stale"] = True
            return 200, json.dumps(data).encode()
        except Exception:
            return 200, payload

    # No cache — collect synchronously
    data = _collect_all()
    if data is None:
        body = json.dumps({"ok": False, "degraded": True, "error": "system metrics unavailable"}).encode()
        return 503, body
    return 200, data


# ── internal ───────────────────────────────────────────────────────────────

def _bg_refresh() -> None:
    global _metrics_refreshing
    try:
        _collect_all()
    finally:
        with _metrics_lock:
            globals()["_metrics_refreshing"] = False


def _collect_all() -> Optional[bytes]:
    global _metrics_payload, _metrics_at

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

    # ── Versions (separate TTL) ──
    versions = _get_versions_cached()

    degraded = bool(errors)
    resp = {
        "ok": True,
        "degraded": degraded,
        "stale": False,
        "collectedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "pollSeconds": _cfg.get("pollSeconds", 5),
        "cpu": cpu,
        "ram": ram,
        "swap": swap,
        "disk": disk,
        "versions": versions,
    }
    if errors:
        resp["errors"] = errors

    try:
        b = json.dumps(resp).encode()
    except Exception:
        return None

    with _metrics_lock:
        globals()["_metrics_payload"] = b
        globals()["_metrics_at"] = time.monotonic()
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
        ["top", "-l", "1", "-n", "0", "-s", "0"],
        capture_output=True, text=True, timeout=4
    )
    return parse_top_cpu(result.stdout, os.cpu_count() or 1)


def _collect_cpu_linux() -> dict:
    cores = os.cpu_count() or 1
    s1 = _read_proc_stat()
    time.sleep(0.2)
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
        keys = ["user", "nice", "system", "idle", "iowait", "irq", "softirq"]
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
    r = subprocess.run(["sysctl", "-n", "hw.memsize"], capture_output=True, text=True, timeout=2)
    total = int(r.stdout.strip())
    vm = subprocess.run(["vm_stat"], capture_output=True, text=True, timeout=2)
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
            r = subprocess.run(["sysctl", "vm.swapusage"], capture_output=True, text=True, timeout=2)
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
    ttl = _cfg.get("versionsTtlSeconds", 300)
    now = time.monotonic()
    with _versions_lock:
        if _versions_cache is not None and (now - _versions_at) < ttl:
            return dict(_versions_cache)

    v = _collect_versions()
    with _versions_lock:
        globals()["_versions_cache"] = v
        globals()["_versions_at"] = time.monotonic()
    return v


def _collect_versions() -> dict:
    timeout_s = _cfg.get("gatewayTimeoutMs", 1500) / 1000

    # OpenClaw version
    openclaw = "unknown"
    try:
        r = subprocess.run(["openclaw", "--version"], capture_output=True, text=True, timeout=timeout_s)
        val = r.stdout.strip()
        if val:
            openclaw = val.removeprefix("openclaw ").strip()
    except Exception:
        pass

    # Gateway status
    gw = {"version": "", "status": "unknown", "error": None}
    try:
        r = subprocess.run(["openclaw", "gateway", "status"], capture_output=True, text=True, timeout=timeout_s)
        out = r.stdout.lower()
        if "running" in out or "online" in out:
            gw["status"] = "online"
        else:
            gw["status"] = "offline"
        # try extract version
        for line in r.stdout.splitlines():
            for token in line.split():
                t = token.strip("()v,")
                if len(t) > 4 and (t.startswith("20") or t.startswith("0.")):
                    gw["version"] = t
                    break
    except Exception as e:
        gw["status"] = "offline"
        gw["error"] = str(e)

    return {
        "dashboard": _dashboard_version,
        "openclaw": openclaw,
        "gateway": gw,
    }


# ── Parsers (exported for unit tests) ─────────────────────────────────────

def parse_top_cpu(output: str, cores: int = 1) -> dict:
    """Parse macOS `top -l 1 -n 0` output → cpu dict."""
    for line in output.splitlines():
        if "CPU usage" in line or "cpu usage" in line.lower():
            m = re.search(r"([\d.]+)%\s*idle", line, re.IGNORECASE)
            if m:
                idle = float(m.group(1))
                pct = round(100 - idle, 1)
                return {"percent": pct, "cores": cores, "error": None}
    return {"percent": 0.0, "cores": cores, "error": "CPU usage line not found in top output"}


def parse_vm_stat(output: str, total_bytes: int) -> dict:
    """Parse macOS vm_stat output → ram dict."""
    page_size = 4096
    m = re.search(r"page size of (\d+) bytes", output)
    if m:
        page_size = int(m.group(1))

    def get_pages(label: str) -> int:
        pat = re.compile(r"^" + re.escape(label) + r"\s+(\d+)", re.MULTILINE)
        mm = pat.search(output)
        return int(mm.group(1)) if mm else 0

    active = get_pages("Pages active:")
    wired = get_pages("Pages wired down:")
    compressed = get_pages("Pages occupied by compressor:")
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
