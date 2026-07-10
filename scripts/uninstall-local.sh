#!/usr/bin/env bash
set -euo pipefail

for label in local.openclaw.observatory-web local.openclaw.observatory; do
  launchctl bootout "gui/$(id -u)/$label" >/dev/null 2>&1 || true
  rm -f "$HOME/Library/LaunchAgents/$label.plist"
done
rm -f "$HOME/.local/bin/openclaw-observatoryd" "$HOME/.local/bin/openclaw-observatory-web"
rm -rf "$HOME/.local/share/openclaw-observatory/web"
openclaw plugins disable openclaw-observatory >/dev/null 2>&1 || true
openclaw plugins uninstall openclaw-observatory --force >/dev/null 2>&1 || true
openclaw gateway restart
echo "Services removed. Database preserved at $HOME/.openclaw-observatory/observatory.db"
