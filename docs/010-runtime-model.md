# Runtime Model

```text
Host
└── OpenClaw Instance
    ├── Gateway
    ├── Workspace
    ├── Session
    │   └── Agent Run
    │       ├── LLM Call
    │       ├── Tool Call
    │       ├── MCP Call
    │       └── Subagent Run
    └── Resource Samples
```

| Object | Definition and lifecycle | Identity and parent | Main properties | Prometheus label? | Persist? |
| --- | --- | --- | --- | --- | --- |
| Host | Machine running the daemon; exists across Gateway restarts | Locally derived anonymous host ID; root | OS, architecture | No raw host ID | Yes, through instance metadata |
| Instance | One OpenClaw state/config installation | Stable hashed instance ID; parent Host | adapter version, last seen, current PID | `instance`, only when bounded | Yes |
| Gateway | One OS process lifetime | `(instanceId, processId, processStart)` | start/stop/crash, version, uptime | instance only | Yes, represented by lifecycle events |
| Workspace | Agent working context | Hashed/canonical workspace identity; parent Instance | safe name/hash only | No | Optional in later schema |
| Session | OpenClaw conversational/runtime session | OpenClaw `sessionId`; parent Instance | state, start/end/reason, message count | No | Yes |
| Agent Run | One accepted agent turn | OpenClaw `runId`; parent Session | provider, model, status, duration, trigger | No | Yes |
| LLM Call | One provider attempt inside a run | OpenClaw `callId`; parent Agent Run | provider, model, timing, outcome, token/cost accounting | provider/model/status only | Yes |
| Tool Call | One tool execution | `toolCallId`, or deterministic event-derived fallback; parent Agent Run | normalized tool, source, duration, outcome | bounded tool/status only | Yes |
| MCP Call | Tool call reported with source `mcp` | Same identity rules; parent Agent Run | server/owner when safely available, duration, outcome | bounded tool/status only | Yes |
| Subagent Run | Child session/run launched by an agent | child session hash + run ID; parent Agent Run/Session | mode, child agent, model, outcome | No | Yes |
| Resource Sample | Point-in-time process measurement | generated event ID; parent Gateway/Instance | CPU time, RSS, VM, threads, FDs, IO | instance only | Yes, downsampled |

## Required distinctions

- **Instance** survives process restarts; **Gateway** is one process lifetime.
- **Session** is a long-lived OpenClaw state boundary; **Agent Run** is one turn.
- An **LLM Call** is one provider attempt. Retries/failover can create multiple
  calls inside a run.
- A **Tool Call** is semantic execution; an **MCP Call** is the subset whose
  source is authoritatively reported as MCP.
- A **Resource Sample** is daemon-observed host data and may exist without any
  active session.

## Identifier handling

Raw session keys can embed routing identity and are hashed in the plugin.
OpenClaw-provided session/run/call IDs are stored for local joins but never used
as metrics labels. PID is never sufficient identity by itself because operating
systems reuse PIDs; reducers pair it with instance and process-start evidence.
