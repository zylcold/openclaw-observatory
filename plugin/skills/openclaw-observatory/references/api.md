# Observatory API reference

Base URL: `http://127.0.0.1:10086`. All endpoints below are read-only.

| Tool resource | REST endpoint | Useful filters |
| --- | --- | --- |
| `status` | `/api/v1/status` | none |
| `instances` | `/api/v1/instances` | none |
| `sessions` | `/api/v1/sessions` | `limit`, `instanceId`, `status`, `from`, `to` |
| `session` | `/api/v1/sessions/{id}` | `id` required |
| `runs` | `/api/v1/runs` | `limit`, `instanceId`, `status`, `from`, `to` |
| `run` | `/api/v1/runs/{id}` | `id` required |
| `resources` | `/api/v1/resources` | `limit`, `instanceId`, `from`, `to` |
| `tools` | `/api/v1/tools/stats` | none |
| `models` | `/api/v1/models/stats` | none |
| `events` | `/api/v1/events` | `limit`, `instanceId`, `eventType`, `from`, `to` |

Time filters are UTC RFC3339. List limits are 1–100 through the Agent tool.

Lifecycle status:

- `active`: a start was observed without a terminal event;
- `completed`: a successful terminal event was observed;
- `failed`: a categorized failure/abort/block terminal event was observed;
- `unknown` or `incomplete`: evidence is insufficient, commonly after loss or
  crash recovery.

Resource fields:

- `cpuSecondsTotal`: cumulative process CPU, not percent;
- `residentMemoryBytes`: physical resident set;
- `virtualMemoryBytes`: reserved virtual address space, often very large on
  macOS and not equivalent to physical pressure;
- `threads`, `openFds`: current sampled counts;
- `readBytesTotal`, `writeBytesTotal`: cumulative IO when supported; zero can
  mean unavailable rather than no IO.

The API omits raw Prompt, response, Tool input/output, commands, paths, chat
identity, and free-form error text. Do not promise analysis of absent content.
