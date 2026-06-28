#!/usr/bin/env bash
set -euo pipefail

REPO="samcharles93/tau"
BASE_URL="https://github.com/${REPO}/releases"

# ---------- helpers ----------
bold()   { printf '\033[1m%s\033[0m' "$*"; }
green()  { printf '\033[32m%s\033[0m' "$*"; }
yellow() { printf '\033[33m%s\033[0m' "$*"; }
red()    { printf '\033[31m%s\033[0m' "$*"; }

# ---------- detect OS / arch ----------
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *)
    echo "$(red 'Error'): unsupported architecture: $ARCH"
    exit 1
    ;;
esac

case "$OS" in
  darwin)  PLATFORM="${OS}_${ARCH}" ;;
  linux)   PLATFORM="${OS}_${ARCH}"   ;;
  *)
    echo "$(red 'Error'): unsupported OS: $OS"
    exit 1
    ;;
esac

# ---------- resolve version ----------
if [ -n "${TAU_VERSION:-}" ]; then
  VERSION="$TAU_VERSION"
else
  echo "$(bold '→') Fetching latest release..."
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name":' \
    | sed -E 's/.*"([^"]+)".*/\1/')
fi

if [ -z "$VERSION" ]; then
  echo "$(red 'Error'): could not determine latest version"
  exit 1
fi

ARCHIVE="tau_${VERSION#v}_${PLATFORM}.tar.gz"
DOWNLOAD_URL="${BASE_URL}/download/${VERSION}/${ARCHIVE}"

# ---------- install dir ----------
if [ -w /usr/local/bin ]; then
  INSTALL_DIR=/usr/local/bin
elif [ -n "${XDG_BIN_HOME:-}" ]; then
  INSTALL_DIR="$XDG_BIN_HOME"
else
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

# ---------- download & extract & install ----------
echo "$(bold '→') Downloading tau ${VERSION} (${PLATFORM})..."
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

curl -fsSL# -o "$TMP/$ARCHIVE" "$DOWNLOAD_URL"
tar -xzf "$TMP/$ARCHIVE" -C "$TMP"
chmod +x "$TMP/tau"
mv "$TMP/tau" "${INSTALL_DIR}/tau"

echo ""
echo "$(green '✓') tau ${VERSION} installed to ${INSTALL_DIR}/tau"

# ---------- PATH reminder ----------
if ! echo "$PATH" | grep -qF "$INSTALL_DIR"; then
  echo ""
  echo "$(yellow 'Note'): $(bold "$INSTALL_DIR") is not on your PATH."
  echo "  Add this to your shell config:"
  echo ""
  echo "    export PATH=\"$INSTALL_DIR:\$PATH\""
  echo ""
fi

echo "  Run $(bold 'tau --help') to get started."
