#!/bin/sh
# Installs the latest duck release: a standalone statically linked binary,
# no dependencies. Usage: curl -fsSL https://raw.githubusercontent.com/sonirico/duck/main/install.sh | sh
set -eu

REPO=sonirico/duck
INSTALL_DIR=${DUCK_INSTALL_DIR:-"$HOME/.local/bin"}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $os in
  linux | darwin) ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

case $(uname -m) in
  x86_64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

mkdir -p "$INSTALL_DIR"
curl -fsSL "https://github.com/$REPO/releases/latest/download/duck_${os}_${arch}" -o "$INSTALL_DIR/duck"
chmod +x "$INSTALL_DIR/duck"

echo "duck installed to $INSTALL_DIR/duck"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: $INSTALL_DIR is not on your PATH" ;;
esac
