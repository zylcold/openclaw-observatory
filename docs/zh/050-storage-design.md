# 存储设计

SQLite 是本地系统记录。守护进程使用单一逻辑写入者、短事务、WAL 模式、`foreign_keys=ON` 和 busy timeout。

## 表

| 表 | 主/外键和重要字段 | 索引 |
| --- | --- | --- |
| `instances` | `instance_id` PK；版本、PID、状态、启动/最后可见/停止 | status, last_seen |
| `sessions` | `(instance_id, session_id)` PK；Agent/启动/结束/状态/原因/消息数；FK instance | agent, start, status |
| `agent_runs` | `(instance_id, run_id)` PK；`session_id` FK；Agent/提供商/模型/状态/时间/持续时间 | agent, session, start, status |
| `llm_calls` | `(instance_id, call_id)` PK；`run_id` FK；提供商/模型/状态/时间/用量/成本 | run, provider+model, start |
| `tool_calls` | `(instance_id, tool_call_id)` PK；`run_id` FK；工具/来源/状态/持续时间 | run, tool+status, start |
| `mcp_calls` | 同生命周期字段加有界 owner/server | run, tool+status |
| `subagent_runs` | `(instance_id, subagent_id)` PK；父/子会话/运行/状态 | parent run, child session |
| `resource_samples` | 整数 PK；instance/PID/时间和进程测量 | instance+time |
| `events` | `event_id` PK；信封列和载荷 JSON | instance+time, type+time, producer+sequence |
| `daily_stats` | `(day, instance_id, dimension_type, dimension_value)` PK；聚合 JSON | instance+day |
| `schema_migrations` | `version` PK，应用时间 | 无 |

外键在概念模型中是可延迟的，因为终止/乱序事件可能创建占位父级。MVP 归约器使用 upsert 和可空父引用，避免拒绝有用的终止事实。

## 事件和投影关系

`events` 是不可变元数据账本。投影表是查询优化的状态。插入事件和更新投影在一个事务中完成：

1. `INSERT ... ON CONFLICT(event_id) DO NOTHING`；
2. 如果已插入，运行事件类型归约器；
3. 两者一起提交或不提交。

这使得重复投递是安全的。投影可后续通过按 `(occurred_at, producer_id, sequence)` 顺序回放事件重建。

## 崩溃恢复

WAL 保护已提交的事务。重启时，活跃行与实例活跃性比较。如果其 Gateway 确认已死，它们变为 `incomplete` 而非虚假的 `failed`；合成的 `gateway.crashed` 记录原因。

## 迁移

迁移是有序的、内嵌的 SQL 事务。守护进程拒绝运行比其支持 Schema 更新的数据库。破坏性迁移使用创建-复制-验证-重命名策略，从不与保留工作混合。

Schema v2 为会话和 Agent 运行增加 `agent_id`。Schema v3 从不可变事件账本回填这些字段，再使用会话和子 Agent 归属作为后备。升级完成后，已经采集的运行也能立即进入按 Agent 统计的时间线。

## 资源保留

- 最近 1 小时：保留 5 秒采样；
- 1-24 小时：聚合到 1 分钟；
- 1-30 天：聚合到 15 分钟；
- 超过 30 天：每日聚合或按运维策略删除。

保留以小批量索引方式运行。聚合在原始采样删除前写入，在同一事务中完成。
