# REST and SSE API

Base URL: `http://127.0.0.1:10086`. Responses use JSON except `/metrics` and
SSE. The default listener is the local web proxy; the backend listens on
`127.0.0.1:10087`.

## Endpoints

| Method/path | Description |
| --- | --- |
| `GET /health` | Process liveness |
| `GET /ready` | Database/listener readiness |
| `GET /metrics` | Prometheus text format |
| `GET /api/v1/status` | Daemon/Gateway/storage summary |
| `GET /api/v1/instances` | Instances |
| `GET /api/v1/sessions` | Sessions |
| `GET /api/v1/sessions/{id}` | Session, runs, and unified LLM/Tool/MCP/Subagent waterfall timeline |
| `GET /api/v1/runs` | Agent runs |
| `GET /api/v1/runs/{id}` | Run with model/tool activity |
| `GET /api/v1/agents/stats` | Runs, tokens, tools, duration, errors, and cost by agent |
| `GET /api/v1/subagents` | Subagent runs |
| `GET /api/v1/llm/calls` | LLM calls, including normalized usage |
| `GET /api/v1/mcp/calls` | MCP calls |
| `GET /api/v1/timeseries` | Bucketed runs, LLM, token, tool, resource, model, and agent series |
| `GET /api/v1/errors/stats` | Error aggregation by source and category |
| `GET /api/v1/resources` | Resource samples |
| `GET /api/v1/tools/stats` | Tool aggregates |
| `GET /api/v1/models/stats` | Model aggregates |
| `GET /api/v1/events` | Metadata events |
| `GET /api/v1/stream` | Live metadata events over SSE |

## Query conventions

- `limit`: 1–500, default 100;
- `cursor`: opaque stable row/timestamp cursor;
- `from`, `to`: UTC RFC3339 inclusive range;
- `instanceId`: exact local instance filter;
- `agentId`: resolved Agent filter;
- `bucket`: `1m`, `5m`, `1h`, or `1d` on `/timeseries` (at most 2,000 buckets);
- endpoint-specific filters such as `status`, `eventType`, `sessionId`.

Lists return newest first:

```json
{
  "data": [{"runId":"run_123","status":"completed"}],
  "page": {"limit":100,"nextCursor":"..."}
}
```

## Errors

```json
{
  "error": {
    "code": "invalid_time_range",
    "message": "from must be earlier than to",
    "requestId": "8b4b14c0-ff4c-4f64-a945-f7e247731f68"
  }
}
```

Codes are stable machine strings; messages contain no database details.
Validation errors are `400`, unsupported protocol is `422`, missing objects are
`404`, oversized bodies are `413`, rate limits are `429`, and unexpected errors
are `500`.

## Status example

```json
{
  "apiVersion":3,
  "schemaVersion":5,
  "capabilities":["agent-stats-v3","session-waterfall-v3","timeseries-v3","dashboard-config-v3","disk-space-v3"],
  "buildId":"20260710T130000Z-abc1234",
  "daemon":{"ready":true,"version":"0.3.0"},
  "gateway":{"up":true,"instanceId":"local-abc123","pid":10207},
  "storage":{"events":832,"databaseBytes":1048576},
  "time":"2026-07-10T10:00:00Z"
}
```

The frontend checks `apiVersion` and `capabilities` before rendering features
that depend on newer projections. A mismatch is shown explicitly instead of
silently falling back to an older visualization.

## Time series

`GET /api/v1/timeseries?from=2026-07-10T00:00:00Z&to=2026-07-11T00:00:00Z&bucket=1h`
returns three aligned sparse series: `points` for system/run/LLM/tool metrics,
`models` for stacked token and cost trends, and `agents` for the activity
heatmap. SQLite performs the bucketing and aggregation; raw rows are not sent to
the browser for trend charts.

## SSE

`GET /api/v1/stream` sends:

```text
event: monitor-event
id: 7cb1a981-17b4-47c1-8307-e7f976d3843b
data: {"schemaVersion":1,"eventType":"tool.completed",...}
```

Heartbeats are comments every 15 seconds. Each client has a small bounded
buffer; a slow client is disconnected rather than blocking ingestion. Clients
reconnect and use REST for gap recovery; the MVP does not guarantee SSE replay
from `Last-Event-ID`.
