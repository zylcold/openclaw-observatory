# Event Model

## Envelope v1

```typescript
interface MonitorEvent<TPayload> {
  schemaVersion: 1;
  eventId: string;                 // UUID
  eventType: string;               // closed v1 enum
  occurredAt: string;              // UTC RFC3339
  monotonicTimeNs?: number;        // producer-local only
  instanceId: string;
  producerId: string;              // one plugin/daemon boot
  processId?: number;
  sequence: number;                // monotonic per producerId
  source: "plugin" | "daemon";
  payload: Record<string, unknown>;
}
```

`occurredAt` is the cross-process ordering baseline. `sequence` breaks ties and
detects gaps only within one producer. A monotonic value must never be compared
between processes or boots.

## Event types

Gateway: `gateway.started`, `gateway.heartbeat`, `gateway.stopped`,
`gateway.crashed`.

Runtime: `session.started|completed|failed`, `agent.started|completed|failed`,
`llm.started|completed|failed|retried`, `tool.started|completed|failed`,
`mcp.started|completed|failed`, `subagent.started|completed|failed`.

Daemon/monitor: `resource.sampled`, `monitor.events_dropped`,
`monitor.plugin_error`.

Started and terminal events are separate facts. A terminal event never mutates
or replaces the started event.

## Ordering, duplicates, and incomplete operations

- The daemon inserts `eventId` with a unique constraint. A duplicate returns a
  successful acknowledgement but does not rerun reducers.
- Events may arrive out of order. Upserts use the earliest start and latest
  terminal timestamp while retaining the raw events.
- Entities can be created by a terminal event if the corresponding start was
  lost.
- On daemon startup, rows left active are not immediately failed. Gateway
  liveness reconciliation decides whether they are still possible or marks
  them incomplete/crashed.
- Event timestamp regressions are retained but flagged; producer sequence order
  is preferred within a boot.

## Version negotiation

`gateway.started` includes `protocolVersions: [1]`, plugin/OpenClaw versions,
and detected capability names. The daemon rejects unsupported schema versions
with HTTP `422`. A future plugin must select the highest mutually supported
version before sending non-startup events.

## Privacy and bounds

The v1 allowlist has no fields for Prompt, response, Tool arguments/results,
shell commands, file content/path, raw chat identity, or free-form error text.
Error values are controlled categories/codes. Each serialized event is limited
to 16 KiB by the plugin and daemon; each batch is limited to 1 MiB. JSON Schema
adds string/array/property limits, while the receiver enforces byte size and
decoder limits that Schema cannot express.

See [`../schemas/monitor-event.schema.json`](../schemas/monitor-event.schema.json)
for the machine-readable contract and examples.
