# Grafana Dashboard

受版本控制的 `OpenClaw Observatory Overview` Dashboard 统一上传到远端
<https://grafana.yunlongzhu.com>，项目不部署本地 Grafana。所有面板查询守护进程的
Prometheus 契约。

## 布局

1. Gateway 主动响应状态、响应耗时、活跃会话/运行，以及滚动 24 小时 Token/成本。
2. CPU、内存、文件描述符、IO 和磁盘可用率。
3. Gateway 响应/心跳新鲜度、重启、丢弃事件、队列深度和存储查询耗时。
4. Agent 运行量，以及按 Agent 统计的 Token/成本速率。
5. 模型请求量、Token 用量，以及默认 24 小时范围内、参考 OpenRouter 的按小时
   Token/成本堆积柱状图。
6. Agent × Model 的 24 小时模型占比、Token 占比、归因成本和速率。

Dashboard 提供 Prometheus datasource 选择器，以及 `instance`、`agent`、`model`
三个有界多选变量，所有面板统一跟随所选远端 datasource。计数器查询使用 Grafana
的 `$__rate_interval`，远端抓取间隔为 15 秒。Agent × Model 面板限制为 Top 20
序列，避免浏览器与 Prometheus 在高基数下过载。

远端 API 认证统一使用 `GRAFANA_TOKEN`，令牌保存在 OpenClaw 运行环境中且不得提交。
Dashboard UID `openclaw-observatory` 保持稳定，API 上传会更新既有远端 Dashboard，
不会创建重复副本。

归因指标的规范名称为 `openclaw_llm_tokens_by_agent_model_total` 与
`openclaw_llm_cost_usd_by_agent_model_total`；旧名称仅作为兼容别名保留。

## OpenRouter 成本估算

OpenClaw 事件可能返回 `costUsd=0`。Observatory 因此每 6 小时刷新 OpenRouter
模型价格目录，并以 `0600` 权限缓存到
`<data-dir>/openrouter-pricing.json`。每次 LLM 调用按以下顺序计费：

1. 如果上游返回正数成本，始终以实报金额为准；
2. 否则使用缓存目录分别计算输入、输出、缓存读取和缓存写入 Token；
3. 无法识别的模型计入 `openclaw_llm_unpriced_tokens_total`，不会静默套用错误价格。

最终累计成本由 `openclaw_llm_cost_usd_total` 与
`openclaw_llm_cost_usd_by_agent_model_total` 暴露，Grafana 使用 `increase()`
和 `rate()` 计算趋势。精确滚动 24 小时金额直接使用按调用时间计算的
`openclaw_llm_cost_usd_24h` 与
`openclaw_llm_cost_usd_by_agent_model_24h` gauge，避免新 counter 首次上线时
把历史回填误算进当前窗口。

通常读取公开目录不需要密钥。如果 OpenRouter 要求认证，可在 Observatory
守护进程环境中设置 `OPENROUTER_API_KEY`。也可通过 `--pricing-url` 和
`--pricing-refresh-interval` 调整接口及刷新周期；周期设为 `0` 时停止刷新，
继续使用内置或已缓存价格。

目录健康度由 `openclaw_pricing_catalog_models` 和
`openclaw_pricing_catalog_last_success_unixtime` 暴露。

## 告警默认值

- `OpenClawGatewayDown`：Gateway 宕机 2 分钟；
- `OpenClawGatewayUnresponsive`：主动 HTTP 健康检查失败 30 秒；
- `OpenClawGatewaySlowResponse`：主动健康检查超过 2 秒并持续 5 分钟；
- `OpenClawGatewayHeartbeatStale`：超过 2 分钟没有 Gateway 心跳；
- `OpenClawObservatoryDown`：Prometheus 无法抓取 Observatory 持续 1 分钟；
- `OpenClawHighMemory`：RSS 超过 2 GiB 持续 10 分钟；
- `OpenClawHighLLMErrorRate`：错误率超过 10% 持续 10 分钟；
- `OpenClawToolErrorSpike`：5 分钟内超过 5 个错误；
- `OpenClawMonitorDroppingEvents`：10 分钟内有任何丢弃。

这些是初始示例，不是通用的安全阈值。运维人员必须根据模型延迟、宿主内存、工作负载大小和预期工具失败来调整。
