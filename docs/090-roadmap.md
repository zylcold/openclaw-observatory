# Roadmap

## Phase 0 — Architecture and Contracts

- runtime and ownership model;
- versioned event Schema and compatibility negotiation;
- metrics and API contracts;
- privacy threat model and cardinality policy.

Exit: valid Schema examples, linked documents, and reviewed compatibility map.

## Phase 1 — Local MVP

- OpenClaw monitor plugin;
- Go daemon and Unix-socket ingestion;
- idempotent SQLite projections;
- macOS resource sampling;
- `/metrics`, REST, SSE, built-in UI, and baseline Grafana dashboard;
- LaunchAgent installer and end-to-end smoke test.

Exit: a real OpenClaw run appears in SQLite/API without content capture and
daemon outage does not interrupt the run.

## Phase 2 — Product Dashboard

- richer Web UI and live timeline;
- session/run detail;
- interactive resource charts;
- categorical error explorer;
- retention controls and database maintenance UX.

Exit: common diagnosis flows need no direct SQL or Grafana access.

## Phase 3 — Advanced Observability

- metadata-only session replay;
- OpenTelemetry trace export and correlation;
- Loki/Tempo integration;
- authenticated remote mode;
- multi-instance discovery and federation;
- opt-in encrypted content capture only after a separate security design.

Exit: remote/multi-instance operation has authentication, authorization,
encryption, audit logging, and documented upgrade/rollback behavior.

## OpenClaw interfaces still requiring verification

- behavior on versions older/newer than `2026.6.11`;
- whether all external harnesses emit canonical `tool.execution.*` events;
- stable MCP owner/server identity beyond authoritative `toolSource: "mcp"`;
- exact terminal behavior for hard process kill and host sleep/wake;
- provider-specific cost/token completeness.
