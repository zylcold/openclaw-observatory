# SQLite 查询计划基线

修改分析 SQL 或索引前，应在具有代表性的 30 天数据库上采集此基线。计划仅包含元数据；提交的输出中不得包含事件 payload 或标识符。

```sh
sqlite3 ~/.openclaw-observatory/observatory.db \
  "EXPLAIN QUERY PLAN SELECT run_id FROM agent_runs
   WHERE instance_id=? AND started_at>=? AND started_at<=?"
```

预期基线：

```text
SEARCH agent_runs USING INDEX idx_runs_started (instance_id=? AND started_at>? AND started_at<?)
```

还应采集 Dashboard 中资源、LLM、工具/MCP 与 Agent 聚合的计划。存在实例/时间过滤条件时，若对无界 30 天源表做全表扫描，即视为回归。记录冷、热 SQLite 页缓存下的耗时；v0.5 对 `/agents/stats` 的目标是低于 500 ms。
