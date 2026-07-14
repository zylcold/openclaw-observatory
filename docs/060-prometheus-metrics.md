# Prometheus Metrics

Prometheus stores aggregates only. Session, run, call, user, request, path,
Prompt, command, and error-message values are forbidden as labels.

## Contract

| Metric | Type | Allowed labels |
| --- | --- | --- |
| `openclaw_gateway_up` | gauge | `instance` |
| `openclaw_gateway_uptime_seconds` | gauge | `instance` |
| `openclaw_gateway_restarts_total` | counter | `instance` |
| `openclaw_sessions_active` | gauge | `instance` |
| `openclaw_agent_runs_active` | gauge | `instance` |
| `openclaw_agent_runs_total` | counter | `instance,status` |
| `openclaw_agent_run_duration_seconds` | histogram | `instance,status` |
| `openclaw_llm_requests_total` | counter | `instance,provider,model,status` |
| `openclaw_llm_tokens_total` | counter | `instance,provider,model,direction` |
| `openclaw_llm_request_duration_seconds` | histogram | `instance,provider,model,status` |
| `openclaw_llm_errors_total` | counter | `instance,provider,model,reason` |
| `openclaw_llm_cost_usd_total` | counter | `instance,provider,model` |
| `openclaw_tool_calls_total` | counter | `instance,tool,status` |
| `openclaw_tool_call_duration_seconds` | histogram | `instance,tool,status` |
| `openclaw_tool_errors_total` | counter | `instance,tool,reason` |
| `openclaw_process_cpu_seconds_total` | counter | `instance` |
| `openclaw_process_resident_memory_bytes` | gauge | `instance` |
| `openclaw_process_virtual_memory_bytes` | gauge | `instance` |
| `openclaw_process_threads` | gauge | `instance` |
| `openclaw_process_open_fds` | gauge | `instance` |
| `openclaw_process_read_bytes_total` | counter | `instance` |
| `openclaw_process_write_bytes_total` | counter | `instance` |
| `openclaw_monitor_events_received_total` | counter | `instance,event_type` |
| `openclaw_monitor_events_dropped_total` | counter | `instance,reason` |
| `openclaw_monitor_event_queue_depth` | gauge | `instance` |
| `openclaw_monitor_insert_duration_seconds` | summary | none |
| `openclaw_monitor_reduce_duration_seconds` | summary | none |
| `openclaw_monitor_commit_duration_seconds` | summary | none |
| `openclaw_monitor_query_duration_seconds` | summary | none |

Durations are seconds, storage is bytes, ratios range from 0–1, and counters end
in `_total`. Histogram implementations expose `_bucket`, `_sum`, and `_count`.

Although `model` and `tool` are useful dimensions, they are not intrinsically
bounded. The daemon normalizes values, retains a configured allowlist/top-N, and
maps overflow to `other`. Total in-memory series are capped; overflow increments
a monitor drop/error counter rather than growing without limit.

## PromQL examples

```promql
# Gateway availability
min_over_time(openclaw_gateway_up[5m])

# Agent run P95
histogram_quantile(0.95,
  sum by (le, instance) (rate(openclaw_agent_run_duration_seconds_bucket[5m])))

# LLM error rate
sum(rate(openclaw_llm_errors_total[5m]))
/
clamp_min(sum(rate(openclaw_llm_requests_total[5m])), 1)

# Token volume by provider in the last hour
sum by (provider, direction) (increase(openclaw_llm_tokens_total[1h]))

# Slowest tools by mean latency
topk(10,
  sum by (tool) (rate(openclaw_tool_call_duration_seconds_sum[10m]))
  /
  clamp_min(sum by (tool) (rate(openclaw_tool_call_duration_seconds_count[10m])), 1))

# Monitor loss
sum(increase(openclaw_monitor_events_dropped_total[15m])) > 0
```
