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
- ✅ 历史趋势图替换“最近 N 条”模式
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

## 阶段 4 — 高级可观测性

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
