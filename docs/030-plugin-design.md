# Monitor Plugin Design

## Responsibilities

The plugin:

1. detects the supported OpenClaw diagnostic and typed-hook surfaces;
2. maps verified lifecycle data into protocol v1;
3. hashes session routing keys and selects only safe fields;
4. performs an O(1) enqueue on the OpenClaw path;
5. batches delivery asynchronously over the Observatory Unix socket;
6. degrades silently when the daemon is absent.

It does not open SQLite, sample system resources, aggregate Prometheus metrics,
render dashboards, retain events on disk, perform long retries, or synchronously
wait for the daemon.

The package also registers the read-only `observatory_query` Tool and bundles
the `openclaw-observatory` Skill. The Tool accepts a closed resource enum,
connects only to `127.0.0.1:10086`, performs GET requests, and rejects responses
over 64 KiB. It is a query surface, not part of event collection.

## OpenClaw 2026.6.11 adapter

| Observatory events | OpenClaw source |
| --- | --- |
| Gateway start/stop | plugin service lifecycle |
| Session start/end | `session_start`, `session_end` observation hooks |
| Agent start/end | `run.started`, `run.completed` diagnostics |
| LLM start/end/error/retry | `model.call.*`, `model.failover` diagnostics |
| Tool start/end/error | `tool.execution.*` diagnostics |
| MCP start/end/error | Tool diagnostic with `toolSource === "mcp"` |
| Subagent start/end | `subagent_spawned`, `subagent_ended` hooks |
| Heartbeat/drop | diagnostic heartbeat/queue-drop events |

Unknown SDK event names are ignored. No API is fabricated.

The adapter can copy normalized numeric usage buckets (`input`, `output`,
`cacheRead`, and `cacheWrite`) from the supported `llm_output` hook when
`plugins.entries.openclaw-observatory.hooks.allowConversationAccess=true` is
explicitly configured. Although that hook's event also contains conversation
fields, the handler passes only bounded identifiers and numeric usage to the
forwarder; prompt, response, and reasoning content are never queued. Usage is
matched to the last call by run ID, stable hashed session key, or session ID.

## Queue and failure policy

- Default capacity: 10,000 events.
- Single-event cap: 16 KiB; batch cap: 1 MiB/100 events.
- Flush interval: 250 ms when healthy, bounded backoff to 5 seconds on failure.
- Critical: crash and lifecycle terminal events.
- Normal: Agent, LLM, Tool, MCP, Session, and Subagent lifecycle.
- Low: heartbeats and high-frequency intermediate events.
- When full, remove the oldest Low event before dropping Normal. Critical is
  dropped only when no lower-priority entry exists.
- Dropped counts are coalesced into `monitor.events_dropped` when capacity is
  available again.

Hook handlers never await transport. HTTP socket callbacks are owned by the
background flusher. Delivery failures are logged at a rate-limited debug/warn
level and cannot propagate into OpenClaw.

## Performance targets

- synchronous handler P95 below 1 ms;
- plugin-owned memory below 20 MB at default capacity;
- daemon absence adds no agent latency;
- no Promise returned by observation handlers for transport work.

The test suite verifies mapping and queue policy; production profiling should
measure the P95 target under 10,000-event bursts.
