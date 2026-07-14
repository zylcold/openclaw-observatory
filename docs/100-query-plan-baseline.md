# SQLite Query-plan Baseline

Capture this baseline against a representative 30-day database before changing
analytics SQL or indexes. Plans are metadata-only: do not include event payloads
or identifiers in committed output.

```sh
sqlite3 ~/.openclaw-observatory/observatory.db \
  "EXPLAIN QUERY PLAN SELECT run_id FROM agent_runs
   WHERE instance_id=? AND started_at>=? AND started_at<=?"
```

Expected baseline:

```text
SEARCH agent_runs USING INDEX idx_runs_started (instance_id=? AND started_at>? AND started_at<?)
```

Also capture plans for the dashboard's resource, LLM, tool/MCP, and agent
aggregations. A regression is a full table scan over an unbounded 30-day source
when an instance/time filter is present. Record elapsed time with a cold and warm
SQLite page cache; the v0.5 target for `/agents/stats` is below 500 ms.
