#!/usr/bin/env python3
"""OpenClaw Dashboard Server — static files + on-demand refresh."""

import argparse
import http.server
import json
import os
import subprocess
import threading
import time
import sys

PORT = 8080
BIND = "127.0.0.1"
DIR = os.path.dirname(os.path.abspath(__file__))
REFRESH_SCRIPT = os.path.join(DIR, "refresh.sh")
DATA_FILE = os.path.join(DIR, "data.json")
DEBOUNCE_SEC = 30
REFRESH_TIMEOUT = 15

_last_refresh = 0
_refresh_lock = threading.Lock()


def run_refresh():
    """Run refresh.sh with debounce and timeout."""
    global _last_refresh
    now = time.time()

    with _refresh_lock:
        if now - _last_refresh < DEBOUNCE_SEC:
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


def main():
    import socket

    parser = argparse.ArgumentParser(description="OpenClaw Dashboard Server")
    parser.add_argument(
        "--bind", "-b",
        default=os.environ.get("DASHBOARD_BIND", BIND),
        help="Address to bind to (default: 127.0.0.1, use 0.0.0.0 for LAN access)",
    )
    parser.add_argument(
        "--port", "-p",
        type=int,
        default=int(os.environ.get("DASHBOARD_PORT", PORT)),
        help="Port to listen on (default: 8080)",
    )
    args = parser.parse_args()

    server = http.server.HTTPServer((args.bind, args.port), DashboardHandler)
    server.socket.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    print(f"[dashboard] Serving on http://{args.bind}:{args.port}/")
    print(f"[dashboard] Refresh endpoint: /api/refresh (debounce: {DEBOUNCE_SEC}s)")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n[dashboard] Shutting down.")
        server.shutdown()


if __name__ == "__main__":
    main()
