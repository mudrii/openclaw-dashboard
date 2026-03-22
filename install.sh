#!/bin/bash
# OpenClaw Dashboard Installer (Go binary)
# Supports: macOS, Linux
# Usage: curl -fsSL https://raw.githubusercontent.com/mudrii/openclaw-dashboard/main/install.sh | bash

set -e

REPO="https://github.com/mudrii/openclaw-dashboard"
INSTALL_DIR="${OPENCLAW_HOME:-$HOME/.openclaw}/dashboard"

echo "🦞 OpenClaw Dashboard Installer"
echo ""

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)       echo "❌ Unsupported architecture: $ARCH"; exit 1 ;;
esac
echo "📍 Detected: $OS/$ARCH"

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

# Download or build the Go binary
BINARY_NAME="openclaw-dashboard-${OS}-${ARCH}"
BINARY_URL="$REPO/releases/latest/download/$BINARY_NAME"
echo "📦 Downloading Go binary ($BINARY_NAME)..."
if curl -fsSL "$BINARY_URL" -o openclaw-dashboard 2>/dev/null; then
  chmod +x openclaw-dashboard
  echo "✅ Binary downloaded"
elif command -v go >/dev/null 2>&1; then
  echo "⚠️  Download failed, building from source..."
  go build -ldflags="-s -w" -o openclaw-dashboard .
  echo "✅ Binary built from source"
else
  echo "❌ Could not download binary and 'go' is not available to build from source"
  exit 1
fi

# Make scripts executable
chmod +x refresh.sh

# Create config if not exists
if [ ! -f "config.json" ]; then
  echo "📝 Creating default config.json..."
  if [ -f "examples/config.minimal.json" ]; then
    cp examples/config.minimal.json config.json
  else
    echo '{"bot":{"name":"OpenClaw Dashboard","emoji":"🦞"},"server":{"port":8080}}' > config.json
  fi
  echo "   Edit config.json to customize your dashboard"
fi

# Initial data refresh
echo "🔄 Running initial data refresh..."
./openclaw-dashboard --refresh

# Setup auto-start based on OS
echo ""
if [ "$(uname)" = "Darwin" ]; then
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
    <string>${INSTALL_DIR}/openclaw-dashboard</string>
    <string>--port</string>
    <string>8080</string>
  </array>
  <key>WorkingDirectory</key>
  <string>${INSTALL_DIR}</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>${INSTALL_DIR}/server.log</string>
  <key>StandardErrorPath</key>
  <string>${INSTALL_DIR}/server.log</string>
</dict>
</plist>
PLISTEOF

  launchctl unload "$PLIST_FILE" 2>/dev/null || true
  launchctl load "$PLIST_FILE"
  echo "🚀 Server started via LaunchAgent (auto-starts on login)"

elif [ "$(uname)" = "Linux" ]; then
  if command -v systemctl >/dev/null 2>&1; then
    SERVICE_DIR="$HOME/.config/systemd/user"
    SERVICE_FILE="$SERVICE_DIR/openclaw-dashboard.service"

    mkdir -p "$SERVICE_DIR"
    cat > "$SERVICE_FILE" << SERVICEEOF
[Unit]
Description=OpenClaw Dashboard Server
After=network.target

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/openclaw-dashboard --port 8080
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
    echo "   cd $INSTALL_DIR && ./openclaw-dashboard --port 8080 &"
  fi
else
  echo "⚠️  Unknown OS. Start manually:"
  echo "   cd $INSTALL_DIR && ./openclaw-dashboard --port 8080 &"
fi

echo ""
echo "✅ Installation complete!"
echo ""
echo "📊 Dashboard: http://127.0.0.1:8080"
echo "🔄 API:       http://127.0.0.1:8080/api/refresh (on-demand refresh)"
echo "⚙️  Config:    $INSTALL_DIR/config.json"
echo "📚 Docs:      $INSTALL_DIR/docs/CONFIGURATION.md"
echo ""
echo "The Go binary serves the dashboard AND refreshes data on-demand"
echo "when you open the page. No separate cron job needed!"
