# openclaw-observatory

你正在一个新建的 GitHub 仓库 openclaw-observatory 中工作。

请完成该仓库的首版架构设计和工程骨架。

项目目标：

构建一个专门用于监控 OpenClaw 的本地可观测性平台。

整体架构：

OpenClaw
→ Monitor Plugin
→ Local Daemon
→ REST API / SSE / Prometheus Metrics
→ 专属 Dashboard / Prometheus / Grafana

核心原则：

1. OpenClaw 插件只负责采集运行时语义事件。
2. Daemon 负责事件接收、资源采集、聚合、存储和查询。
3. Prometheus 只承载低基数数值指标。
4. Session、Agent Run、Tool Call 等详细数据通过 REST API 查询。
5. 插件故障不能影响 OpenClaw 正常运行。
6. 默认不采集完整 Prompt、Tool 参数和 Tool 输出。
7. 首版优先支持 macOS，架构需允许后续支持 Linux。
8. 当前阶段主要完成设计文档、协议定义和部署骨架，不需要实现完整业务代码。

请创建以下目录和文件：

README.md
LICENSE
.gitignore
docs/
├── 000-overview.md
├── 010-runtime-model.md
├── 020-event-model.md
├── 030-plugin-design.md
├── 040-daemon-design.md
├── 050-storage-design.md
├── 060-prometheus-metrics.md
├── 070-api-design.md
├── 080-grafana-dashboard.md
└── 090-roadmap.md
schemas/
└── monitor-event.schema.json
deploy/
├── docker-compose.yml
├── prometheus/
│   ├── prometheus.yml
│   └── rules/
│       └── openclaw-alerts.yml
└── grafana/
    ├── provisioning/
    │   ├── datasources/
    │   │   └── prometheus.yaml
    │   └── dashboards/
    │       └── dashboards.yaml
    └── dashboards/
        └── openclaw-overview.json

README.md

README 至少包含：

* 项目简介
* 项目目标
* 当前状态
* 核心架构图
* 主要组件
* 快速开始占位说明
* 文档导航
* Roadmap
* 隐私与安全原则

项目暂定名称：

OpenClaw Observatory

一句话描述：

A local-first observability platform for OpenClaw runtime, resource usage, agent execution, LLM calls, and tool activity.

README 使用英文为主，必要时可以附中文说明。

Runtime Model

在 docs/010-runtime-model.md 中定义以下核心对象：

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

为每个对象说明：

* 定义
* 唯一标识
* 生命周期
* 父子关系
* 主要属性
* 是否适合作为 Prometheus Label
* 是否需要持久化

明确区分：

* Instance
* Session
* Agent Run
* LLM Call
* Tool Call
* Resource Sample

Event Model

在 docs/020-event-model.md 中采用事件驱动模型。

定义通用事件 Envelope：

interface MonitorEvent<TPayload> {
  schemaVersion: 1;
  eventId: string;
  eventType: string;
  occurredAt: string;
  monotonicTimeNs?: number;
  instanceId: string;
  processId?: number;
  sequence: number;
  source: "plugin" | "daemon";
  payload: TPayload;
}

首版事件至少包括：

gateway.started
gateway.heartbeat
gateway.stopped
gateway.crashed
session.started
session.completed
session.failed
agent.started
agent.completed
agent.failed
llm.started
llm.completed
llm.failed
llm.retried
tool.started
tool.completed
tool.failed
mcp.started
mcp.completed
mcp.failed
subagent.started
subagent.completed
subagent.failed
resource.sampled
monitor.events_dropped
monitor.plugin_error

要求说明：

* started 和 completed 必须分开发送
* 事件必须可排序
* Daemon 必须处理重复事件
* 允许事件乱序到达
* 允许 OpenClaw 崩溃后存在未结束事件
* 不得在事件中默认包含完整 Prompt、Shell 命令、文件内容或 Tool 输出

JSON Schema

在 schemas/monitor-event.schema.json 中创建 Draft 2020-12 JSON Schema。

要求：

* 校验通用 Envelope
* 校验 schemaVersion
* 校验时间格式
* 限制 eventType
* 保留 payload 扩展能力
* 设置合理的字符串长度上限
* 禁止未受控的超大数据
* 添加至少三个合法示例：
    * agent.started
    * tool.completed
    * resource.sampled

Plugin Design

在 docs/030-plugin-design.md 中设计 OpenClaw 插件。

插件职责：

* 监听可用生命周期 Hook
* 转换成统一事件
* 做最小化脱敏
* 写入内存队列
* 异步发送给 Daemon
* 在 Daemon 不可用时降级

插件不负责：

* SQLite
* Prometheus 聚合
* 系统资源采集
* Dashboard
* 长期重试
* 阻塞等待 Daemon

性能目标：

同步 Hook 处理 P95 < 1 ms
默认队列上限 10,000 条
单事件默认上限 16 KB
插件额外内存目标 < 20 MB
Daemon 不可用时不得阻塞 OpenClaw

定义事件优先级：

* Critical：崩溃、生命周期结束
* Normal：Agent、LLM、Tool 生命周期
* Low：高频中间事件和心跳

队列满时优先丢弃 Low 事件，并产生 monitor.events_dropped。

Daemon Design

在 docs/040-daemon-design.md 中设计独立 Daemon。

推荐语言：

* Go 作为首选
* 说明选择理由
* 保持接口与语言无关

Daemon 模块：

Event Receiver
Event Validator
Event Normalizer
Runtime State Reducer
Process Discovery
Resource Collector
Metrics Aggregator
SQLite Repository
REST API
SSE Stream
Prometheus Exporter
Retention Worker

本地 IPC 首选 Unix Domain Socket：

~/.openclaw-observatory/observatory.sock

同时预留 localhost HTTP 调试入口。

Daemon 必须：

* 独立于 OpenClaw 进程运行
* 根据 PID 采集资源
* 记录 OpenClaw 崩溃前状态
* 支持幂等事件处理
* 支持优雅关闭
* 支持数据保留策略
* 暴露健康检查接口

Storage Design

在 docs/050-storage-design.md 中设计 SQLite 数据模型。

至少包括：

instances
sessions
agent_runs
llm_calls
tool_calls
mcp_calls
subagent_runs
resource_samples
events
daily_stats
schema_migrations

要求：

* 给出主要字段
* 给出主键和外键
* 给出索引建议
* 说明事件表与聚合表的关系
* 说明崩溃恢复方式
* 说明幂等写入方式
* 说明数据库迁移策略

资源数据保留建议：

最近 1 小时：5 秒粒度
最近 24 小时：1 分钟粒度
最近 30 天：15 分钟粒度
30 天以上：按天聚合或删除

Prometheus Metrics

在 docs/060-prometheus-metrics.md 中定义第一版指标。

至少包括：

openclaw_gateway_up
openclaw_gateway_uptime_seconds
openclaw_gateway_restarts_total
openclaw_sessions_active
openclaw_agent_runs_active
openclaw_agent_runs_total
openclaw_agent_run_duration_seconds
openclaw_llm_requests_total
openclaw_llm_tokens_total
openclaw_llm_request_duration_seconds
openclaw_llm_errors_total
openclaw_llm_cost_usd_total
openclaw_tool_calls_total
openclaw_tool_call_duration_seconds
openclaw_tool_errors_total
openclaw_process_cpu_seconds_total
openclaw_process_resident_memory_bytes
openclaw_process_virtual_memory_bytes
openclaw_process_threads
openclaw_process_open_fds
openclaw_process_read_bytes_total
openclaw_process_write_bytes_total
openclaw_monitor_events_received_total
openclaw_monitor_events_dropped_total
openclaw_monitor_event_queue_depth

遵循 Prometheus 命名规范：

* 时间使用秒
* 容量使用字节
* 比例使用 0 到 1
* Counter 使用 _total
* Histogram 使用 _bucket、_sum、_count

允许的低基数标签：

instance
provider
model
status
tool
event_type
reason

禁止作为标签：

session_id
run_id
request_id
user_id
prompt
shell_command
file_path
error_message

为主要指标附上示例 PromQL。

REST API

在 docs/070-api-design.md 中设计 API：

GET /health
GET /ready
GET /metrics
GET /api/v1/status
GET /api/v1/instances
GET /api/v1/sessions
GET /api/v1/sessions/{id}
GET /api/v1/runs
GET /api/v1/runs/{id}
GET /api/v1/resources
GET /api/v1/tools/stats
GET /api/v1/models/stats
GET /api/v1/events
GET /api/v1/stream

要求：

* 使用 JSON
* 统一错误结构
* 支持分页
* 支持时间范围筛选
* 支持 instanceId 筛选
* SSE 用于单向实时事件
* API 默认只监听 localhost
* 提供响应示例

Grafana Dashboard

在 docs/080-grafana-dashboard.md 中定义 Dashboard 布局：

第一行：

* Gateway 状态
* Uptime
* Active Sessions
* Active Agent Runs
* 今日 Token
* 今日 Cost

第二行：

* CPU
* RSS
* Virtual Memory
* Threads
* File Descriptors
* IO

第三行：

* LLM Requests
* Input / Output Tokens
* P50 / P95 / P99 Latency
* Error Rate
* Provider 分布
* Model 分布

第四行：

* Agent Run 数量
* Agent Run 耗时
* Tool Calls
* Tool Errors
* Slowest Tools
* Queue Depth

第五行：

* Gateway Restarts
* Dropped Events
* Monitor Queue Depth
* Recent Alerts

创建一个最小可导入的 Grafana Dashboard JSON。无需完整美化，但必须合法且包含基础面板。

Deployment

创建 deploy/docker-compose.yml，包含：

* Prometheus
* Grafana

Daemon 暂时可以作为注释或占位服务，因为首版还没有二进制镜像。

Prometheus 默认抓取：

host.docker.internal:17654

Grafana：

* 自动配置 Prometheus 数据源
* 自动加载 OpenClaw Dashboard
* 默认端口 3000

Prometheus：

* 默认端口 9090
* 加载告警规则

至少创建以下告警：

OpenClawGatewayDown
OpenClawHighMemory
OpenClawHighLLMErrorRate
OpenClawToolErrorSpike
OpenClawMonitorDroppingEvents

告警阈值应说明只是初始默认值，需要按环境调整。

Roadmap

在 docs/090-roadmap.md 中拆成四阶段：

Phase 0：Architecture and Contracts

* Runtime Model
* Event Schema
* Metrics Contract
* API Contract

Phase 1：Local MVP

* OpenClaw Plugin
* Go Daemon
* SQLite
* /metrics
* 基础 Grafana Dashboard

Phase 2：Product Dashboard

* Web UI
* Live Timeline
* Session Detail
* Resource Charts
* Error Explorer

Phase 3：Advanced Observability

* Session Replay
* OpenTelemetry
* Trace
* Loki / Tempo
* Remote Mode
* Multi-instance Support

设计要求

* 文档内容必须具体，不能只写标题和 TODO
* 对尚未确认的 OpenClaw Hook 使用“能力探测”和“适配层”设计，不要假设所有版本接口完全一致
* 所有协议都必须带版本号
* 所有时间统一使用 UTC RFC3339
* 本地耗时优先使用 monotonic clock
* 数据采集必须默认脱敏
* Daemon 与插件之间必须支持版本协商
* 不要伪造已经存在的 OpenClaw API
* 不确定的 OpenClaw 接口请明确标记为待验证
* 使用 Mermaid 绘制架构图和主要时序图
* Markdown 文档要求结构清晰
* JSON、YAML 和 JSON Schema 必须语法有效

Git 操作

完成后：

1. 检查目录结构。
2. 校验 JSON 文件。
3. 校验 YAML 文件。
4. 检查 Markdown 链接。
5. 确认没有提交密钥、Token 或隐私数据。
6. 将全部内容提交到默认分支。
7. Commit message 使用：

docs: initialize OpenClaw Observatory architecture

最后输出：

* 创建的文件列表
* 核心架构决策摘要
* 尚待验证的 OpenClaw 接口
* 下一步建议实现的第一个模块