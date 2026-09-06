#!/bin/sh
# Exercise the shipped runtime image without a live OpenClaw installation.
set -eu

checkdir="$(mktemp -d)"
trap 'rm -rf "$checkdir"' EXIT HUP INT TERM

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
echo "Container smoke passed: process tools, timezone, refresh, private snapshot"
