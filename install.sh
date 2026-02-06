#!/bin/bash
# OpenClaw Dashboard Installer
# Supports: macOS, Linux
# Usage: curl -fsSL https://raw.githubusercontent.com/openclaw-community/dashboard/main/install.sh | bash

set -e

REPO="https://github.com/openclaw-community/dashboard"
INSTALL_DIR="${OPENCLAW_HOME:-$HOME/.openclaw}/dashboard"

echo "🦞 OpenClaw Dashboard Installer"
echo ""

# Detect OS
OS="$(uname)"
echo "📍 Detected: $OS"

# Check prerequisites
command -v python3 >/dev/null 2>&1 || { echo "❌ Python 3 is required"; exit 1; }
echo "✅ Python 3 found"

# Check OpenClaw installation
OPENCLAW_PATH="${OPENCLAW_HOME:-$HOME/.openclaw}"
if [ ! -d "$OPENCLAW_PATH" ]; then
  echo "⚠️  OpenClaw not found at $OPENCLAW_PATH"
  echo "   Install OpenClaw first: npm install -g openclaw"
  echo "   Or set OPENCLAW_HOME environment variable"
  exit 1
fi
echo "✅ OpenClaw found at $OPENCLAW_PATH"

echo ""
echo "📁 Installing to: $INSTALL_DIR"

# Clone or update
if [ -d "$INSTALL_DIR/.git" ]; then
  echo "📥 Updating existing installation..."
  cd "$INSTALL_DIR"
  git pull --quiet
else
  if command -v git >/dev/null 2>&1; then
    echo "📥 Cloning repository..."
    mkdir -p "$(dirname "$INSTALL_DIR")"
    git clone --quiet "$REPO" "$INSTALL_DIR"
  else
    echo "📥 Downloading (git not found)..."
    mkdir -p "$INSTALL_DIR"
    curl -fsSL "$REPO/archive/main.tar.gz" | tar -xz --strip-components=1 -C "$INSTALL_DIR"
  fi
  cd "$INSTALL_DIR"
fi

# Create config if not exists
if [ ! -f "config.json" ]; then
  echo "📝 Creating default config.json..."
  cp examples/config.minimal.json config.json
  echo "   Edit config.json to customize your bot name"
fi

# Initial data refresh
echo "🔄 Running initial data refresh..."
./refresh.sh

# Setup auto-start based on OS
echo ""
if [ "$OS" = "Darwin" ]; then
  # macOS: LaunchAgent
  PLIST_DIR="$HOME/Library/LaunchAgents"
  PLIST_FILE="$PLIST_DIR/com.openclaw.dashboard.plist"
  
  mkdir -p "$PLIST_DIR"
  cat > "$PLIST_FILE" << PLISTEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.openclaw.dashboard</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/bin/python3</string>
    <string>-m</string>
    <string>http.server</string>
    <string>8080</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$INSTALL_DIR</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>$INSTALL_DIR/server.log</string>
  <key>StandardErrorPath</key>
  <string>$INSTALL_DIR/server.log</string>
</dict>
</plist>
PLISTEOF

  launchctl unload "$PLIST_FILE" 2>/dev/null || true
  launchctl load "$PLIST_FILE"
  echo "🚀 Server started via LaunchAgent (auto-starts on login)"

elif [ "$OS" = "Linux" ]; then
  # Linux: systemd user service
  if command -v systemctl >/dev/null 2>&1; then
    SERVICE_DIR="$HOME/.config/systemd/user"
    SERVICE_FILE="$SERVICE_DIR/openclaw-dashboard.service"
    
    mkdir -p "$SERVICE_DIR"
    cat > "$SERVICE_FILE" << SERVICEEOF
[Unit]
Description=OpenClaw Dashboard
After=network.target

[Service]
Type=simple
WorkingDirectory=$INSTALL_DIR
ExecStart=/usr/bin/python3 -m http.server 8080
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
SERVICEEOF

    systemctl --user daemon-reload
    systemctl --user enable openclaw-dashboard
    systemctl --user start openclaw-dashboard
    echo "🚀 Server started via systemd user service"
  else
    echo "⚠️  systemd not found. Start manually:"
    echo "   cd $INSTALL_DIR && python3 -m http.server 8080 &"
  fi
else
  echo "⚠️  Unknown OS. Start manually:"
  echo "   cd $INSTALL_DIR && python3 -m http.server 8080 &"
fi

echo ""
echo "✅ Installation complete!"
echo ""
echo "📊 Dashboard: http://127.0.0.1:8080"
echo "⚙️  Config:    $INSTALL_DIR/config.json"
echo "🔄 Refresh:   $INSTALL_DIR/refresh.sh"
echo "📚 Docs:      $INSTALL_DIR/docs/CONFIGURATION.md"
echo ""
echo "Next steps:"
echo "  1. Edit config.json to set your bot name"
echo "  2. Open http://127.0.0.1:8080 in your browser"
echo "  3. (Optional) Set up a cron job to run refresh.sh periodically"
