#!/usr/bin/env bash
# OpenClaw Dashboard Uninstaller

set -euo pipefail

INSTALL_DIR="${OPENCLAW_HOME:-$HOME/.openclaw}/dashboard"

echo "🦞 OpenClaw Dashboard Uninstaller"
echo ""

if [ -x "$INSTALL_DIR/openclaw-dashboard" ]; then
  echo "🛑 Stopping and unregistering service..."
  "$INSTALL_DIR/openclaw-dashboard" uninstall || true
else
  if [ "$(uname)" = "Darwin" ]; then
    PLIST_FILE="$HOME/Library/LaunchAgents/com.openclaw.dashboard.plist"
    if [ -f "$PLIST_FILE" ]; then
      launchctl unload "$PLIST_FILE" 2>/dev/null || true
      rm -f "$PLIST_FILE"
    fi
  fi

  if [ "$(uname)" = "Linux" ] && command -v systemctl >/dev/null 2>&1; then
    systemctl --user stop openclaw-dashboard 2>/dev/null || true
    systemctl --user disable openclaw-dashboard 2>/dev/null || true
    rm -f "$HOME/.config/systemd/user/openclaw-dashboard.service"
    systemctl --user daemon-reload 2>/dev/null || true
  fi

  pkill -f "${INSTALL_DIR}/openclaw-dashboard" 2>/dev/null || true
fi

# Remove installation
if [ -d "$INSTALL_DIR" ]; then
  echo "🗑️  Removing $INSTALL_DIR..."
  rm -rf "$INSTALL_DIR"
  echo "✅ Dashboard removed"
else
  echo "⚠️  Dashboard not found at $INSTALL_DIR"
fi

echo ""
echo "✅ Uninstall complete!"
