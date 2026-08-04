# 监控插件设计

## 职责

插件负责：

1. 检测已支持的 OpenClaw 诊断和类型化钩子接口；
2. 将已验证的生命周期数据映射到协议 v1；
3. 哈希会话路由密钥，仅选择安全字段；
4. 在 OpenClaw 路径上执行 O(1) 入队；
5. 通过 Observatory Unix socket 异步批量投递；
6. 守护进程不可用时静默降级。

它不直接操作 SQLite、采样系统资源、聚合 Prometheus 指标、渲染 Dashboard、在磁盘上保留事件、执行长时间重试或同步等待守护进程。

该包还注册只读 `observatory_query` 工具并打包 `openclaw-observatory` Skill。该工具接受闭合的资源枚举，仅连接 `127.0.0.1:10086`，执行 GET 请求，拒绝超过 64 KiB 的响应。它是查询接口，不是事件采集的一部分。

## OpenClaw 2026.6.11 适配器

| Observatory 事件 | OpenClaw 来源 |
| --- | --- |
| Gateway 启动/停止 | 插件服务生命周期 |
| 会话启动/结束 | `session_start`、`session_end` 观察钩子 |
| Agent 启动/结束 | `run.started`、`run.completed` 诊断 |
| LLM 启动/结束/错误/重试 | `model.call.*`、`model.failover` 诊断 |
| 工具启动/结束/错误 | `tool.execution.*` 诊断 |
| MCP 启动/结束/错误 | 工具诊断且 `toolSource === "mcp"` |
| 子 Agent 启动/结束 | `subagent_spawned`、`subagent_ended` 钩子 |
| 心跳/丢弃 | 诊断心跳/队列丢弃事件 |

未知 SDK 事件名称被忽略，不伪造任何 API。

当显式配置 `plugins.entries.openclaw-observatory.hooks.allowConversationAccess=true` 时，适配器可从受支持的 `llm_output` Hook 复制规范化的数值用量字段（`input`、`output`、`cacheRead`、`cacheWrite`）。虽然该 Hook 事件也包含会话内容字段，但处理器只向转发器传递有界标识符和数值 usage，不会将提示词、响应或推理内容入队。usage 会按 run ID、稳定的会话密钥哈希或 session ID 关联到最后一次模型调用。

## 队列和失败策略

- 默认容量：10,000 个事件。
- 单事件上限：16 KiB；批次上限：1 MiB/100 个事件。
- 刷新间隔：健康时 250 ms；socket 探测失败时使用带抖动的指数退避，范围从 250 ms 到 30 秒。
- 每次重试前检查 socket 路径。常规投递使用可配置的 5 秒请求超时；关闭时刷新使用独立可配置的 250 ms 超时。
- 关键：崩溃和生命周期终止事件。
- 普通：Agent、LLM、工具、MCP、会话和子 Agent 生命周期。
- 低：心跳和高频中间事件。
- 队列满时，先移除最旧的低优先级事件再丢弃普通事件。关键生命周期事件会被保留，即使这会暂时超过队列容量。
- 心跳会同时上报队列深度和配置容量。深度达到 80% 时守护进程会输出告警，并将最近深度暴露给 Prometheus。
- 丢弃计数在容量恢复后合并到 `monitor.events_dropped`。

钩子处理程序从不等待传输。HTTP socket 回调由后台刷新器拥有。投递失败以限速的 debug/warn 级别记录，不能传播到 OpenClaw。

## 性能目标

- 同步处理程序 P95 低于 1 ms；
- 默认容量下插件自有内存低于 20 MB；
- 守护进程缺席不增加 Agent 延迟；
- 观察处理程序不返回用于传输工作的 Promise。

测试套件验证映射和队列策略；生产分析应在 10,000 事件突发下测量 P95 目标。
