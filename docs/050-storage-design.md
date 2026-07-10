# Storage Design

SQLite is the local system of record. The daemon uses one logical writer,
short transactions, WAL mode, `foreign_keys=ON`, and a busy timeout.

## Tables

| Table | Primary/foreign keys and important fields | Indexes |
| --- | --- | --- |
| `instances` | `instance_id` PK; version, PID, status, start/last-seen/stop | status, last_seen |
| `sessions` | `(instance_id, session_id)` PK; agent/start/end/status/reason/message_count; FK instance | agent, start, status |
| `agent_runs` | `(instance_id, run_id)` PK; `session_id` FK; agent/provider/model/status/times/duration | agent, session, start, status |
| `llm_calls` | `(instance_id, call_id)` PK; `run_id` FK; provider/model/status/times/usage/cost | run, provider+model, start |
| `tool_calls` | `(instance_id, tool_call_id)` PK; `run_id` FK; tool/source/status/duration | run, tool+status, start |
| `mcp_calls` | same lifecycle fields plus bounded owner/server | run, tool+status |
| `subagent_runs` | `(instance_id, subagent_id)` PK; parent/child session/run/status | parent run, child session |
| `resource_samples` | integer PK; instance/PID/time, process measurements, disk capacity/available bytes | instance+time |
| `events` | `event_id` PK; envelope columns and payload JSON | instance+time, type+time, producer+sequence |
| `daily_stats` | `(day, instance_id, dimension_type, dimension_value)` PK; aggregate JSON | instance+day |
| `schema_migrations` | `version` PK, applied time | none |

Foreign keys are deferrable in the conceptual model because terminal/out-of-
order events may create placeholder parents. The MVP reducer uses upserts and
nullable parent references to avoid rejecting useful terminal facts.

## Event and projection relationship

`events` is the immutable metadata ledger. Projection tables are query-optimized
state. Inserting the event and updating projections occurs in one transaction:

1. `INSERT ... ON CONFLICT(event_id) DO NOTHING`;
2. if inserted, run the event-type reducer;
3. commit both or neither.

This makes duplicate delivery safe. Projections can later be rebuilt by replaying
events in `(occurred_at, producer_id, sequence)` order.

## Crash recovery

WAL protects committed transactions. On restart, active rows are compared with
instance liveness. If their Gateway is confirmed dead, they become `incomplete`
rather than falsely `failed`; a synthesized `gateway.crashed` records why.

## Migrations

Migrations are ordered, embedded SQL transactions. The daemon refuses to run
against a database newer than its supported schema. Destructive migrations use
create-copy-verify-rename and are never mixed with retention work.

Schema v2 adds `agent_id` to sessions and agent runs. Schema v3 backfills those
columns from the immutable event ledger, then falls back to session and
subagent attribution. This makes already-collected runs visible in the
per-Agent timeline immediately after upgrade.
Schema v4 adds time-oriented aggregation indexes. Schema v5 adds host filesystem
capacity and available-byte samples for disk utilization trends.

## Resource retention

- last hour: retain 5-second samples;
- 1–24 hours: aggregate to one minute;
- 1–30 days: aggregate to 15 minutes;
- over 30 days: daily aggregate or delete by operator policy.

Retention runs in small indexed batches. Aggregates are written before raw
samples are deleted, in the same transaction.
