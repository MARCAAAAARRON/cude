#!/usr/bin/env bash
# CUDE installer — downloads the latest release binary from GitHub.
# Usage:
#   curl -sSL https://raw.githubusercontent.com/MARCAAAAARRON/cude/main/scripts/install.sh | bash
#
set -euo pipefail

REPO="MARCAAAAARRON/cude"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS / Arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "[x] Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "[*] Detecting latest release for ${OS}/${ARCH}..."

# Get latest tag from GitHub API
LATEST=$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$LATEST" ]; then
  echo "[x] Could not determine latest release. Check https://github.com/${REPO}/releases"
  exit 1
fi

VERSION="${LATEST#v}"
TARBALL="cude_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${LATEST}/${TARBALL}"

echo "[*] Downloading cude ${LATEST} (${TARBALL})..."

TMP=$(mktemp -d)
trap "rm -rf $TMP" EXIT

curl -sSL "$URL" -o "${TMP}/${TARBALL}"
tar -xzf "${TMP}/${TARBALL}" -C "$TMP"

echo "[*] Installing to ${INSTALL_DIR}/cude ..."
sudo install -m 755 "${TMP}/cude" "${INSTALL_DIR}/cude"

echo "[*] Done! Run 'cude --version' to verify."
