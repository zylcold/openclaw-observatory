#!/usr/bin/env bash
set -euo pipefail

LABEL="local.openclaw.observatory"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
launchctl bootout "gui/$(id -u)/$LABEL" >/dev/null 2>&1 || true
rm -f "$PLIST" "$HOME/.local/bin/openclaw-observatoryd"
openclaw plugins disable openclaw-observatory >/dev/null 2>&1 || true
openclaw plugins uninstall openclaw-observatory --force >/dev/null 2>&1 || true
openclaw gateway restart
echo "Services removed. Database preserved at $HOME/.openclaw-observatory/observatory.db"
