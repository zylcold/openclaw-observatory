package storage

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// PageResult wraps a list query result with pagination metadata.
type PageResult struct {
	Data       []map[string]any `json:"data"`
	NextCursor string           `json:"nextCursor"`
}

// EncodeCursor encodes a position map into a safe opaque string.
func EncodeCursor(pos map[string]any) string {
	return encodeCursor(pos)
}

// encodeCursor encodes a position map into a safe opaque string.
func encodeCursor(pos map[string]any) string {
	b, _ := json.Marshal(pos)
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeCursor decodes an opaque cursor string back into a position map.
// Returns nil if the cursor is empty or invalid.
func decodeCursor(s string) map[string]any {
	if s == "" {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

type ListOptions struct {
	Limit      int
	Cursor     string
	InstanceID string
	AgentID    string
	From       string
	To         string
	EventType  string
	Status     string
}

func (o ListOptions) normalized() ListOptions {
	if o.Limit <= 0 {
		o.Limit = 100
	}
	if o.Limit > 500 {
		o.Limit = 500
	}
	return o
}

func (r *Repository) ListInstances(ctx context.Context) ([]map[string]any, error) {
	return queryMaps(ctx, r.db, `SELECT instance_id AS instanceId,plugin_version AS pluginVersion,openclaw_version AS openclawVersion,
    process_id AS processId,status,started_at AS startedAt,last_seen_at AS lastSeenAt,stopped_at AS stoppedAt FROM instances ORDER BY last_seen_at DESC`)
}

func (r *Repository) ListSessions(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	o := opts.normalized()
	q := `SELECT instance_id AS instanceId,session_id AS sessionId,session_key_hash AS sessionKeyHash,agent_id AS agentId,status,
    started_at AS startedAt,ended_at AS endedAt,end_reason AS endReason,message_count AS messageCount,
    (SELECT COUNT(*) FROM llm_calls l WHERE l.instance_id=sessions.instance_id AND l.session_id=sessions.session_id) AS llmCalls,
    (SELECT COUNT(*) FROM tool_calls t WHERE t.instance_id=sessions.instance_id AND t.session_id=sessions.session_id)
      +(SELECT COUNT(*) FROM mcp_calls m WHERE m.instance_id=sessions.instance_id AND m.session_id=sessions.session_id) AS toolCalls,
    (SELECT COALESCE(SUM(l.input_tokens+l.output_tokens+l.cache_read_tokens+l.cache_write_tokens),0) FROM llm_calls l
      WHERE l.instance_id=sessions.instance_id AND l.session_id=sessions.session_id) AS totalTokens,
    (SELECT COALESCE(SUM(l.cost_usd),0) FROM llm_calls l WHERE l.instance_id=sessions.instance_id AND l.session_id=sessions.session_id) AS costUsd,
    (SELECT COUNT(*) FROM llm_calls l WHERE l.instance_id=sessions.instance_id AND l.session_id=sessions.session_id AND l.status='failed')
      +(SELECT COUNT(*) FROM tool_calls t WHERE t.instance_id=sessions.instance_id AND t.session_id=sessions.session_id AND t.status='failed')
      +(SELECT COUNT(*) FROM mcp_calls m WHERE m.instance_id=sessions.instance_id AND m.session_id=sessions.session_id AND m.status='failed') AS errors,
    (SELECT COUNT(*) FROM retry_events re WHERE re.instance_id=sessions.instance_id AND re.session_id=sessions.session_id) AS retries
    FROM sessions WHERE 1=1`
	var args []any
	q, args = filters(q, args, o, "started_at", true)
	if o.AgentID != "" {
		q += ` AND COALESCE(NULLIF(agent_id,''),'unknown')=?`
		args = append(args, o.AgentID)
	}
	// Cursor: sessions are ordered by COALESCE(started_at,ended_at) DESC, session_id DESC
	if cur := decodeCursor(o.Cursor); cur != nil {
		if t, ok := cur["t"].(string); ok {
			q += ` AND COALESCE(started_at,ended_at)<=?`
			args = append(args, t)
		}
		if id, ok := cur["id"].(string); ok && id != "" {
			q += ` AND session_id<?`
			args = append(args, id)
		}
	}
	q += ` ORDER BY COALESCE(started_at,ended_at) DESC, session_id DESC LIMIT ?`
	args = append(args, o.Limit+1) // fetch one extra to determine nextCursor
	rows, err := queryMaps(ctx, r.db, q, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *Repository) ListRuns(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	o := opts.normalized()
	q := `SELECT instance_id AS instanceId,run_id AS runId,session_id AS sessionId,
	    COALESCE(NULLIF(agent_runs.agent_id,''),NULLIF((SELECT agent_id FROM subagent_runs WHERE subagent_runs.instance_id=agent_runs.instance_id AND subagent_runs.subagent_id=agent_runs.run_id LIMIT 1),''),NULLIF((SELECT agent_id FROM sessions WHERE sessions.instance_id=agent_runs.instance_id AND sessions.session_id=agent_runs.session_id LIMIT 1),''),'unknown') AS agentId,
	    provider,model,channel,trigger,status,
    started_at AS startedAt,ended_at AS endedAt,duration_ms AS durationMs,error_category AS errorCategory FROM agent_runs WHERE 1=1`
	var args []any
	q, args = filters(q, args, o, "started_at", true)
	if o.AgentID != "" {
		q += ` AND COALESCE(NULLIF(agent_runs.agent_id,''),NULLIF((SELECT agent_id FROM subagent_runs WHERE subagent_runs.instance_id=agent_runs.instance_id AND subagent_runs.subagent_id=agent_runs.run_id LIMIT 1),''),NULLIF((SELECT agent_id FROM sessions WHERE sessions.instance_id=agent_runs.instance_id AND sessions.session_id=agent_runs.session_id LIMIT 1),''),'unknown')=?`
		args = append(args, o.AgentID)
	}
	if cur := decodeCursor(o.Cursor); cur != nil {
		if t, ok := cur["t"].(string); ok {
			q += ` AND COALESCE(started_at,ended_at)<=?`
			args = append(args, t)
		}
		if id, ok := cur["id"].(string); ok && id != "" {
			q += ` AND run_id<?`
			args = append(args, id)
		}
	}
	q += ` ORDER BY COALESCE(started_at,ended_at) DESC, run_id DESC LIMIT ?`
	args = append(args, o.Limit+1)
	rows, err := queryMaps(ctx, r.db, q, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *Repository) ListResources(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	o := opts.normalized()
	q := `SELECT id,event_id AS eventId,instance_id AS instanceId,process_id AS processId,sampled_at AS sampledAt,
    cpu_seconds_total AS cpuSecondsTotal,resident_memory_bytes AS residentMemoryBytes,virtual_memory_bytes AS virtualMemoryBytes,
    threads,open_fds AS openFds,read_bytes AS readBytesTotal,write_bytes AS writeBytesTotal,
    disk_total_bytes AS diskTotalBytes,disk_available_bytes AS diskAvailableBytes FROM resource_samples WHERE 1=1`
	var args []any
	q, args = filters(q, args, o, "sampled_at", false)
	q += ` ORDER BY sampled_at DESC LIMIT ?`
	args = append(args, o.Limit)
	return queryMaps(ctx, r.db, q, args...)
}

func (r *Repository) ListEvents(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	o := opts.normalized()
	q := `SELECT event_id AS eventId,schema_version AS schemaVersion,event_type AS eventType,occurred_at AS occurredAt,
    instance_id AS instanceId,producer_id AS producerId,process_id AS processId,sequence,source,json(payload_json) AS payload FROM events WHERE 1=1`
	var args []any
	q, args = filters(q, args, o, "occurred_at", false)
	if o.EventType != "" {
		q += ` AND event_type=?`
		args = append(args, o.EventType)
	}
	if cur := decodeCursor(o.Cursor); cur != nil {
		if t, ok := cur["t"].(string); ok {
			q += ` AND occurred_at<=?`
			args = append(args, t)
		}
		if id, ok := cur["id"].(string); ok && id != "" {
			q += ` AND event_id<?`
			args = append(args, id)
		}
	}
	q += ` ORDER BY occurred_at DESC, event_id DESC LIMIT ?`
	args = append(args, o.Limit+1)
	rows, err := queryMaps(ctx, r.db, q, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func filters(q string, args []any, o ListOptions, timeColumn string, status bool) (string, []any) {
	if o.InstanceID != "" {
		q += ` AND instance_id=?`
		args = append(args, o.InstanceID)
	}
	if o.From != "" {
		q += ` AND ` + timeColumn + `>=?`
		args = append(args, o.From)
	}
	if o.To != "" {
		q += ` AND ` + timeColumn + `<=?`
		args = append(args, o.To)
	}
	if status && o.Status != "" {
		q += ` AND status=?`
		args = append(args, o.Status)
	}
	return q, args
}

func (r *Repository) SessionDetail(ctx context.Context, id string) (map[string]any, error) {
	rows, err := queryMaps(ctx, r.db, `SELECT instance_id AS instanceId,session_id AS sessionId,session_key_hash AS sessionKeyHash,agent_id AS agentId,status,
    started_at AS startedAt,ended_at AS endedAt,end_reason AS endReason,message_count AS messageCount FROM sessions WHERE session_id=? LIMIT 1`, id)
	if err != nil || len(rows) == 0 {
		if err == nil {
			err = sql.ErrNoRows
		}
		return nil, err
	}
	runs, err := queryMaps(ctx, r.db, `SELECT run_id AS runId,COALESCE(NULLIF(agent_id,''),NULLIF((SELECT agent_id FROM sessions WHERE sessions.instance_id=agent_runs.instance_id AND sessions.session_id=agent_runs.session_id LIMIT 1),''),'unknown') AS agentId,
    provider,model,status,started_at AS startedAt,ended_at AS endedAt,duration_ms AS durationMs,
    COALESCE(NULLIF(trace_id,''),run_id) AS traceId,COALESCE(NULLIF(span_id,''),run_id) AS spanId,parent_span_id AS parentSpanId
    FROM agent_runs WHERE session_id=? ORDER BY started_at DESC LIMIT 500`, id)
	if err != nil {
		return nil, err
	}
	rows[0]["runs"] = runs
	timeline, err := queryMaps(ctx, r.db, `SELECT * FROM (
      SELECT 'llm' AS kind,l.call_id AS id,l.run_id AS runId,
        COALESCE(NULLIF(l.trace_id,''),NULLIF(r.trace_id,''),l.run_id,l.session_id) AS traceId,
        COALESCE(NULLIF(l.span_id,''),l.call_id) AS spanId,COALESCE(NULLIF(l.parent_span_id,''),NULLIF(r.span_id,''),l.run_id) AS parentSpanId,
        COALESCE(NULLIF(l.model,''),'unknown') AS label,l.provider,l.model,NULL AS toolName,l.status,
        COALESCE(l.started_at,l.ended_at) AS startedAt,l.ended_at AS endedAt,l.duration_ms AS durationMs,l.error_category AS errorCategory,
        l.input_tokens AS inputTokens,l.output_tokens AS outputTokens,l.cache_read_tokens AS cacheReadTokens,l.cache_write_tokens AS cacheWriteTokens,l.cost_usd AS costUsd,
        l.time_to_first_byte_ms AS timeToFirstByteMs,l.time_to_first_token_ms AS timeToFirstTokenMs,l.generation_duration_ms AS generationDurationMs,
        l.stop_reason AS stopReason,l.attempt,l.retry_reason AS retryReason,l.request_bytes AS requestBytes,l.response_bytes AS responseBytes
      FROM llm_calls l LEFT JOIN agent_runs r ON r.instance_id=l.instance_id AND r.run_id=l.run_id
      WHERE l.session_id=?
      UNION ALL
      SELECT 'tool',t.tool_call_id,t.run_id,
        COALESCE(NULLIF(t.trace_id,''),NULLIF(r.trace_id,''),t.run_id,t.session_id),
        COALESCE(NULLIF(t.span_id,''),t.tool_call_id),COALESCE(NULLIF(t.parent_span_id,''),NULLIF(r.span_id,''),t.run_id),
        COALESCE(NULLIF(t.tool_name,''),'unknown'),NULL,NULL,t.tool_name,t.status,COALESCE(t.started_at,t.ended_at),t.ended_at,t.duration_ms,t.error_category,
        NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,t.attempt,t.retry_reason,NULL,NULL
      FROM tool_calls t LEFT JOIN agent_runs r ON r.instance_id=t.instance_id AND r.run_id=t.run_id WHERE t.session_id=?
      UNION ALL
      SELECT 'mcp',m.call_id,m.run_id,
        COALESCE(NULLIF(m.trace_id,''),NULLIF(r.trace_id,''),m.run_id,m.session_id),
        COALESCE(NULLIF(m.span_id,''),m.call_id),COALESCE(NULLIF(m.parent_span_id,''),NULLIF(r.span_id,''),m.run_id),
        COALESCE(NULLIF(m.tool_name,''),'unknown'),NULL,NULL,m.tool_name,m.status,COALESCE(m.started_at,m.ended_at),m.ended_at,m.duration_ms,m.error_category,
        NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,m.attempt,m.retry_reason,NULL,NULL
      FROM mcp_calls m LEFT JOIN agent_runs r ON r.instance_id=m.instance_id AND r.run_id=m.run_id WHERE m.session_id=?
      UNION ALL
      SELECT 'subagent',sr.subagent_id,sr.parent_run_id,
        COALESCE(NULLIF(sr.trace_id,''),NULLIF(r.trace_id,''),sr.parent_run_id),
        COALESCE(NULLIF(sr.span_id,''),sr.subagent_id),COALESCE(NULLIF(sr.parent_span_id,''),NULLIF(r.span_id,''),sr.parent_run_id),
        COALESCE(NULLIF(sr.agent_id,''),'unknown'),sr.provider,sr.model,NULL,sr.status,COALESCE(sr.started_at,sr.ended_at),sr.ended_at,
        CASE WHEN sr.started_at IS NOT NULL AND sr.ended_at IS NOT NULL THEN 1000.0*(julianday(sr.ended_at)-julianday(sr.started_at))*86400.0 END,
        CASE WHEN sr.status='failed' THEN COALESCE(NULLIF(sr.outcome,''),'unknown') END,
        NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL
      FROM subagent_runs sr LEFT JOIN agent_runs r ON r.instance_id=sr.instance_id AND r.run_id=sr.parent_run_id
      WHERE sr.parent_run_id IN (SELECT run_id FROM agent_runs WHERE session_id=?)
      UNION ALL
      SELECT 'retry',re.event_id,re.run_id,COALESCE(NULLIF(re.trace_id,''),re.run_id,re.session_id),
        COALESCE(NULLIF(re.span_id,''),re.event_id),COALESCE(NULLIF(re.parent_span_id,''),re.run_id),
        COALESCE(NULLIF(re.to_model,''),NULLIF(re.to_provider,''),'model retry'),re.to_provider,re.to_model,NULL,'retried',
        re.occurred_at,re.occurred_at,0,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,re.attempt,re.reason,NULL,NULL
      FROM retry_events re WHERE re.session_id=?
    ) WHERE startedAt IS NOT NULL ORDER BY startedAt,id LIMIT 2000`, id, id, id, id, id)
	if err != nil {
		return nil, err
	}
	rows[0]["timeline"] = timeline
	summary, err := queryMaps(ctx, r.db, `SELECT
      (SELECT COUNT(*) FROM llm_calls WHERE session_id=?) AS llmCalls,
      (SELECT COUNT(*) FROM tool_calls WHERE session_id=?) + (SELECT COUNT(*) FROM mcp_calls WHERE session_id=?) AS toolCalls,
      (SELECT COALESCE(SUM(input_tokens+output_tokens+cache_read_tokens+cache_write_tokens),0) FROM llm_calls WHERE session_id=?) AS totalTokens,
      (SELECT COALESCE(SUM(cost_usd),0) FROM llm_calls WHERE session_id=?) AS costUsd,
      (SELECT COUNT(*) FROM llm_calls WHERE session_id=? AND status='failed')
        + (SELECT COUNT(*) FROM tool_calls WHERE session_id=? AND status='failed')
        + (SELECT COUNT(*) FROM mcp_calls WHERE session_id=? AND status='failed') AS errors,
      (SELECT COUNT(*) FROM retry_events WHERE session_id=?) AS retries`,
		id, id, id, id, id, id, id, id, id)
	if err != nil {
		return nil, err
	}
	if len(summary) > 0 {
		rows[0]["summary"] = summary[0]
	}
	return rows[0], nil
}

func (r *Repository) RunDetail(ctx context.Context, id string) (map[string]any, error) {
	rows, err := queryMaps(ctx, r.db, `SELECT instance_id AS instanceId,run_id AS runId,session_id AS sessionId,COALESCE(NULLIF(agent_id,''),NULLIF((SELECT agent_id FROM sessions WHERE sessions.instance_id=agent_runs.instance_id AND sessions.session_id=agent_runs.session_id LIMIT 1),''),'unknown') AS agentId,provider,model,channel,trigger,status,
    started_at AS startedAt,ended_at AS endedAt,duration_ms AS durationMs,error_category AS errorCategory FROM agent_runs WHERE run_id=? LIMIT 1`, id)
	if err != nil || len(rows) == 0 {
		if err == nil {
			err = sql.ErrNoRows
		}
		return nil, err
	}
	llm, err := queryMaps(ctx, r.db, `SELECT call_id AS callId,provider,model,status,started_at AS startedAt,ended_at AS endedAt,duration_ms AS durationMs,
    input_tokens AS inputTokens,output_tokens AS outputTokens,cost_usd AS costUsd,error_category AS errorCategory FROM llm_calls WHERE run_id=? ORDER BY started_at`, id)
	if err != nil {
		return nil, err
	}
	tools, err := queryMaps(ctx, r.db, `SELECT tool_call_id AS toolCallId,tool_name AS toolName,tool_source AS toolSource,status,started_at AS startedAt,
    ended_at AS endedAt,duration_ms AS durationMs,error_category AS errorCategory FROM tool_calls WHERE run_id=? ORDER BY started_at`, id)
	if err != nil {
		return nil, err
	}
	mcp, err := queryMaps(ctx, r.db, `SELECT call_id AS toolCallId,tool_name AS toolName,owner,status,started_at AS startedAt,ended_at AS endedAt,duration_ms AS durationMs,error_category AS errorCategory FROM mcp_calls WHERE run_id=? ORDER BY started_at`, id)
	if err != nil {
		return nil, err
	}
	rows[0]["llmCalls"], rows[0]["toolCalls"], rows[0]["mcpCalls"] = llm, tools, mcp
	return rows[0], nil
}

func (r *Repository) Status(ctx context.Context) (map[string]any, error) {
	instances, err := r.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, table := range []string{"events", "sessions", "agent_runs", "subagent_runs", "llm_calls", "tool_calls", "mcp_calls", "retry_events", "resource_samples"} {
		n, err := r.Count(ctx, table)
		if err != nil {
			return nil, err
		}
		counts[table] = n
	}
	var activeSessions, activeRuns int64
	_ = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE status='active'`).Scan(&activeSessions)
	_ = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs WHERE status='active'`).Scan(&activeRuns)
	var lastEventReceivedAt sql.NullString
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(received_at) FROM events`).Scan(&lastEventReceivedAt); err != nil {
		return nil, err
	}
	var eventQueueDepth float64
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(COALESCE(CAST(json_extract(payload_json,'$.queueDepth') AS REAL),0)),0) FROM events WHERE rowid IN (SELECT MAX(rowid) FROM events WHERE event_type='gateway.heartbeat' GROUP BY instance_id)`).Scan(&eventQueueDepth); err != nil {
		return nil, err
	}
	return map[string]any{
		"instances": instances, "counts": counts, "activeSessions": activeSessions, "activeRuns": activeRuns,
		"databaseBytes": r.DBSize(), "dbSizeBytes": r.DBSize(), "eventQueueDepth": eventQueueDepth,
		"lastEventReceivedAt": nullableString(lastEventReceivedAt),
	}, nil
}

func nullableString(value sql.NullString) any {
	if !value.Valid || value.String == "" {
		return nil
	}
	return value.String
}

func queryMaps(ctx context.Context, db *sql.DB, query string, args ...any) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, col := range cols {
			v := values[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			if col == "payload" {
				if raw, ok := v.(string); ok {
					var decoded any
					if json.Unmarshal([]byte(raw), &decoded) == nil {
						v = decoded
					}
				}
			}
			m[col] = v
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type MetricRow struct {
	Labels map[string]string
	Value  float64
}
type MetricsSnapshot struct {
	GatewayUp, Uptime, Restarts, SessionsActive, RunsActive                                                          []MetricRow
	Runs, LLM, LLMTokensInput, LLMTokensOutput, LLMCost, Tools, ToolErrors, Resources, Received, Dropped, QueueDepth []MetricRow
}

func (r *Repository) Metrics(ctx context.Context, nowUnix float64) (MetricsSnapshot, error) {
	var s MetricsSnapshot
	queries := []struct {
		dst       *[]MetricRow
		q         string
		labelCols []string
	}{
		{&s.GatewayUp, `SELECT instance_id,status,CASE WHEN status='up' THEN 1 ELSE 0 END FROM instances`, []string{"instance", "_status"}},
		{&s.Uptime, `SELECT instance_id,MAX(0,? - unixepoch(started_at)) FROM instances WHERE started_at IS NOT NULL`, []string{"instance"}},
		{&s.Restarts, `SELECT instance_id,MAX(COUNT(*)-1,0) FROM events WHERE event_type='gateway.started' GROUP BY instance_id`, []string{"instance"}},
		{&s.SessionsActive, `SELECT instance_id,COUNT(*) FROM sessions WHERE status='active' GROUP BY instance_id`, []string{"instance"}},
		{&s.RunsActive, `SELECT instance_id,COUNT(*) FROM agent_runs WHERE status='active' GROUP BY instance_id`, []string{"instance"}},
		{&s.Runs, `SELECT instance_id,status,COUNT(*) FROM agent_runs GROUP BY instance_id,status`, []string{"instance", "status"}},
		{&s.LLM, `SELECT instance_id,COALESCE(provider,'unknown'),COALESCE(model,'unknown'),status,COUNT(*) FROM llm_calls GROUP BY instance_id,provider,model,status`, []string{"instance", "provider", "model", "status"}},
		{&s.LLMTokensInput, `SELECT instance_id,COALESCE(provider,'unknown'),COALESCE(model,'unknown'),SUM(input_tokens) FROM llm_calls GROUP BY instance_id,provider,model`, []string{"instance", "provider", "model"}},
		{&s.LLMTokensOutput, `SELECT instance_id,COALESCE(provider,'unknown'),COALESCE(model,'unknown'),SUM(output_tokens) FROM llm_calls GROUP BY instance_id,provider,model`, []string{"instance", "provider", "model"}},
		{&s.LLMCost, `SELECT instance_id,COALESCE(provider,'unknown'),COALESCE(model,'unknown'),SUM(cost_usd) FROM llm_calls GROUP BY instance_id,provider,model`, []string{"instance", "provider", "model"}},
		{&s.Tools, `SELECT instance_id,COALESCE(tool_name,'unknown'),status,COUNT(*) FROM tool_calls GROUP BY instance_id,tool_name,status`, []string{"instance", "tool", "status"}},
		{&s.ToolErrors, `SELECT instance_id,COALESCE(tool_name,'unknown'),COALESCE(error_category,'unknown'),COUNT(*) FROM tool_calls WHERE status='failed' GROUP BY instance_id,tool_name,error_category`, []string{"instance", "tool", "reason"}},
		{&s.Received, `SELECT instance_id,event_type,COUNT(*) FROM events GROUP BY instance_id,event_type`, []string{"instance", "event_type"}},
		{&s.Dropped, `SELECT instance_id,COALESCE(json_extract(payload_json,'$.reason'),'unknown'),SUM(COALESCE(json_extract(payload_json,'$.count'),1)) FROM events WHERE event_type='monitor.events_dropped' GROUP BY instance_id,2`, []string{"instance", "reason"}},
		{&s.QueueDepth, `SELECT instance_id,COALESCE(CAST(json_extract(payload_json,'$.queueDepth') AS REAL),0) FROM events WHERE rowid IN (SELECT MAX(rowid) FROM events WHERE event_type='gateway.heartbeat' GROUP BY instance_id)`, []string{"instance"}},
	}
	for _, item := range queries {
		args := []any{}
		if strings.Contains(item.q, "unixepoch") {
			args = append(args, nowUnix)
		}
		rows, err := metricRows(ctx, r.db, item.q, item.labelCols, args...)
		if err != nil {
			return s, err
		}
		*item.dst = rows
	}
	res, err := queryMaps(ctx, r.db, `SELECT r.instance_id AS instanceId,r.cpu_seconds_total AS cpuSecondsTotal,r.resident_memory_bytes AS residentMemoryBytes,
	    r.virtual_memory_bytes AS virtualMemoryBytes,r.threads,r.open_fds AS openFds,r.read_bytes AS readBytesTotal,r.write_bytes AS writeBytesTotal,
	    r.disk_total_bytes AS diskTotalBytes,r.disk_available_bytes AS diskAvailableBytes
    FROM resource_samples r JOIN (SELECT instance_id,MAX(sampled_at) t FROM resource_samples GROUP BY instance_id) x ON x.instance_id=r.instance_id AND x.t=r.sampled_at`)
	if err != nil {
		return s, err
	}
	for _, m := range res {
		instance := fmt.Sprint(m["instanceId"])
		for _, k := range []string{"cpuSecondsTotal", "residentMemoryBytes", "virtualMemoryBytes", "threads", "openFds", "readBytesTotal", "writeBytesTotal", "diskTotalBytes", "diskAvailableBytes"} {
			if v, ok := toFloat(m[k]); ok {
				s.Resources = append(s.Resources, MetricRow{Labels: map[string]string{"instance": instance, "kind": k}, Value: v})
			}
		}
	}
	return s, nil
}

func metricRows(ctx context.Context, db *sql.DB, q string, labels []string, args ...any) ([]MetricRow, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetricRow{}
	for rows.Next() {
		vals := make([]any, len(labels)+1)
		ptrs := make([]any, len(vals))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]string{}
		for i, k := range labels {
			if !strings.HasPrefix(k, "_") {
				m[k] = fmt.Sprint(vals[i])
			}
		}
		v, _ := toFloat(vals[len(vals)-1])
		out = append(out, MetricRow{Labels: m, Value: v})
	}
	return out, rows.Err()
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case []byte:
		var f float64
		_, e := fmt.Sscan(string(n), &f)
		return f, e == nil
	case string:
		var f float64
		_, e := fmt.Sscan(n, &f)
		return f, e == nil
	}
	return 0, false
}
