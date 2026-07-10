# Grafana Dashboard

已配置的 `OpenClaw Observatory Overview` Dashboard 刻意保持精简但可导入。所有面板查询守护进程的 Prometheus 契约。

## 布局

1. Gateway 状态、运行时间、活跃会话/运行、今日 Token 和成本。
2. CPU、RSS、虚拟内存、线程、文件描述符和 IO。
3. LLM 请求、输入/输出 Token、延迟百分位、错误、提供商和模型。
4. Agent 运行数量/持续时间、工具调用/错误、慢工具、队列深度。
5. Gateway 重启、丢弃事件、监控队列深度和近期告警。

MVP JSON 聚焦于关键状态和数量面板。后续迭代须保持稳定的指标查询，添加有界 `instance`、`provider` 和 `model` 变量，并在固定 Grafana 镜像上测试配置。

## 告警默认值

- `OpenClawGatewayDown`：Gateway 宕机 2 分钟；
- `OpenClawHighMemory`：RSS 超过 2 GiB 持续 10 分钟；
- `OpenClawHighLLMErrorRate`：错误率超过 10% 持续 10 分钟；
- `OpenClawToolErrorSpike`：5 分钟内超过 5 个错误；
- `OpenClawMonitorDroppingEvents`：10 分钟内有任何丢弃。

这些是初始示例，不是通用的安全阈值。运维人员必须根据模型延迟、宿主内存、工作负载大小和预期工具失败来调整。
