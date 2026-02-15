#!/usr/bin/env python3
"""OpenClaw Dashboard Server — static files + on-demand refresh."""

import argparse
import http.server
import json
import os
import socket
import subprocess
import threading
import time
import sys

VERSION = "2.3.0"
PORT = 8080
BIND = "127.0.0.1"
DIR = os.path.dirname(os.path.abspath(__file__))
CONFIG_FILE = os.path.join(DIR, "config.json")
REFRESH_SCRIPT = os.path.join(DIR, "refresh.sh")
DATA_FILE = os.path.join(DIR, "data.json")
REFRESH_TIMEOUT = 15

_last_refresh = 0
_refresh_lock = threading.Lock()
_debounce_sec = 30


def load_config():
    """Load config.json, return empty dict on failure."""
    try:
        with open(CONFIG_FILE, "r") as f:
            return json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        return {}


def run_refresh():
    """Run refresh.sh with debounce and timeout."""
    global _last_refresh
    now = time.time()

    with _refresh_lock:
        if now - _last_refresh < _debounce_sec:
            return True  # debounced, serve cached

        try:
            subprocess.run(
                ["bash", REFRESH_SCRIPT],
                timeout=REFRESH_TIMEOUT,
                cwd=DIR,
                capture_output=True,
            )
            _last_refresh = time.time()
            return True
        except subprocess.TimeoutExpired:
            print(f"[dashboard] refresh.sh timed out after {REFRESH_TIMEOUT}s")
            return False
        except Exception as e:
            print(f"[dashboard] refresh.sh failed: {e}")
            return False


class DashboardHandler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=DIR, **kwargs)

    def do_GET(self):
        if self.path == "/api/refresh" or self.path.startswith("/api/refresh?"):
            self.handle_refresh()
        else:
            super().do_GET()

    def handle_refresh(self):
        run_refresh()

        try:
            with open(DATA_FILE, "r") as f:
                data = f.read()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Access-Control-Allow-Origin", "*")
            self.end_headers()
            self.wfile.write(data.encode())
        except FileNotFoundError:
            self.send_response(503)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"error": "data.json not found"}).encode())
        except Exception as e:
            self.send_response(500)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"error": str(e)}).encode())

    def log_message(self, format, *args):
        # Quiet logging — only log errors and refreshes
        msg = format % args
        if "/api/refresh" in msg or "error" in msg.lower():
            print(f"[dashboard] {msg}")


def resolve_config_value(key, cli_val, env_var, config_path, default):
    """Resolve config with priority: CLI flag > env var > config.json > default."""
    # CLI flag (argparse default is the fallback, so check if explicitly set)
    if cli_val is not None:
        return cli_val
    # Environment variable
    env_val = os.environ.get(env_var)
    if env_val is not None:
        return env_val
    # config.json
    cfg = load_config()
    parts = config_path.split(".")
    val = cfg
    for part in parts:
        if isinstance(val, dict):
            val = val.get(part)
        else:
            val = None
            break
    if val is not None:
        return val
    return default


def main():
    cfg = load_config()
    server_cfg = cfg.get("server", {})
    refresh_cfg = cfg.get("refresh", {})

    # Config.json defaults (lowest priority)
    cfg_bind = server_cfg.get("host", BIND)
    cfg_port = server_cfg.get("port", PORT)
    global _debounce_sec
    _debounce_sec = refresh_cfg.get("intervalSeconds", _debounce_sec)

    # Env vars override config.json
    env_bind = os.environ.get("DASHBOARD_BIND", cfg_bind)
    env_port = int(os.environ.get("DASHBOARD_PORT", cfg_port))

    parser = argparse.ArgumentParser(
        description="OpenClaw Dashboard Server",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""priority: CLI flags > env vars > config.json > defaults

examples:
  %(prog)s                          # localhost:8080 (default)
  %(prog)s --bind 0.0.0.0           # LAN access on port 8080
  %(prog)s -b 0.0.0.0 -p 9090      # LAN access on custom port
  DASHBOARD_BIND=0.0.0.0 %(prog)s   # env var override""",
    )
    parser.add_argument(
        "--bind", "-b",
        default=env_bind,
        help=f"Bind address (default: {env_bind}, use 0.0.0.0 for LAN)",
    )
    parser.add_argument(
        "--port", "-p",
        type=int,
        default=env_port,
        help=f"Listen port (default: {env_port})",
    )
    parser.add_argument(
        "--version", "-V",
        action="version",
        version=f"%(prog)s {VERSION}",
    )
    args = parser.parse_args()

    server = http.server.HTTPServer((args.bind, args.port), DashboardHandler)
    server.socket.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    print(f"[dashboard] v{VERSION}")
    print(f"[dashboard] Serving on http://{args.bind}:{args.port}/")
    print(f"[dashboard] Refresh endpoint: /api/refresh (debounce: {_debounce_sec}s)")
    if args.bind == "0.0.0.0":
        try:
            hostname = socket.gethostname()
            local_ip = socket.gethostbyname(hostname)
            print(f"[dashboard] LAN access: http://{local_ip}:{args.port}/")
        except Exception:
            pass
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n[dashboard] Shutting down.")
        server.shutdown()


if __name__ == "__main__":
    main()
