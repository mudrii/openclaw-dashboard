#!/usr/bin/env bash
# OpenClaw Dashboard Installer (Go binary)
# Supports: macOS, Linux
# Usage: curl -fsSL https://raw.githubusercontent.com/mudrii/openclaw-dashboard/main/install.sh | bash

set -euo pipefail

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

mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR"

# Download or build the Go binary
ARCHIVE_NAME="openclaw-dashboard-${OS}-${ARCH}.tar.gz"
ARCHIVE_URL="$REPO/releases/latest/download/$ARCHIVE_NAME"
echo "📦 Downloading release archive ($ARCHIVE_NAME)..."
if tmp_archive="$(mktemp "${TMPDIR:-/tmp}/openclaw-dashboard.XXXXXX.tar.gz")"; then
  cleanup_archive() {
    rm -f "$tmp_archive"
  }
  trap cleanup_archive EXIT
else
  echo "❌ Could not create temporary archive file"
  exit 1
fi

if curl -fsSL "$ARCHIVE_URL" -o "$tmp_archive" 2>/dev/null; then
  tar -xzf "$tmp_archive" -C "$INSTALL_DIR"
  if [ ! -f "openclaw-dashboard" ]; then
    echo "❌ Release archive did not contain openclaw-dashboard"
    exit 1
  fi
  if [ ! -f "assets/runtime/refresh.sh" ]; then
    echo "❌ Release archive did not contain assets/runtime/refresh.sh"
    exit 1
  fi
  chmod +x openclaw-dashboard assets/runtime/refresh.sh
  echo "✅ Release archive downloaded"
elif command -v go >/dev/null 2>&1; then
  echo "⚠️  Download failed, building from source..."
  curl -fsSL "$REPO/archive/main.tar.gz" | tar -xz --strip-components=1 -C "$INSTALL_DIR"
  go build -ldflags="-s -w" -o openclaw-dashboard ./cmd/openclaw-dashboard
  echo "✅ Binary built from source"
else
  echo "❌ Could not download binary and 'go' is not available to build from source"
  exit 1
fi

# Seed runtime assets into the install root
if [ ! -f "assets/runtime/refresh.sh" ]; then
  echo "❌ Missing runtime asset: assets/runtime/refresh.sh"
  exit 1
fi
cp assets/runtime/refresh.sh ./refresh.sh
chmod +x refresh.sh

# Create config if not exists
if [ ! -f "config.json" ]; then
  echo "📝 Creating default config.json..."
  if [ -f "assets/runtime/config.json" ]; then
    cp assets/runtime/config.json config.json
  elif [ -f "examples/config.minimal.json" ]; then
    cp examples/config.minimal.json config.json
  else
    echo '{"bot":{"name":"OpenClaw Dashboard","emoji":"🦞"},"server":{"port":8080}}' > config.json
  fi
  echo "   Edit config.json to customize your dashboard"
fi

# Initial data refresh
echo "🔄 Running initial data refresh..."
./openclaw-dashboard --refresh

# Setup auto-start using the binary's built-in service management
echo ""
if ./openclaw-dashboard install; then
  sleep 2
  status_output="$(./openclaw-dashboard status || true)"
  if printf '%s\n' "$status_output" | grep -q '^Status:     running$'; then
    echo "🚀 Server installed and started as a background service"
  else
    echo "⚠️  Service was installed but is not healthy yet:"
    printf '%s\n' "$status_output"
    echo ""
    echo "   Check ~/.openclaw/dashboard/server.log for details."
  fi
else
  echo "⚠️  Automatic service installation failed. Start manually:"
  echo "   cd $INSTALL_DIR && ./openclaw-dashboard --port 8080"
fi

echo ""
echo "✅ Installation complete!"
echo ""
echo "📊 Dashboard: http://127.0.0.1:8080"
echo "🔄 API:       http://127.0.0.1:8080/api/refresh (on-demand refresh)"
echo "⚙️  Config:    $INSTALL_DIR/config.json"
echo "📚 Docs:      $INSTALL_DIR/README.md"
echo ""
echo "The Go binary serves the dashboard AND refreshes data on-demand"
echo "when you open the page. No separate cron job needed!"
