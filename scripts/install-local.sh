#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="$HOME/.local/bin"
DATA_DIR="$HOME/.openclaw-observatory"
LOG_DIR="$DATA_DIR/logs"
PLIST="$HOME/Library/LaunchAgents/local.openclaw.observatory.plist"
LABEL="local.openclaw.observatory"

mkdir -p "$BIN_DIR" "$DATA_DIR" "$LOG_DIR" "$HOME/Library/LaunchAgents"
chmod 700 "$DATA_DIR" "$LOG_DIR"

(cd "$ROOT" && go build -o "$BIN_DIR/openclaw-observatoryd" ./cmd/observatoryd)

sed \
  -e "s|__BIN__|$BIN_DIR/openclaw-observatoryd|g" \
  -e "s|__LOG_DIR__|$LOG_DIR|g" \
  "$ROOT/deploy/local.openclaw.observatory.plist.template" > "$PLIST"

launchctl bootout "gui/$(id -u)" "$PLIST" >/dev/null 2>&1 || \
  launchctl bootout "gui/$(id -u)/$LABEL" >/dev/null 2>&1 || true
for _ in 1 2 3 4 5; do
  if ! launchctl print "gui/$(id -u)/$LABEL" >/dev/null 2>&1; then break; fi
  sleep 0.2
done
if ! launchctl bootstrap "gui/$(id -u)" "$PLIST"; then
  sleep 1
  launchctl bootstrap "gui/$(id -u)" "$PLIST"
fi
launchctl kickstart -k "gui/$(id -u)/$LABEL"

if ! openclaw plugins inspect openclaw-observatory >/dev/null 2>&1; then
  openclaw plugins install "$ROOT/plugin" --link
fi
openclaw plugins enable openclaw-observatory
openclaw gateway restart

echo "OpenClaw Observatory installed."
echo "Dashboard: http://127.0.0.1:10086/"
echo "Logs: $LOG_DIR"
