#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WEB_DIR="$HOME/.local/share/openclaw-observatory/web"
BUILD_ID="${OBSERVATORY_BUILD_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo dev)}"
RELEASE_DIR="$WEB_DIR/releases/$BUILD_ID"

mkdir -p "$WEB_DIR/releases"
(cd "$ROOT/web" && npm ci --silent --include=dev && VITE_BUILD_ID="$BUILD_ID" npm run build)
rm -rf "$RELEASE_DIR" "$RELEASE_DIR.tmp"
mkdir -p "$RELEASE_DIR.tmp"
cp -R "$ROOT/web/dist/." "$RELEASE_DIR.tmp/"
mv "$RELEASE_DIR.tmp" "$RELEASE_DIR"
ln -sfn "$RELEASE_DIR" "$WEB_DIR/current"

echo "Frontend published: $BUILD_ID"
echo "Release: $RELEASE_DIR"
