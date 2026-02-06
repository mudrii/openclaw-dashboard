#!/bin/bash
# OpenClaw Dashboard Uninstaller

set -e

INSTALL_DIR="${OPENCLAW_HOME:-$HOME/.openclaw}/dashboard"

echo "🦞 OpenClaw Dashboard Uninstaller"
echo ""

# Stop and remove LaunchAgent (macOS)
if [ "$(uname)" = "Darwin" ]; then
  PLIST_FILE="$HOME/Library/LaunchAgents/com.openclaw.dashboard.plist"
  if [ -f "$PLIST_FILE" ]; then
    echo "🛑 Stopping server..."
    launchctl unload "$PLIST_FILE" 2>/dev/null || true
    rm -f "$PLIST_FILE"
    echo "✅ LaunchAgent removed"
  fi
fi

# Stop systemd service (Linux)
if [ "$(uname)" = "Linux" ] && command -v systemctl >/dev/null 2>&1; then
  if systemctl --user is-active openclaw-dashboard >/dev/null 2>&1; then
    echo "🛑 Stopping service..."
    systemctl --user stop openclaw-dashboard
    systemctl --user disable openclaw-dashboard
    rm -f "$HOME/.config/systemd/user/openclaw-dashboard.service"
    echo "✅ Systemd service removed"
  fi
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
