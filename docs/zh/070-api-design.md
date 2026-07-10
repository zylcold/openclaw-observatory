# REST 和 SSE API

基础 URL：`http://127.0.0.1:10086`。除 `/metrics` 和 SSE 外，响应使用 JSON。该地址默认由本地 Web 代理监听，后端监听 `127.0.0.1:10087`。

## 端点

| 方法/路径 | 描述 |
| --- | --- |
| `GET /health` | 进程存活 |
| `GET /ready` | 数据库/监听器就绪 |
| `GET /metrics` | Prometheus 文本格式 |
| `GET /api/v1/status` | 守护进程/Gateway/存储摘要 |
| `GET /api/v1/instances` | 实例列表 |
| `GET /api/v1/sessions` | 会话列表 |
| `GET /api/v1/sessions/{id}` | 会话、运行及统一 LLM/Tool/MCP/Subagent 瀑布时间线 |
| `GET /api/v1/runs` | Agent 运行列表 |
| `GET /api/v1/runs/{id}` | 运行及其模型/工具活动 |
| `GET /api/v1/agents/stats` | 按 Agent 聚合运行、Token、工具、耗时、错误和成本 |
| `GET /api/v1/subagents` | Subagent 运行列表 |
| `GET /api/v1/llm/calls` | LLM 调用及标准化用量 |
| `GET /api/v1/mcp/calls` | MCP 调用列表 |
| `GET /api/v1/timeseries` | 按时间桶聚合运行、LLM、Token、工具、资源、模型与 Agent |
| `GET /api/v1/errors/stats` | 按来源和 Category 聚合错误 |
| `GET /api/v1/resources` | 资源采样 |
| `GET /api/v1/tools/stats` | 工具聚合 |
| `GET /api/v1/models/stats` | 模型聚合 |
| `GET /api/v1/events` | 元数据事件 |
| `GET /api/v1/stream` | SSE 实时元数据事件流 |

## 查询约定

- `limit`：1-500，默认 100；
- `cursor`：不透明的稳定行/时间戳游标；
- `from`、`to`：UTC RFC3339 包含范围；
- `instanceId`：精确本地实例过滤；
- `agentId`：解析后的 Agent 过滤；
- `bucket`：`/timeseries` 支持 `1m`、`5m`、`1h`、`1d`，最多 2,000 个时间桶；
- 端点特有过滤器如 `status`、`eventType`、`sessionId`。

列表按最新优先返回：

```json
{
  "data": [{"runId":"run_123","status":"completed"}],
  "page": {"limit":100,"nextCursor":"..."}
}
```

## 错误

```json
{
  "error": {
    "code": "invalid_time_range",
    "message": "from 必须早于 to",
    "requestId": "8b4b14c0-ff4c-4f64-a945-f7e247731f68"
  }
}
```

错误代码是稳定的机器字符串；消息不包含数据库细节。校验错误返回 `400`，不支持的协议返回 `422`，缺失对象返回 `404`，超大请求体返回 `413`，速率限制返回 `429`，未知错误返回 `500`。

## 状态示例

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

前端会在渲染依赖新投影的功能前检查 `apiVersion` 和 `capabilities`。版本不匹配时会明确提示，而不是静默回退到旧可视化。

## 时间序列

`GET /api/v1/timeseries?from=2026-07-10T00:00:00Z&to=2026-07-11T00:00:00Z&bucket=1h`
返回三组对齐的稀疏序列：`points` 用于系统、运行、LLM 与工具指标，`models` 用于堆叠 Token 和成本趋势，`agents` 用于活跃度热力图。时间分桶与聚合在 SQLite 内完成，趋势图不再把原始明细行发送给浏览器。

## SSE

`GET /api/v1/stream` 发送：

```text
event: monitor-event
id: 7cb1a981-17b4-47c1-8307-e7f976d3843b
data: {"schemaVersion":1,"eventType":"tool.completed",...}
```

心跳为每 15 秒的注释。每个客户端有小型的有界缓冲区；慢客户端会被断开而非阻塞摄入。客户端重连并使用 REST 进行缺口恢复；MVP 不保证从 `Last-Event-ID` 进行 SSE 回放。
