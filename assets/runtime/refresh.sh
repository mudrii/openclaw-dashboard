#!/bin/bash
# OpenClaw Dashboard — Data Refresh Script
# Generates data.json using the Go binary

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DIR="$SCRIPT_DIR"
if { [ -f "$SCRIPT_DIR/../../go.mod" ] && [ -f "$SCRIPT_DIR/../../cmd/openclaw-dashboard/main.go" ]; } ||
   [ -x "$SCRIPT_DIR/../../openclaw-dashboard" ]; then
  DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
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
    (cd "$DIR" && go build -ldflags="-s -w" -o openclaw-dashboard ./cmd/openclaw-dashboard)
    BINARY="$DIR/openclaw-dashboard"
  else
    echo "❌ openclaw-dashboard binary not found and 'go' is not available to build from source"
    exit 1
  fi
fi

# Let the binary resolve and validate native/profile/container state. In
# particular, do not export a default OPENCLAW_HOME into OpenClaw CLI children.
exec "$BINARY" --refresh
