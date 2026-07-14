package storage

import (
	"context"
	"database/sql"
	"fmt"
)

func analyticsArgs(o ListOptions) []any {
	return []any{
		sql.Named("from", o.From),
		sql.Named("to", o.To),
		sql.Named("instance", o.InstanceID),
		sql.Named("agent", o.AgentID),
	}
}

const runAgentsCTE = `run_agents AS (
  SELECT ar.instance_id,ar.run_id,ar.session_id,ar.status,ar.started_at,ar.ended_at,ar.duration_ms,ar.error_category,
    COALESCE(NULLIF(ar.agent_id,''),NULLIF(sr.agent_id,''),NULLIF(s.agent_id,''),'unknown') AS agent_id
  FROM agent_runs ar
  LEFT JOIN sessions s ON s.instance_id=ar.instance_id AND s.session_id=ar.session_id
  LEFT JOIN subagent_runs sr ON sr.instance_id=ar.instance_id AND sr.subagent_id=ar.run_id
)`

// agentStatsRunAgentsCTE applies the broadest filters before joining related
// tables, keeping the downstream aggregate CTEs proportional to the selected
// dashboard range rather than the full agent_runs table.
const agentStatsRunAgentsCTE = `filtered_agent_runs AS (
  SELECT instance_id,run_id,session_id,status,started_at,ended_at,duration_ms,error_category,agent_id
  FROM agent_runs
  WHERE (@instance='' OR instance_id=@instance)
    AND (@from='' OR started_at>=@from) AND (@to='' OR started_at<=@to)
), run_agents AS (
  SELECT ar.instance_id,ar.run_id,ar.session_id,ar.status,ar.started_at,ar.ended_at,ar.duration_ms,ar.error_category,
    COALESCE(NULLIF(ar.agent_id,''),NULLIF(sr.agent_id,''),NULLIF(s.agent_id,''),'unknown') AS agent_id
  FROM filtered_agent_runs ar
  LEFT JOIN sessions s ON s.instance_id=ar.instance_id AND s.session_id=ar.session_id
  LEFT JOIN subagent_runs sr ON sr.instance_id=ar.instance_id AND sr.subagent_id=ar.run_id
)`

func (r *Repository) AgentStats(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	q := `WITH ` + agentStatsRunAgentsCTE + `,
  filtered_runs AS (
    SELECT * FROM run_agents
    WHERE (@agent='' OR agent_id=@agent)
  ),
  llm_by_run AS (
    SELECT l.instance_id,l.run_id,COUNT(*) AS requests,
      SUM(CASE WHEN l.status='failed' THEN 1 ELSE 0 END) AS errors,
      SUM(l.input_tokens) AS input_tokens,SUM(l.output_tokens) AS output_tokens,
      SUM(l.cache_read_tokens) AS cache_read_tokens,SUM(l.cache_write_tokens) AS cache_write_tokens,
      SUM(l.cost_usd) AS cost_usd,SUM(COALESCE(l.duration_ms,0)) AS duration_ms
    FROM llm_calls l JOIN filtered_runs f ON f.instance_id=l.instance_id AND f.run_id=l.run_id
    GROUP BY l.instance_id,l.run_id
  ),
  tool_events AS (
    SELECT t.instance_id,t.run_id,t.status,t.duration_ms FROM tool_calls t
      JOIN filtered_runs f ON f.instance_id=t.instance_id AND f.run_id=t.run_id
    UNION ALL
    SELECT m.instance_id,m.run_id,m.status,m.duration_ms FROM mcp_calls m
      JOIN filtered_runs f ON f.instance_id=m.instance_id AND f.run_id=m.run_id
  ),
  tools_by_run AS (
    SELECT instance_id,run_id,COUNT(*) AS calls,
      SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END) AS errors,
      SUM(COALESCE(duration_ms,0)) AS duration_ms
    FROM tool_events GROUP BY instance_id,run_id
  )
  SELECT f.agent_id AS agentId,COUNT(*) AS runs,
    SUM(CASE WHEN f.status='failed' THEN 1 ELSE 0 END) AS runErrors,
    100.0*SUM(CASE WHEN f.status='failed' THEN 1 ELSE 0 END)/COUNT(*) AS errorRate,
    SUM(COALESCE(f.duration_ms,0)) AS totalDurationMs,AVG(f.duration_ms) AS averageDurationMs,
    SUM(COALESCE(l.requests,0)) AS llmRequests,SUM(COALESCE(l.errors,0)) AS llmErrors,
    SUM(COALESCE(l.input_tokens,0)) AS inputTokens,SUM(COALESCE(l.output_tokens,0)) AS outputTokens,
    SUM(COALESCE(l.cache_read_tokens,0)) AS cacheReadTokens,SUM(COALESCE(l.cache_write_tokens,0)) AS cacheWriteTokens,
    SUM(COALESCE(l.input_tokens,0)+COALESCE(l.output_tokens,0)+COALESCE(l.cache_read_tokens,0)+COALESCE(l.cache_write_tokens,0)) AS totalTokens,
    SUM(COALESCE(l.cost_usd,0)) AS costUsd,SUM(COALESCE(l.duration_ms,0)) AS llmDurationMs,
    SUM(COALESCE(t.calls,0)) AS toolCalls,SUM(COALESCE(t.errors,0)) AS toolErrors,SUM(COALESCE(t.duration_ms,0)) AS toolDurationMs
  FROM filtered_runs f
  LEFT JOIN llm_by_run l ON l.instance_id=f.instance_id AND l.run_id=f.run_id
  LEFT JOIN tools_by_run t ON t.instance_id=f.instance_id AND t.run_id=f.run_id
  GROUP BY f.agent_id ORDER BY totalTokens DESC,runs DESC`
	return queryMaps(ctx, r.db, q, analyticsArgs(opts)...)
}

func (r *Repository) ToolStats(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	q := `WITH ` + runAgentsCTE + `,
  tool_events AS (
    SELECT t.instance_id,t.run_id,t.tool_name AS tool,t.status,t.duration_ms,t.started_at,'tool' AS source
    FROM tool_calls t
    UNION ALL
    SELECT m.instance_id,m.run_id,m.tool_name AS tool,m.status,m.duration_ms,m.started_at,'mcp' AS source
    FROM mcp_calls m
  )
  SELECT e.source,COALESCE(NULLIF(e.tool,''),'unknown') AS tool,COUNT(*) AS calls,
    SUM(CASE WHEN e.status='failed' THEN 1 ELSE 0 END) AS errors,
    AVG(e.duration_ms) AS averageDurationMs,MAX(e.duration_ms) AS maxDurationMs
  FROM tool_events e LEFT JOIN run_agents r ON r.instance_id=e.instance_id AND r.run_id=e.run_id
  WHERE (@instance='' OR e.instance_id=@instance)
    AND (@from='' OR e.started_at>=@from) AND (@to='' OR e.started_at<=@to)
    AND (@agent='' OR r.agent_id=@agent)
  GROUP BY e.source,e.tool ORDER BY calls DESC LIMIT 200`
	return queryMaps(ctx, r.db, q, analyticsArgs(opts)...)
}

func (r *Repository) ModelStats(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	q := `WITH ` + runAgentsCTE + `
  SELECT COALESCE(NULLIF(l.provider,''),'unknown') AS provider,COALESCE(NULLIF(l.model,''),'unknown') AS model,
    COUNT(*) AS requests,SUM(CASE WHEN l.status='failed' THEN 1 ELSE 0 END) AS errors,
    SUM(l.input_tokens) AS inputTokens,SUM(l.output_tokens) AS outputTokens,
    SUM(l.cache_read_tokens) AS cacheReadTokens,SUM(l.cache_write_tokens) AS cacheWriteTokens,
    SUM(l.cost_usd) AS costUsd,AVG(l.duration_ms) AS averageDurationMs,MAX(l.duration_ms) AS maxDurationMs
  FROM llm_calls l LEFT JOIN run_agents r ON r.instance_id=l.instance_id AND r.run_id=l.run_id
  WHERE (@instance='' OR l.instance_id=@instance)
    AND (@from='' OR l.started_at>=@from) AND (@to='' OR l.started_at<=@to)
    AND (@agent='' OR r.agent_id=@agent)
  GROUP BY l.provider,l.model
  ORDER BY SUM(l.input_tokens+l.output_tokens+l.cache_read_tokens+l.cache_write_tokens) DESC,requests DESC LIMIT 200`
	return queryMaps(ctx, r.db, q, analyticsArgs(opts)...)
}

func (r *Repository) ListLLMCalls(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	o := opts.normalized()
	args := append(analyticsArgs(o), sql.Named("limit", o.Limit))
	q := `WITH ` + runAgentsCTE + `
  SELECT l.instance_id AS instanceId,l.call_id AS callId,l.run_id AS runId,l.session_id AS sessionId,
    COALESCE(r.agent_id,'unknown') AS agentId,l.provider,l.model,l.status,l.started_at AS startedAt,l.ended_at AS endedAt,
    l.duration_ms AS durationMs,l.error_category AS errorCategory,l.input_tokens AS inputTokens,l.output_tokens AS outputTokens,
    l.cache_read_tokens AS cacheReadTokens,l.cache_write_tokens AS cacheWriteTokens,
    l.input_tokens+l.output_tokens+l.cache_read_tokens+l.cache_write_tokens AS totalTokens,l.cost_usd AS costUsd
  FROM llm_calls l LEFT JOIN run_agents r ON r.instance_id=l.instance_id AND r.run_id=l.run_id
  WHERE (@instance='' OR l.instance_id=@instance)
    AND (@from='' OR l.started_at>=@from) AND (@to='' OR l.started_at<=@to)
    AND (@agent='' OR r.agent_id=@agent)
  ORDER BY COALESCE(l.started_at,l.ended_at) DESC LIMIT @limit`
	return queryMaps(ctx, r.db, q, args...)
}

func (r *Repository) ListMCPCalls(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	o := opts.normalized()
	args := append(analyticsArgs(o), sql.Named("limit", o.Limit))
	q := `WITH ` + runAgentsCTE + `
  SELECT m.instance_id AS instanceId,m.call_id AS callId,m.run_id AS runId,m.session_id AS sessionId,
    COALESCE(r.agent_id,'unknown') AS agentId,m.tool_name AS toolName,m.owner,m.status,
    m.started_at AS startedAt,m.ended_at AS endedAt,m.duration_ms AS durationMs,m.error_category AS errorCategory
  FROM mcp_calls m LEFT JOIN run_agents r ON r.instance_id=m.instance_id AND r.run_id=m.run_id
  WHERE (@instance='' OR m.instance_id=@instance)
    AND (@from='' OR m.started_at>=@from) AND (@to='' OR m.started_at<=@to)
    AND (@agent='' OR r.agent_id=@agent)
  ORDER BY COALESCE(m.started_at,m.ended_at) DESC LIMIT @limit`
	return queryMaps(ctx, r.db, q, args...)
}

func (r *Repository) ListSubagentRuns(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	o := opts.normalized()
	args := append(analyticsArgs(o), sql.Named("limit", o.Limit))
	q := `SELECT instance_id AS instanceId,subagent_id AS subagentId,parent_run_id AS parentRunId,
    child_session_hash AS childSessionHash,COALESCE(NULLIF(agent_id,''),'unknown') AS agentId,mode,provider,model,status,
    started_at AS startedAt,ended_at AS endedAt,outcome
  FROM subagent_runs
  WHERE (@instance='' OR instance_id=@instance)
    AND (@from='' OR started_at>=@from) AND (@to='' OR started_at<=@to)
    AND (@agent='' OR COALESCE(NULLIF(agent_id,''),'unknown')=@agent)
  ORDER BY COALESCE(started_at,ended_at) DESC LIMIT @limit`
	return queryMaps(ctx, r.db, q, args...)
}

func (r *Repository) ErrorStats(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	q := `WITH ` + runAgentsCTE + `,
  error_events AS (
    SELECT instance_id,agent_id,'run' AS kind,COALESCE(NULLIF(error_category,''),'unknown') AS category,started_at,duration_ms
      FROM run_agents WHERE status='failed'
    UNION ALL
    SELECT l.instance_id,COALESCE(r.agent_id,'unknown'),'llm',COALESCE(NULLIF(l.error_category,''),'unknown'),l.started_at,l.duration_ms
      FROM llm_calls l LEFT JOIN run_agents r ON r.instance_id=l.instance_id AND r.run_id=l.run_id WHERE l.status='failed'
    UNION ALL
    SELECT t.instance_id,COALESCE(r.agent_id,'unknown'),'tool',COALESCE(NULLIF(t.error_category,''),'unknown'),t.started_at,t.duration_ms
      FROM tool_calls t LEFT JOIN run_agents r ON r.instance_id=t.instance_id AND r.run_id=t.run_id WHERE t.status='failed'
    UNION ALL
    SELECT m.instance_id,COALESCE(r.agent_id,'unknown'),'mcp',COALESCE(NULLIF(m.error_category,''),'unknown'),m.started_at,m.duration_ms
      FROM mcp_calls m LEFT JOIN run_agents r ON r.instance_id=m.instance_id AND r.run_id=m.run_id WHERE m.status='failed'
  )
  SELECT kind,category,COUNT(*) AS errors,AVG(duration_ms) AS averageDurationMs,MAX(started_at) AS lastOccurredAt
  FROM error_events
  WHERE (@instance='' OR instance_id=@instance)
    AND (@from='' OR started_at>=@from) AND (@to='' OR started_at<=@to)
    AND (@agent='' OR agent_id=@agent)
  GROUP BY kind,category ORDER BY errors DESC,lastOccurredAt DESC LIMIT 200`
	return queryMaps(ctx, r.db, q, analyticsArgs(opts)...)
}

func (r *Repository) TimeSeries(ctx context.Context, opts ListOptions, bucketSeconds int64) (map[string]any, error) {
	args := append(analyticsArgs(opts), sql.Named("bucket", bucketSeconds))
	overviewQuery := `WITH ` + runAgentsCTE + `,
  resource_window AS (
    SELECT instance_id,sampled_at,resident_memory_bytes,disk_total_bytes,disk_available_bytes,
      cpu_seconds_total-LAG(cpu_seconds_total) OVER (PARTITION BY instance_id,process_id ORDER BY sampled_at) AS cpu_delta,
      unixepoch(sampled_at)-LAG(unixepoch(sampled_at)) OVER (PARTITION BY instance_id,process_id ORDER BY sampled_at) AS time_delta
    FROM resource_samples
    WHERE (@instance='' OR instance_id=@instance)
      AND (@from='' OR sampled_at>=@from) AND (@to='' OR sampled_at<=@to)
  ),
  series AS (
    SELECT CAST(unixepoch(started_at)/@bucket AS INTEGER)*@bucket AS bucket,
      COUNT(*) AS runs,SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END) AS run_errors,SUM(COALESCE(duration_ms,0)) AS run_duration,
      0 AS llm_requests,0 AS llm_errors,0 AS input_tokens,0 AS output_tokens,0 AS cache_read,0 AS cache_write,0 AS cost_usd,0 AS llm_duration,0 AS llm_duration_count,
      0 AS tool_calls,0 AS tool_errors,0 AS memory_avg,0 AS memory_max,0 AS cpu_avg,0 AS disk_total,0 AS disk_available
    FROM run_agents WHERE (@instance='' OR instance_id=@instance)
      AND (@from='' OR started_at>=@from) AND (@to='' OR started_at<=@to) AND (@agent='' OR agent_id=@agent)
    GROUP BY bucket
    UNION ALL
    SELECT CAST(unixepoch(l.started_at)/@bucket AS INTEGER)*@bucket,0,0,0,
      COUNT(*),SUM(CASE WHEN l.status='failed' THEN 1 ELSE 0 END),SUM(l.input_tokens),SUM(l.output_tokens),SUM(l.cache_read_tokens),SUM(l.cache_write_tokens),SUM(l.cost_usd),SUM(COALESCE(l.duration_ms,0)),COUNT(l.duration_ms),
      0,0,0,0,0,0,0
    FROM llm_calls l LEFT JOIN run_agents r ON r.instance_id=l.instance_id AND r.run_id=l.run_id
    WHERE (@instance='' OR l.instance_id=@instance)
      AND (@from='' OR l.started_at>=@from) AND (@to='' OR l.started_at<=@to) AND (@agent='' OR r.agent_id=@agent)
    GROUP BY 1
    UNION ALL
    SELECT bucket,0,0,0,0,0,0,0,0,0,0,0,0,COUNT(*),SUM(failed),0,0,0,0,0 FROM (
      SELECT CAST(unixepoch(t.started_at)/@bucket AS INTEGER)*@bucket AS bucket,CASE WHEN t.status='failed' THEN 1 ELSE 0 END AS failed
        FROM tool_calls t LEFT JOIN run_agents r ON r.instance_id=t.instance_id AND r.run_id=t.run_id
        WHERE (@instance='' OR t.instance_id=@instance) AND (@from='' OR t.started_at>=@from) AND (@to='' OR t.started_at<=@to) AND (@agent='' OR r.agent_id=@agent)
      UNION ALL
      SELECT CAST(unixepoch(m.started_at)/@bucket AS INTEGER)*@bucket,CASE WHEN m.status='failed' THEN 1 ELSE 0 END
        FROM mcp_calls m LEFT JOIN run_agents r ON r.instance_id=m.instance_id AND r.run_id=m.run_id
        WHERE (@instance='' OR m.instance_id=@instance) AND (@from='' OR m.started_at>=@from) AND (@to='' OR m.started_at<=@to) AND (@agent='' OR r.agent_id=@agent)
    ) GROUP BY bucket
    UNION ALL
    SELECT CAST(unixepoch(sampled_at)/@bucket AS INTEGER)*@bucket,0,0,0,0,0,0,0,0,0,0,0,0,0,0,
      AVG(resident_memory_bytes),MAX(resident_memory_bytes),AVG(CASE WHEN time_delta>0 THEN 100.0*cpu_delta/time_delta END),
      AVG(disk_total_bytes),AVG(disk_available_bytes)
    FROM resource_window GROUP BY 1
  )
  SELECT strftime('%Y-%m-%dT%H:%M:%SZ',bucket,'unixepoch') AS time,
    SUM(runs) AS runs,SUM(run_errors) AS runErrors,SUM(run_duration) AS runDurationMs,
    SUM(llm_requests) AS llmRequests,SUM(llm_errors) AS llmErrors,
    CASE WHEN SUM(llm_requests)>0 THEN 100.0*SUM(llm_errors)/SUM(llm_requests) ELSE 0 END AS llmErrorRate,
    SUM(input_tokens) AS inputTokens,SUM(output_tokens) AS outputTokens,SUM(cache_read) AS cacheReadTokens,SUM(cache_write) AS cacheWriteTokens,
    SUM(cost_usd) AS costUsd,CASE WHEN SUM(llm_duration_count)>0 THEN SUM(llm_duration)/SUM(llm_duration_count) ELSE 0 END AS averageLlmDurationMs,
    SUM(tool_calls) AS toolCalls,SUM(tool_errors) AS toolErrors,MAX(memory_avg) AS averageMemoryBytes,MAX(memory_max) AS maxMemoryBytes,MAX(cpu_avg) AS averageCpuPercent,
    MAX(disk_total) AS diskTotalBytes,MAX(disk_available) AS diskAvailableBytes,
    CASE WHEN MAX(disk_total)>0 THEN 100.0*(MAX(disk_total)-MAX(disk_available))/MAX(disk_total) ELSE 0 END AS diskUsedPercent
  FROM series WHERE bucket IS NOT NULL GROUP BY bucket ORDER BY bucket`
	points, err := queryMaps(ctx, r.db, overviewQuery, args...)
	if err != nil {
		return nil, err
	}

	modelQuery := `WITH ` + runAgentsCTE + `
  SELECT strftime('%Y-%m-%dT%H:%M:%SZ',CAST(unixepoch(l.started_at)/@bucket AS INTEGER)*@bucket,'unixepoch') AS time,
    COALESCE(NULLIF(l.provider,''),'unknown') AS provider,COALESCE(NULLIF(l.model,''),'unknown') AS model,
    COUNT(*) AS requests,SUM(l.input_tokens) AS inputTokens,SUM(l.output_tokens) AS outputTokens,
    SUM(l.cache_read_tokens) AS cacheReadTokens,SUM(l.cache_write_tokens) AS cacheWriteTokens,SUM(l.cost_usd) AS costUsd
  FROM llm_calls l LEFT JOIN run_agents r ON r.instance_id=l.instance_id AND r.run_id=l.run_id
  WHERE (@instance='' OR l.instance_id=@instance)
    AND (@from='' OR l.started_at>=@from) AND (@to='' OR l.started_at<=@to) AND (@agent='' OR r.agent_id=@agent)
  GROUP BY 1,l.provider,l.model ORDER BY 1,requests DESC`
	models, err := queryMaps(ctx, r.db, modelQuery, args...)
	if err != nil {
		return nil, err
	}

	agentQuery := `WITH ` + runAgentsCTE + `
  SELECT strftime('%Y-%m-%dT%H:%M:%SZ',CAST(unixepoch(started_at)/@bucket AS INTEGER)*@bucket,'unixepoch') AS time,
    agent_id AS agentId,COUNT(*) AS runs,SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END) AS errors,SUM(COALESCE(duration_ms,0)) AS durationMs
  FROM run_agents WHERE (@instance='' OR instance_id=@instance)
    AND (@from='' OR started_at>=@from) AND (@to='' OR started_at<=@to) AND (@agent='' OR agent_id=@agent)
  GROUP BY 1,agent_id ORDER BY 1,runs DESC`
	agents, err := queryMaps(ctx, r.db, agentQuery, args...)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"bucketSeconds": bucketSeconds,
		"from":          opts.From,
		"to":            opts.To,
		"points":        points,
		"models":        models,
		"agents":        agents,
	}, nil
}

// CostTrends returns daily cost breakdowns grouped by provider/model and agent.
// The period parameter controls the grouping: "day", "week", or "month".
func (r *Repository) CostTrends(ctx context.Context, opts ListOptions, period string) ([]map[string]any, error) {
	var dateFormat string
	switch period {
	case "week":
		dateFormat = `strftime('%Y-W%W', l.started_at)`
	case "month":
		dateFormat = `strftime('%Y-%m', l.started_at)`
	default:
		dateFormat = `strftime('%Y-%m-%d', l.started_at)`
	}

	// Cost by provider/model per period
	q := fmt.Sprintf(`WITH %s
  SELECT %s AS period,
    COALESCE(NULLIF(l.provider,''),'unknown') AS provider,
    COALESCE(NULLIF(l.model,''),'unknown') AS model,
    COUNT(*) AS requests,
    SUM(l.input_tokens) AS inputTokens,
    SUM(l.output_tokens) AS outputTokens,
    SUM(l.cache_read_tokens) AS cacheReadTokens,
    SUM(l.cache_write_tokens) AS cacheWriteTokens,
    SUM(l.cost_usd) AS costUsd
  FROM llm_calls l LEFT JOIN run_agents r ON r.instance_id=l.instance_id AND r.run_id=l.run_id
  WHERE (@instance='' OR l.instance_id=@instance)
    AND (@from='' OR l.started_at>=@from) AND (@to='' OR l.started_at<=@to)
    AND (@agent='' OR r.agent_id=@agent)
  GROUP BY period, l.provider, l.model
  ORDER BY period DESC, costUsd DESC`, runAgentsCTE, dateFormat)

	return queryMaps(ctx, r.db, q, analyticsArgs(opts)...)
}

// CostSummary returns aggregate cost stats for the given period.
func (r *Repository) CostSummary(ctx context.Context, opts ListOptions) (map[string]any, error) {
	q := fmt.Sprintf(`WITH %s
  SELECT
    COUNT(*) AS totalRequests,
    SUM(l.cost_usd) AS totalCost,
    SUM(l.input_tokens+l.output_tokens+l.cache_read_tokens+l.cache_write_tokens) AS totalTokens,
    AVG(l.cost_usd) AS avgCostPerRequest,
    MAX(l.cost_usd) AS maxCostPerRequest,
    SUM(CASE WHEN l.started_at >= datetime('now', '-1 day') THEN l.cost_usd ELSE 0 END) AS lastDayCost,
    SUM(CASE WHEN l.started_at >= datetime('now', '-7 days') THEN l.cost_usd ELSE 0 END) AS lastWeekCost,
    SUM(CASE WHEN l.started_at >= datetime('now', '-30 days') THEN l.cost_usd ELSE 0 END) AS lastMonthCost
  FROM llm_calls l LEFT JOIN run_agents r ON r.instance_id=l.instance_id AND r.run_id=l.run_id
  WHERE (@instance='' OR l.instance_id=@instance)
    AND (@from='' OR l.started_at>=@from) AND (@to='' OR l.started_at<=@to)
    AND (@agent='' OR r.agent_id=@agent)`, runAgentsCTE)

	rows, err := queryMaps(ctx, r.db, q, analyticsArgs(opts)...)
	if err != nil || len(rows) == 0 {
		if err == nil {
			return map[string]any{"totalRequests": 0, "totalCost": 0, "totalTokens": 0}, nil
		}
		return nil, err
	}
	return rows[0], nil
}
