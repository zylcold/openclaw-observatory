# 事件模型

## 信封 v1

```typescript
interface MonitorEvent<TPayload> {
  schemaVersion: 1;
  eventId: string;                 // UUID
  eventType: string;               // 闭合 v1 枚举
  occurredAt: string;              // UTC RFC3339
  monotonicTimeNs?: number;        // 仅生产者本地
  instanceId: string;
  producerId: string;              // 一次插件/守护进程启动
  processId?: number;
  sequence: number;                // 每个 producerId 单调递增
  source: "plugin" | "daemon";
  payload: Record<string, unknown>;
}
```

`occurredAt` 是跨进程排序基线。`sequence` 仅在一个生产者内打破平局和检测缺口。单调值绝不能在进程或启动之间比较。

## 事件类型

Gateway：`gateway.started`、`gateway.heartbeat`、`gateway.stopped`、`gateway.crashed`。

运行时：`session.started|completed|failed`、`agent.started|completed|failed`、`llm.started|completed|failed|retried`、`tool.started|completed|failed`、`mcp.started|completed|failed`、`subagent.started|completed|failed`。

守护进程/监控：`resource.sampled`、`monitor.events_dropped`、`monitor.plugin_error`。

启动和终止事件是独立的事实。终止事件从不修改或替换启动事件。

## 顺序、重复和不完整操作

- 守护进程插入 `eventId` 时带唯一约束。重复返回成功确认但不重跑归约器。
- 事件可能乱序到达。Upsert 使用最早的开始和最新的终止时间戳，同时保留原始事件。
- 实体可由终止事件创建（如果对应的启动事件丢失）。
- 守护进程启动时，不会立即将活跃行标记为失败。Gateway 活跃性协调决定它们是否仍可能存活或标记为不完整/崩溃。
- 事件时间戳回退会被保留但标记；在一次启动内优先使用生产者序列顺序。

## 版本协商

`gateway.started` 包含 `protocolVersions: [1]`、插件/OpenClaw 版本和检测到的能力名称。守护进程以 HTTP `422` 拒绝不支持的 Schema 版本。未来插件必须在发送非启动事件前选择双方支持的最高版本。

## 隐私和限制

v1 白名单没有 Prompt、响应、工具参数/结果、Shell 命令、文件内容/路径、原始聊天身份或自由格式错误文本的字段。错误值是受控的类别/代码。每个序列化事件在插件和守护进程端限制为 16 KiB；每批次限制为 1 MiB。JSON Schema 增加了字符串/数组/属性限制，接收端还强制执行 Schema 无法表达的字节大小和解码限制。

详见 [`../schemas/monitor-event.schema.json`](../../schemas/monitor-event.schema.json) 获取机器可读的契约和示例。
