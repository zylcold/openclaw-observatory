#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="$HOME/.local/bin"
DATA_DIR="$HOME/.openclaw-observatory"
LOG_DIR="$DATA_DIR/logs"
SHARE_DIR="$HOME/.local/share/openclaw-observatory"
WEB_DIR="$SHARE_DIR/web"
BACKEND_PLIST="$HOME/Library/LaunchAgents/local.openclaw.observatory.plist"
WEB_PLIST="$HOME/Library/LaunchAgents/local.openclaw.observatory-web.plist"
BACKEND_LABEL="local.openclaw.observatory"
WEB_LABEL="local.openclaw.observatory-web"
BUILD_ID="${OBSERVATORY_BUILD_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo dev)}"
WEB_ROOT="$WEB_DIR/current"

mkdir -p "$BIN_DIR" "$DATA_DIR" "$LOG_DIR" "$WEB_DIR/releases" "$HOME/Library/LaunchAgents"
chmod 700 "$DATA_DIR" "$LOG_DIR"

OBSERVATORY_BUILD_ID="$BUILD_ID" "$ROOT/scripts/publish-web.sh"

LDFLAGS="-X github.com/zylcold/openclaw-observatory/internal/server.BuildID=$BUILD_ID"
WEB_LDFLAGS="-X main.buildID=$BUILD_ID"
(cd "$ROOT" && go build -ldflags "$LDFLAGS" -o "$BIN_DIR/openclaw-observatoryd" ./cmd/observatoryd)
(cd "$ROOT" && go build -ldflags "$WEB_LDFLAGS" -o "$BIN_DIR/openclaw-observatory-web" ./cmd/observatory-web)

sed \
  -e "s|__BIN__|$BIN_DIR/openclaw-observatoryd|g" \
  -e "s|__LOG_DIR__|$LOG_DIR|g" \
  "$ROOT/deploy/local.openclaw.observatory.plist.template" > "$BACKEND_PLIST"
sed \
  -e "s|__WEB_BIN__|$BIN_DIR/openclaw-observatory-web|g" \
  -e "s|__WEB_ROOT__|$WEB_ROOT|g" \
  -e "s|__LOG_DIR__|$LOG_DIR|g" \
  "$ROOT/deploy/local.openclaw.observatory-web.plist.template" > "$WEB_PLIST"

for service in "$WEB_LABEL" "$BACKEND_LABEL"; do
  launchctl bootout "gui/$(id -u)/$service" >/dev/null 2>&1 || true
done

bootstrap_service() {
  local label="$1"
  local plist="$2"
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if launchctl bootstrap "gui/$(id -u)" "$plist" >/dev/null 2>&1; then
      launchctl kickstart -k "gui/$(id -u)/$label"
      return 0
    fi
    sleep 0.5
  done
  launchctl bootstrap "gui/$(id -u)" "$plist"
}

bootstrap_service "$BACKEND_LABEL" "$BACKEND_PLIST"
bootstrap_service "$WEB_LABEL" "$WEB_PLIST"

if ! openclaw plugins inspect openclaw-observatory >/dev/null 2>&1; then
  openclaw plugins install "$ROOT/plugin" --link
fi
openclaw plugins enable openclaw-observatory
openclaw gateway restart

for _ in 1 2 3 4 5 6 7 8 9 10; do
  if curl -fsS --max-time 2 http://127.0.0.1:10086/health >/dev/null 2>&1; then break; fi
  sleep 0.5
done
STATUS="$(curl -fsS --max-time 3 http://127.0.0.1:10086/api/v1/status)"
PAGE="$(curl -fsS --max-time 3 http://127.0.0.1:10086/)"
if [[ "$STATUS" != *'"apiVersion":2'* || "$STATUS" != *'"schemaVersion":3'* || "$STATUS" != *'agent-timeline-v2'* ]]; then
  echo "Observatory backend compatibility check failed: $STATUS" >&2
  exit 1
fi
if [[ "$PAGE" != *'>AGENT<'* || "$PAGE" == *'>RUN ID<'* ]]; then
  echo "Observatory frontend build verification failed." >&2
  exit 1
fi

echo "OpenClaw Observatory installed."
echo "Dashboard: http://127.0.0.1:10086/"
echo "Backend API: http://127.0.0.1:10087/"
echo "Build: $BUILD_ID"
echo "Logs: $LOG_DIR"
