#!/usr/bin/env bash
set -euo pipefail

base="http://127.0.0.1:10086"
resource="${1:-status}"
arg="${2:-}"

case "$resource" in
  status) path="/api/v1/status" ;;
  instances) path="/api/v1/instances" ;;
  sessions) path="/api/v1/sessions" ;;
  runs) path="/api/v1/runs" ;;
  resources) path="/api/v1/resources" ;;
  tools) path="/api/v1/tools/stats" ;;
  models) path="/api/v1/models/stats" ;;
  events) path="/api/v1/events" ;;
  session|run)
    if [[ -z "$arg" ]]; then echo "usage: $0 $resource <id>" >&2; exit 2; fi
    encoded="$(jq -rn --arg value "$arg" '$value|@uri')"
    if [[ "$resource" == "session" ]]; then path="/api/v1/sessions/$encoded"; else path="/api/v1/runs/$encoded"; fi
    ;;
  *) echo "unsupported resource: $resource" >&2; exit 2 ;;
esac

if [[ "$resource" =~ ^(sessions|runs|resources|events)$ ]]; then
  limit="${arg:-20}"
  if ! [[ "$limit" =~ ^[0-9]+$ ]] || (( limit < 1 || limit > 100 )); then echo "limit must be 1-100" >&2; exit 2; fi
  curl --fail --silent --show-error --get "$base$path" --data-urlencode "limit=$limit" | jq .
else
  curl --fail --silent --show-error "$base$path" | jq .
fi
