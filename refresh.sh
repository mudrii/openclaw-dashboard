#!/bin/bash
# OpenClaw Dashboard — Data Refresh Script
# Generates data.json using the Go binary

set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
OPENCLAW_PATH="${OPENCLAW_HOME:-$HOME/.openclaw}"
OPENCLAW_PATH="${OPENCLAW_PATH/#\~/$HOME}"

echo "Dashboard dir: $DIR"
echo "OpenClaw path: $OPENCLAW_PATH"

if [ ! -d "$OPENCLAW_PATH" ]; then
  echo "❌ OpenClaw not found at $OPENCLAW_PATH"
  exit 1
fi

# Find the Go binary — check common locations
BINARY=""
for candidate in \
  "$DIR/openclaw-dashboard" \
  "$DIR/dist/openclaw-dashboard" \
  "$DIR/dist/openclaw-dashboard-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')" \
  "$(command -v openclaw-dashboard 2>/dev/null)"; do
  if [ -n "$candidate" ] && [ -x "$candidate" ]; then
    BINARY="$candidate"
    break
  fi
done

if [ -z "$BINARY" ]; then
  # Try building from source if go is available
  if command -v go >/dev/null 2>&1 && [ -f "$DIR/go.mod" ]; then
    echo "📦 Building from source..."
    (cd "$DIR" && go build -ldflags="-s -w" -o openclaw-dashboard .)
    BINARY="$DIR/openclaw-dashboard"
  else
    echo "❌ openclaw-dashboard binary not found and 'go' is not available to build from source"
    exit 1
  fi
fi

export OPENCLAW_HOME="$OPENCLAW_PATH"
"$BINARY" --refresh
