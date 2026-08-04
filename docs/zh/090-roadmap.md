# 路线图

## 阶段 0 — 架构和契约 ✅

- ✅ 运行时和所有权模型；
- ✅ 版本化事件 Schema 和兼容性协商；
- ✅ 指标和 API 契约；
- ✅ 隐私威胁模型和基数策略。

退出条件：有效的 Schema 示例、关联文档和已审查的兼容性映射。

**完成度：100%**

---

## 阶段 1 — 本地 MVP ✅

- ✅ OpenClaw 监控插件；
- ✅ Go 守护进程和 Unix socket 摄入；
- ✅ 幂等 SQLite 投影；
- ✅ macOS 资源采样；
- ✅ `/metrics`、REST、SSE、内置 UI 和基线 Grafana Dashboard；
- ✅ LaunchAgent 安装脚本和端到端冒烟测试。

退出条件：一次真实的 OpenClaw 运行出现在 SQLite/API 中且无内容捕获，守护进程中断不中断运行。

**完成度：100%**

---

## 阶段 2 — 产品 Dashboard ✅

### 已完成

- ✅ 暗色主题实时面板，KPI 卡片（Gateway 状态、会话、运行、Token）
- ✅ 资源监控图表（CPU%、内存 RSS、线程、FD、IO）
- ✅ LLM 活动面板（按模型请求量、Token 用量、延迟 P50/P95/P99）
- ✅ 工具调用统计（成功/失败分解）
- ✅ Agent 运行时间线（甘特图）
- ✅ 实时事件流 + 错误浏览器（带过滤）
- ✅ SSE 实时更新 + 15 秒轮询刷新
- ✅ 模块折叠/展开，localStorage 持久化
- ✅ 时间范围选择器（1h/6h/24h/7d），localStorage 记忆
- ✅ 独立 Web Server + 反向代理（`cmd/observatory-web`）
- ✅ Vite 构建流水线，Build ID 注入
- ✅ Schema 迁移框架（v1 → v5，agent_id、聚合索引、磁盘容量）
- ✅ Forwarder 从 sessionKey 提取 agentId
- ✅ API version 3 + 兼容性检测横幅
- ✅ 移动端响应式布局，禁用缩放
- ✅ 中文文档（README.zh.md + docs/zh/*）
- ✅ Cloudflare Tunnel 部署 + Access 保护

### v0.3 完成项

- ✅ LLM、Tool、MCP 与 Subagent 会话瀑布图
- ✅ 同一时间范围驱动交互式历史图表
- ✅ Subagent 和 MCP API/UI 暴露
- ✅ Dashboard JSON、拖拽排序、阈值与主题

**完成度：100%**

---

## 阶段 3 — v0.3 可观测性增强 ✅

> 无向后兼容约束 — 首次发布前 API 可自由 breaking change。

### 3.1 数据暴露

- ✅ `/agents/stats` — 按 agent_id 聚合（runs、tokens、tools、duration、error rate）
- ✅ LLM、Tool、MCP 与 Subagent 会话详情瀑布图
- ✅ 暴露 `subagent_runs` 和 `mcp_calls`（API + UI）

### 3.2 时间序列聚合

- ✅ `/timeseries` — 时间桶聚合（1m/5m/1h/1d）
- ✅ 时间范围选择器（1h/6h/24h/7d/30d）驱动所有图表
- ✅ 历史趋势图替换"最近 N 条"模式
- ✅ 资源与各模型 Token 堆叠面积趋势

### 3.3 丰富图表类型

- ✅ LLM 请求量、延迟与错误率多指标组合图
- ✅ 时间×Agent 活跃度热力图
- ✅ Token、工具调用环形图
- ✅ 单次 LLM 调用延迟-Token 散点图
- ✅ 多 Agent runs/tokens/errors 横向对比
- ✅ LLM/Tool/MCP/Subagent 会话瀑布图
- ✅ 按 `error_category` 聚合错误
- ✅ 按模型、Agent、时间分析成本（Provider 上报时）

### 3.4 配置与自定义

- ✅ Dashboard JSON 配置（模块显隐、排序、刷新间隔）
- ✅ 拖拽排序
- ✅ 可配置阈值告警高亮
- ✅ 暗色/亮色主题切换

### 3.5 前端工程化

- ✅ 单 HTML 迁移为 Vite ES Module 组件
- ✅ 按需打包 Chart.js，热力图/瀑布图使用轻量 DOM 实现
- ✅ 平板/手机响应式布局
- ✅ JavaScript gzip 约 72 KiB，无外部 CDN/运行时依赖

**约束：** 无兼容包袱。SQLite 聚合注意性能（利用索引 + `strftime` 桶化）。前端 bundle < 200KB gzipped。零外部运行时依赖。

---

## 阶段 4 — v0.4 运维加固 ✅

### 4.1 数据保留

- ✅ 可配置保留策略（`--retention-events-days`、`--retention-samples-days`、`--retention-all-days`）
- ✅ 后台清理任务每 6 小时执行，清除过期事件/resource_samples
- ✅ WAL 模式 + `PRAGMA synchronous=NORMAL` 已确认

### 4.2 前端增量更新

- ✅ SSE 驱动的增量图表更新（无全量 re-render/destroyCharts 闪烁）
- ✅ `updateChartData`/`updateDoughnut`/`updateScatter` 原地更新 Chart.js
- ✅ KPI 卡片和表格通过 `updateNonChartDOM` 原地补丁

### 4.3 CI/CD

- ✅ GitHub Actions：Go test + vet（含 race detector）、Vite build 验证
- ✅ Concurrency group 取消机制，支持快速迭代

### 4.4 游标分页

- ✅ sessions、runs、events 端点支持游标分页
- ✅ 不透明 base64 编码游标，使用复合键（时间戳 + ID）
- ✅ `LIMIT+1` 策略检测是否有更多数据

### 4.5 URL 状态同步

- ✅ 时间范围、实例过滤、Agent 过滤、会话 ID 写入 URL 查询参数
- ✅ 浏览器前进/后退通过 popstate 监听
- ✅ 可分享的 Dashboard URL

### 4.6 成本分析增强

- ✅ `/api/v1/cost/trends` — 按日/周/月维度拆分各模型成本
- ✅ `/api/v1/cost/summary` — 聚合成本，含 day/week/month 滚动
- ✅ 预算告警 UI，可配置 `costBudgetUsd` 阈值
- ✅ 成本趋势堆叠柱状图（按模型）

---

## 阶段 5 — v0.5 稳定性与韧性 🚧

> 重点：防止 daemon/plugin 重启期间的数据丢失，从网络中断中优雅恢复，
> 针对边缘场景崩溃进行加固。

### 5.1 Plugin → Daemon 断线重连

**现状缺陷：**
- Forwarder 通过 Unix socket 连接 daemon，flush 失败后用指数退避重试（250ms → 5s），但没有连接健康检查——退避后直接盲目 POST
- `post()` 超时硬编码为 250ms（force）和 1500ms，daemon 高负载时可能误判失败
- daemon 崩溃期间队列满后直接丢事件（`queue_full`），critical 事件（`gateway.started/stopped`、`session.completed`）也可能被丢弃

**改进计划：**
- [x] flush 前探测 socket 连通性（`fs.access` 或 `connect` 检查），避免无意义 POST
- [x] 指数退避上限提高到 30s 并加入抖动（jitter），daemon 恢复后快速回连
- [x] critical 事件保护：队列满时优先丢弃最低优先级事件，确保 `gateway.started/stopped`、`session.completed` 等关键事件不丢失
- [x] 队列深度通过 heartbeat 事件上报 daemon，daemon 根据 `queueDepth` 记录 backpressure 告警日志
- [x] 添加可配置的 `queueCapacity` 插件选项，允许调大队列上限

### 5.2 Daemon 崩溃防护与恢复

**现状缺陷：**
- daemon 崩溃后依赖 LaunchAgent 重启，但没有崩溃诊断和自愈机制
- SQLite WAL 模式虽然安全，但异常退出可能留下 stale `-wal`/`-shm` 旁车文件
- `main.go` 中 `errCh` 收到 HTTP server 错误后直接退出，没有尝试 recovery
- 资源采样中 `ps`/`lsof` 子进程失败（如权限问题）被静默忽略，无法区分是 daemon 问题还是 OS 问题

**改进计划：**
- [ ] 启动时检测并清理 SQLite stale lock 文件（`.db-wal`、`.db-shm`）
- [x] HTTP server 致命错误恢复：瞬态错误自动重试（bind 冲突除外）
- [x] 运行时 crash 输出：将未捕获 panic/致命错误的 goroutine 堆栈写入 `logs/`
- [x] 健康检查增强：`/ready` 端点验证 SQLite 可写性和最近事件延迟
- [x] 进程采样错误追踪：连续失败计数器，超过 N 次后标记 `gateway.crashed`

### 5.3 数据写入健壮性

**现状缺陷：**
- `InsertEvents` 在单个事务中执行 insert + reduce，大批量时事务持有锁时间过长
- `PRAGMA busy_timeout=5000` 仅 5s，并发查询可能触发 SQLITE_BUSY
- `SetMaxOpenConns(1)` 是 SQLite 单写模式的必要设置，但缺少连接池健康检查
- retention 清理在事务内逐行 DELETE，大批量时性能差
- 没有写入审计

**改进计划：**
- [x] `busy_timeout` 提高到 30s，适应长查询场景
- [x] 批量写入拆分：单次 batch 超过 50 个事件时拆分为多个小事务（减少锁持有时间）
- [x] Retention DELETE 改用分批删除（`WHERE rowid IN (SELECT rowid FROM ... LIMIT 1000)`），避免全表扫描
- [ ] 定期 VACUUM（retention job 完成后触发，低峰期节流执行）
- [x] 写入性能指标：暴露 `INSERT OR IGNORE` 耗时、reduce 耗时、事务提交耗时到 `/metrics`
- [ ] 可选写入审计日志（`--audit-log` flag），记录每批的 accepted/duplicates/errors

### 5.4 前端断线重连与错误恢复

**现状缺陷：**
- SSE `onerror` 简单关闭 + 5s 后重连，没有区分网络错误、服务端 503、正常关闭
- `loadDashboard` 使用裸 `fetch()`，没有超时、重试、AbortController
- daemon 不可用时页面显示空白或错误信息，没有友好的离线状态
- 背景自动刷新失败后没有退避策略，一直按固定间隔重试

**改进计划：**
- [x] SSE 重连加入指数退避（1s → 2s → 4s → … → 30s），连接成功后重置
- [x] SSE `onerror` 区分 `readyState`：CLOSED = 重连，CONNECTING = 等待
- [x] `fetch()` 加入 AbortController + 10s 超时，超时后重试一次
- [x] 离线横幅：daemon 不可达时显示"正在重连…"状态条，恢复后自动消失
- [x] 背景刷新退避：连续失败 3 次后间隔翻倍，上限 60s
- [x] 监听 `navigator.onLine`：离线时暂停刷新，上线后立即触发一次
- [x] 数据缓存：fetch 失败时继续展示上一次成功的 dashboard 数据（标注"数据可能已过期"）

### 5.5 性能优化

**现状缺陷：**
- `/agents/stats` 使用 3 个 CTE + 多个 JOIN，30d 范围大数据量时可能较慢
- `/timeseries` 对每个桶做 `strftime` 聚合，上限 2000 个桶意味着最多 2000 次 `strftime` 调用
- `agentStats` 查询中的 `tool_events` CTE 是无过滤条件的 UNION ALL，有全表扫描风险
- 资源采样每 5s 调用 `ps` + `lsof` 两个子进程，macOS 上 `lsof` 较慢
- 前端一次 dashboard 加载发送 12 个并行 fetch 请求

**改进计划：**
- [ ] `timeseries` 查询优化：预计算桶边界，用 `CASE WHEN` 替代 `strftime`
- [x] `agent_stats`：将时间范围过滤下推到 CTE 内部，减少 JOIN 中间行数
- [ ] 替换 `lsof` 为更快的 FD 统计方式（macOS：`proc_info` syscall；Linux：`/proc/<pid>/fd` readdir）
- [x] 为长查询添加 statement timeout（SQLite `busy_timeout` 不覆盖此场景）
- [x] 前端：合并 Dashboard API 为 composite `/api/v1/dashboard` 端点（单次请求返回 KPI + 图表数据）
- [x] SQLite 查询计划分析：对关键查询运行 `EXPLAIN QUERY PLAN` 建立基线

### 5.6 监控与告警

**现状缺陷：**
- `/metrics` 暴露 Prometheus 格式但无内置告警规则
- 缺少 daemon 自身健康指标（事件延迟、队列积压、查询延迟）
- 日志仅 `slog` 输出到 stderr，无结构化日志文件轮转

**改进计划：**
- [x] 新增 Prometheus 指标：`openclaw_monitor_insert_duration_seconds`、`openclaw_monitor_queue_depth`、`openclaw_monitor_query_duration_seconds`
- [x] 日志轮转：`slog` 输出写入 `logs/observatoryd-YYYY-MM-DD.log`，按天轮转，保留 7 天
- [x] 内置告警阈值：事件队列 > 80% 容量 → WARN，写入延迟 > 1s → WARN
- [x] `status` API 返回 `eventQueueDepth`、`lastEventReceivedAt`、`dbSizeBytes`

**退出条件：**
- daemon 崩溃重启后 critical 事件零丢失
- 前端断网恢复后 5s 内自动重连并展示数据
- 30d 范围 `agents/stats` 查询 < 500ms
- 数据库写入指标暴露到 `/metrics`

---

## 阶段 6 — 高级可观测性

- 仅元数据的会话回放；
- OpenTelemetry 追踪导出和关联；
- Loki/Tempo 集成；
- 经认证的远程模式；
- 多实例发现和联邦；
- 可选的加密内容捕获仅在独立安全设计之后。

退出条件：远程/多实例运维具有认证、授权、加密、审计日志和文档化的升级/回滚行为。

---

## 仍需验证的 OpenClaw 接口

- 在 `2026.6.11` 之前/之后版本上的行为；
- 是否所有外部测试框架都发出规范的 `tool.execution.*` 事件；
- 除了权威的 `toolSource: "mcp"` 之外，稳定的 MCP owner/server 身份；
- 进程硬终止和宿主睡眠/唤醒的精确终止行为；
- 特定于提供商的成本/Token 完整性。
