# Prometheus 指标

Prometheus 仅存储聚合。会话、运行、调用、用户、请求、路径、Prompt、命令和错误消息值禁止作为标签。

## 契约

| 指标 | 类型 | 允许的标签 |
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

持续时间单位为秒，存储单位为字节，比率范围为 0-1，计数器以 `_total` 结尾。直方图实现暴露 `_bucket`、`_sum` 和 `_count`。

虽然 `model` 和 `tool` 是有用的维度，但它们本身不是有界的。守护进程会规范化值、保留配置的白名单/Top-N，并将溢出映射到 `other`。内存中的总序列数有上限；溢出会增加监控丢弃/错误计数器而非无限增长。

## PromQL 示例

```promql
# Gateway 可用性
min_over_time(openclaw_gateway_up[5m])

# Agent 运行 P95
histogram_quantile(0.95,
  sum by (le, instance) (rate(openclaw_agent_run_duration_seconds_bucket[5m])))

# LLM 错误率
sum(rate(openclaw_llm_errors_total[5m]))
/
clamp_min(sum(rate(openclaw_llm_requests_total[5m])), 1)

# 按提供商统计最近一小时的 Token 用量
sum by (provider, direction) (increase(openclaw_llm_tokens_total[1h]))

# 按平均延迟排列的最慢工具
topk(10,
  sum by (tool) (rate(openclaw_tool_call_duration_seconds_sum[10m]))
  /
  clamp_min(sum by (tool) (rate(openclaw_tool_call_duration_seconds_count[10m])), 1))

# 监控丢失
sum(increase(openclaw_monitor_events_dropped_total[15m])) > 0
```
