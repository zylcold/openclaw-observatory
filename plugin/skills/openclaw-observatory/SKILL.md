---
name: openclaw-observatory
description: Inspect and explain local OpenClaw Observatory health, sessions, agent runs, LLM calls, tools, failures, and process resource usage.
---

# OpenClaw Observatory

Use privacy-safe metadata from the local Observatory service. Never claim to
have seen Prompt text, Tool arguments/output, shell commands, file content, or
raw chat identity: the service intentionally does not collect them.

## Query workflow

1. Call `observatory_query` with `resource: "status"` first.
2. If unavailable, report that the local daemon may be stopped. Do not invent
   state. If the `exec` tool is available, use the fallback script:
   `bash {baseDir}/scripts/observatory.sh status`.
3. Narrow follow-up queries by time, instance, status, or event type. Default to
   20 rows and avoid fetching broad histories without a reason.
4. Correlate evidence in this order: Gateway/instance → Session → Agent Run →
   LLM/Tool/MCP activity → process resources.
5. State timestamps in UTC and distinguish observed facts from inferences.

Use these resources:

- `status`: health, active counts, database/event counts.
- `sessions` / `session`: lifecycle and runs within one session.
- `runs` / `run`: one run and its LLM, Tool, and MCP calls.
- `events`: exact recent metadata lifecycle events; filter `eventType`.
- `resources`: CPU time, RSS, virtual memory, threads, and file descriptors.
- `tools` / `models`: bounded aggregate usage, failures, duration, tokens, cost.

Read `references/api.md` when building a complex query or interpreting status
and lifecycle fields.

## Diagnosis rules

- Treat `gateway_up = 0`, `gateway.crashed`, or a stale last-seen time as an
  availability issue. A running daemon is not proof that the Gateway is up.
- Treat `monitor.events_dropped` as incomplete evidence and disclose the gap.
- Treat active rows without recent progress as possibly long-running first;
  call them stuck only when stall/recovery evidence supports it.
- Compare resource samples over time. A single high RSS sample is not proof of
  a leak; sustained growth across comparable workload is stronger evidence.
- Compute error rates with both failures and total calls. Do not present raw
  failure counts as rates.
- Cost/token totals are provider-reported and may be incomplete. Label them as
  reported values.
- MCP is authoritative only when the source is reported as `mcp`; do not infer
  it from a Tool name.

## Safety

Perform read-only queries. Do not access the SQLite file directly, expose the
service beyond localhost, restart OpenClaw, delete data, or change retention
unless the user separately asks for that action.
