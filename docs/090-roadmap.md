# Roadmap

## Phase 0 — Architecture and Contracts ✅

- ✅ Runtime and ownership model;
- ✅ Versioned event Schema and compatibility negotiation;
- ✅ Metrics and API contracts;
- ✅ Privacy threat model and cardinality policy.

Exit: valid Schema examples, linked documents, and reviewed compatibility map.

**Completion: 100%**

---

## Phase 1 — Local MVP ✅

- ✅ OpenClaw monitor plugin;
- ✅ Go daemon and Unix-socket ingestion;
- ✅ Idempotent SQLite projections;
- ✅ macOS resource sampling;
- ✅ `/metrics`, REST, SSE, built-in UI, and baseline Grafana dashboard;
- ✅ LaunchAgent installer and end-to-end smoke test.

Exit: a real OpenClaw run appears in SQLite/API without content capture and
daemon outage does not interrupt the run.

**Completion: 100%**

---

## Phase 2 — Product Dashboard ✅

### Completed

- ✅ Dark-themed real-time dashboard with KPI cards (Gateway status, sessions, runs, tokens)
- ✅ Resource monitoring charts (CPU%, memory RSS, threads, FDs, IO)
- ✅ LLM activity panel (requests by model, token usage, latency P50/P95/P99)
- ✅ Tool call statistics with success/failure breakdown
- ✅ Agent run timeline (Gantt-style)
- ✅ Real-time event stream + error browser with filters
- ✅ SSE live updates + 15s polling refresh
- ✅ Collapsible sections with localStorage persistence
- ✅ Time range selector (1h/6h/24h/7d) with localStorage
- ✅ Standalone web server with reverse proxy (`cmd/observatory-web`)
- ✅ Vite build pipeline with build ID injection
- ✅ Schema migration framework (v1 → v5, agent_id, aggregation indexes, disk capacity)
- ✅ Agent ID extraction from session key in forwarder
- ✅ API version 3 with compatibility banner
- ✅ Mobile responsive layout, pinch-zoom disabled
- ✅ Chinese documentation (README.zh.md + docs/zh/*)
- ✅ Cloudflare Tunnel deployment + Access protection

### Completed in v0.3

- ✅ Session waterfall for LLM, Tool, MCP, and Subagent activity
- ✅ Interactive historical charts driven by one time range
- ✅ Subagent and MCP API/UI exposure
- ✅ Dashboard JSON, drag ordering, thresholds, and themes

**Completion: 100%**

---

## Phase 3 — v0.3 Observability Enhancement ✅

> No backward compatibility constraints — API can break freely until first release.

### 3.1 Data Exposure

- ✅ `/agents/stats` — per-agent_id aggregation (runs, tokens, tools, duration, error rate)
- ✅ Session detail waterfall for LLM, Tool, MCP, and Subagent calls
- ✅ Expose `subagent_runs` and `mcp_calls` via API and UI

### 3.2 Time Series Aggregation

- ✅ `/timeseries` — time-bucket aggregation (1m/5m/1h/1d)
- ✅ Time range selector (1h/6h/24h/7d/30d) drives all charts
- ✅ Historical trend charts replace "latest N rows" mode
- ✅ Stacked area charts for resource and per-model token trends

### 3.3 Rich Chart Types

- ✅ Multi-metric combo charts for LLM volume, latency, and error rate
- ✅ Time × Agent activity heatmap
- ✅ Token and tool distribution doughnut charts
- ✅ Per-LLM-call latency-vs-token scatter
- ✅ Cross-agent runs/tokens/errors comparison
- ✅ LLM/Tool/MCP/Subagent session waterfall
- ✅ Error aggregation by `error_category`
- ✅ Cost analysis by model, agent, and time when providers report cost

### 3.4 Configuration & Customization

- ✅ Dashboard JSON config (module visibility, ordering, refresh interval)
- ✅ Drag-and-drop module reordering
- ✅ Configurable threshold alert highlighting
- ✅ Dark/light theme toggle

### 3.5 Frontend Engineering

- ✅ Migrate single HTML to Vite ES-module components
- ✅ Tree-shaken Chart.js bundle with DOM heatmap/waterfall specializations
- ✅ Responsive tablet/mobile layout
- ✅ 72 KiB gzipped JavaScript bundle with no external runtime CDN/dependency

**Constraints:** No compat burden. SQLite aggregation performance matters (leverage indexes + `strftime` bucketing). Frontend bundle < 200KB gzipped. Zero external runtime dependencies.

---

## Phase 4 — v0.4 Operational Hardening ✅

### 4.1 Data Retention

- ✅ Configurable retention policy (`--retention-events-days`, `--retention-samples-days`, `--retention-all-days`)
- ✅ Background cleaner runs every 6 hours, purges old events/resource_samples
- ✅ WAL mode + `PRAGMA synchronous=NORMAL` confirmed

### 4.2 Frontend Incremental Updates

- ✅ SSE-driven incremental chart updates (no full re-render/destroyCharts flicker)
- ✅ `updateChartData`/`updateDoughnut`/`updateScatter` for in-place Chart.js updates
- ✅ KPI cards and tables patched in-place via `updateNonChartDOM`

### 4.3 CI/CD

- ✅ GitHub Actions: Go test + vet with race detector, Vite build verification
- ✅ Concurrency group cancellation for rapid iteration

### 4.4 Cursor Pagination

- ✅ Cursor-based pagination for sessions, runs, events endpoints
- ✅ Opaque base64-encoded cursor using composite key (timestamp + id)
- ✅ `LIMIT+1` fetch strategy for has-more detection

### 4.5 URL State Sync

- ✅ Time range, instance filter, agent filter, and session written to URL query params
- ✅ Browser back/forward via popstate listener
- ✅ Shareable dashboard URLs

### 4.6 Cost Analysis Enhancement

- ✅ `/api/v1/cost/trends` — daily/weekly/monthly cost breakdown by model
- ✅ `/api/v1/cost/summary` — aggregate with day/week/month rolls
- ✅ Budget alert UI with configurable `costBudgetUsd` threshold
- ✅ Stacked bar chart for cost trends per model

---

## Phase 5 — v0.5 Stability & Resilience 🚧

> Focus: prevent data loss during daemon/plugin restart, recover gracefully from
> network hiccups, and harden against edge-case crashes.

### 5.1 Plugin → Daemon 断线重连

**现状问题：**
- Forwarder 通过 Unix socket 连接 daemon，flush 失败后用指数退避重试（250ms → 5s），但没有重连逻辑——socket 路径不变，退避后再次 POST 即可
- `post()` 超时硬编码 250ms（force）和 1500ms，daemon 高负载时可能误判
- daemon 崩溃期间队列满后直接丢事件（`queue_full`），critical 事件也可能被丢弃

**改进计划：**
- [ ] Forwarder flush 前检测 socket 连通性（`fs.access` 或 `connect` 探针），避免无意义 POST
- [ ] 指数退避上限提高到 30s，加入抖动（jitter），daemon 恢复后快速回连
- [ ] critical 事件保护：队列满时，丢弃 lowest-priority 事件前先尝试再降级，确保 `gateway.started/stopped`、`session.completed` 等关键事件不丢
- [ ] 队列深度通过 heartbeat 事件上报 daemon，daemon 端根据 queue_depth 做 backpressure 日志告警
- [ ] 添加 `--queue-capacity` 命令行参数，允许调大队列上限

### 5.2 Daemon 崩溃防护与恢复

**现状问题：**
- daemon 崩溃后 LaunchAgent 会重启，但没有崩溃诊断和 self-healing
- SQLite WAL mode 虽然安全，但 daemon 异常退出可能留下 stale lock
- `main.go` 中 `errCh` 收到 HTTP server 错误后直接退出，没有尝试 recovery
- resource sampling 中 `ps`/`lsof` 子进程失败（如权限问题）静默忽略，无法区分是 daemon 问题还是 OS 问题

**改进计划：**
- [ ] 启动时检测并清理 SQLite stale lock（`.db-wal`、`.db-shm`、lock file）
- [ ] HTTP server fatal error 加入 recovery 重试（bind 失败除外）
- [ ] SIGBUS / SIGSEGV handler：写入 crash dump 文件到 `logs/`，包含 goroutine stack traces
- [ ] 健康检查增强：`/ready` 端点检测 SQLite 可写性和 recent event latency
- [ ] 进程采样加入 error counter 和 alert threshold（连续 N 次采样失败 → 标记 gateway.crashed）

### 5.3 数据写入健壮性

**现状问题：**
- `InsertEvents` 在单个事务中做 insert + reduce，事件量大时事务持有时间长
- `PRAGMA busy_timeout=5000` 只有 5s，多个查询并发时可能 SQLITE_BUSY
- `SetMaxOpenConns(1)` 是必要的（SQLite 单写），但没有连接池健康检查
- retention purge 在事务内逐行 DELETE，大批量时性能差
- 没有事务日志或写入审计

**改进计划：**
- [ ] `busy_timeout` 提高到 30s，匹配长查询场景
- [ ] 批量写入拆分：单次 batch 超过 50 个事件时拆成多个小事务（减少锁持有时间）
- [ ] Retention DELETE 改用 `WHERE rowid IN (SELECT rowid FROM ... ORDER BY ... LIMIT 1000)` 分批删除
- [ ] 定期 VACUUM（retention job 完成后触发，低峰期）
- [ ] 写入性能指标：`INSERT OR IGNORE` 耗时、reduce 耗时、事务提交耗时，输出到 `/metrics`
- [ ] 添加写入审计日志（可选，`--audit-log` flag），记录每次 batch 的 accepted/duplicates/errors

### 5.4 前端断线重连与错误恢复

**现状问题：**
- SSE `onerror` 简单 close + 5s 重连，没有区分网络错误、服务端 503、正常关闭
- `loadDashboard` 使用裸 `fetch()`，没有超时、重试、AbortController
- daemon 不可用时页面显示空白或错误信息，没有友好的 offline 状态
- 背景自动刷新失败后没有退避策略，一直按固定间隔重试

**改进计划：**
- [ ] SSE 重连加入指数退避（1s → 2s → 4s → ... → 30s），连接成功后重置
- [ ] SSE `onerror` 区分 `readyState`：CLOSED = 重连，CONNECTING = 等待
- [ ] `fetch()` 加入 AbortController + 超时（10s），超时后重试一次
- [ ] 添加 offline banner：daemon 不可达时显示 "正在重连..." 状态条，恢复后自动消失
- [ ] 背景刷新加入退避：连续失败 3 次后间隔翻倍，上限 60s
- [ ] 前端 `navigator.onLine` 监听，离线时暂停刷新，上线后立即触发一次
- [ ] 数据缓存：上一次成功的 dashboard 数据在 fetch 失败时继续展示（标注 "数据可能过期"）

### 5.5 性能优化

**现状问题：**
- `/agents/stats` 使用 3 个 CTE + 多个 JOIN，30d 范围数据量大时可能慢
- `/timeseries` 对每个 bucket 做聚合，2000 个 bucket 上限意味着最多 2000 次 `strftime`
- `agentStats` 查询中的 `tool_events` 是 UNION ALL，没有 filter 条件下扫全表
- Resource sampling 每 5s 调用 `ps` + `lsof` 两个子进程，macOS 上 `lsof` 较慢
- 前端一次 dashboard 请求发 12 个并行 fetch，每个都有完整 JSON 序列化开销

**改进计划：**
- [ ] `timeseries` 查询优化：预计算 bucket 边界，用 `CASE WHEN` 替代 `strftime`
- [ ] `agent_stats` 加入时间范围下推到 CTE（避免 JOIN 后再 filter）
- [ ] Resource sampling `lsof` 替代方案：macOS 用 `proc_info` syscall 或 `/proc/self/fd`（Linux）
- [ ] Long-running 查询加 `statement` timeout（SQLite `busy_timeout` 不覆盖）
- [ ] 前端：合并部分 API 为 composite endpoint（如 `/api/v1/dashboard` 一次返回所有 KPI + chart data）
- [ ] SQLite 查询计划分析：`EXPLAIN QUERY PLAN` 对关键查询做 baseline

### 5.6 监控与告警

**现状问题：**
- `/metrics` 暴露 Prometheus 格式但没有内置告警
- 没有对 daemon 自身的健康指标（事件延迟、队列积压、查询延迟）
- 日志只有 `slog` 到 stderr，没有结构化日志文件轮转

**改进计划：**
- [ ] Prometheus 指标新增：`openclaw_monitor_insert_duration_seconds`（写入延迟）、`openclaw_monitor_queue_depth`（队列积压）、`openclaw_monitor_query_duration_seconds`（查询延迟）
- [ ] 日志轮转：`slog` 写入 `logs/observatoryd.log`，按天轮转，保留 7 天
- [ ] 内置 alert rule：event queue > 80% capacity → WARN，insert latency > 1s → WARN
- [ ] `status` API 返回 `eventQueueDepth`、`lastEventReceivedAt`、`dbSizeBytes`

**Exit Criteria：**
- Daemon 崩溃重启后 0 事件丢失（critical events）
- 前端断网恢复后 5s 内自动重连并展示数据
- 30d 范围 agents/stats 查询 < 500ms
- 数据库写入指标暴露到 `/metrics`

---

## Phase 6 — Advanced Observability

- Metadata-only session replay;
- OpenTelemetry trace export and correlation;
- Loki/Tempo integration;
- Authenticated remote mode;
- Multi-instance discovery and federation;
- Opt-in encrypted content capture only after a separate security design.

Exit: remote/multi-instance operation has authentication, authorization,
encryption, audit logging, and documented upgrade/rollback behavior.

---

## OpenClaw interfaces still requiring verification

- Behavior on versions older/newer than `2026.6.11`;
- Whether all external harnesses emit canonical `tool.execution.*` events;
- Stable MCP owner/server identity beyond authoritative `toolSource: "mcp"`;
- Exact terminal behavior for hard process kill and host sleep/wake;
- Provider-specific cost/token completeness.
