#!/bin/sh
# Exercise the shipped runtime image without a live OpenClaw installation.
set -eu

checkdir="$(mktemp -d)"
server_pid=""
cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$checkdir"
}
trap cleanup EXIT HUP INT TERM

# Collectors require procps-compatible options; BusyBox ps is insufficient.
ps -o etime=,rss= -p "$$" > /dev/null
command -v bash > /dev/null
command -v git > /dev/null

cat > "$checkdir/config.json" <<'JSON'
{
  "timezone": "Asia/Kuala_Lumpur",
  "ai": {"enabled": false},
  "system": {"enabled": false},
  "openclaw": {"mode": "native", "binary": "/missing-openclaw-smoke-fixture"}
}
JSON

if ! OPENCLAW_DASHBOARD_DIR="$checkdir" OPENCLAW_STATE_DIR="$checkdir" \
  /app/openclaw-dashboard --refresh > "$checkdir/refresh.log" 2>&1; then
  cat "$checkdir/refresh.log"
  exit 1
fi
# A binary-only Alpine image must retain the configured IANA timezone, rather
# than falling back to UTC because Go's build-time zoneinfo archive is absent.
if ! grep -q '"timezone": "Asia/Kuala_Lumpur"' "$checkdir/data.json"; then
  echo "Configured IANA timezone was not preserved" >&2
  exit 1
fi
test "$(stat -c '%a' "$checkdir/data.json")" = 600
OPENCLAW_DASHBOARD_DIR="$checkdir" OPENCLAW_STATE_DIR="$checkdir" \
  /app/openclaw-dashboard --bind 127.0.0.1 --port 8080 > "$checkdir/server.log" 2>&1 &
server_pid=$!
attempt=0
until wget -q -O "$checkdir/index.html" http://127.0.0.1:8080/; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 20 ] || ! kill -0 "$server_pid" 2>/dev/null; then
    cat "$checkdir/server.log"
    exit 1
  fi
  sleep 1
done
grep -q '<title>OpenClaw Dashboard</title>' "$checkdir/index.html"
wget -q -O "$checkdir/api.json" http://127.0.0.1:8080/api/refresh
grep -q '"timezone".*"Asia/Kuala_Lumpur"' "$checkdir/api.json"
echo "Container smoke passed: process tools, timezone, refresh, private snapshot, HTTP UI/API"
