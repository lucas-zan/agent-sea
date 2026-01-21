#!/bin/bash

set -e

REPO="lucas-zan/agent-sea"
BINARY_NAME="sea"

# Detect OS
OS="$(uname -s)"
case "$OS" in
    Linux*)     OS_TYPE="linux";;
    Darwin*)    OS_TYPE="darwin";;
    CYGWIN*|MINGW*) OS_TYPE="windows";;
    *)          echo "Unknown OS: $OS"; exit 1;;
esac

# Detect Arch
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)    ARCH_TYPE="amd64";;
    arm64|aarch64)   ARCH_TYPE="arm64";;
    *)         echo "Unknown architecture: $ARCH"; exit 1;;
esac

# Construct Asset Name
ASSET_NAME="sea-${OS_TYPE}-${ARCH_TYPE}"
if [ "$OS_TYPE" == "windows" ]; then
    ASSET_NAME="${ASSET_NAME}.exe"
    BINARY_NAME="${BINARY_NAME}.exe"
fi

echo "🌊 Detected system: $OS_TYPE $ARCH_TYPE"
echo "⬇️  Downloading latest version of sea..."

# GitHub Releases "latest" download URL
DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${ASSET_NAME}"

# Download
curl -fsSL "$DOWNLOAD_URL" -o "$BINARY_NAME"

# Make executable (not needed for windows .exe usually, but good for bash)
chmod +x "$BINARY_NAME"

# Create skills directory
mkdir -p skills

echo "✅ Installed ./$BINARY_NAME"
echo "📂 Created ./skills directory"
echo ""
echo "usage: ./$BINARY_NAME chat"
