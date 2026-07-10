package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zylcold/openclaw-observatory/internal/event"
	_ "modernc.org/sqlite"
)

type Repository struct {
	db   *sql.DB
	path string
}

type InsertResult struct {
	Inserted   []event.Event `json:"-"`
	Accepted   int           `json:"accepted"`
	Duplicates int           `json:"duplicates"`
}

type ProcessRef struct {
	InstanceID string `json:"instanceId"`
	ProcessID  int    `json:"processId"`
}

func Open(path string) (*Repository, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schemaV1); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, ?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		db.Close()
		return nil, err
	}
	return &Repository{db: db, path: path}, nil
}

func (r *Repository) Close() error { return r.db.Close() }

func (r *Repository) Ping(ctx context.Context) error { return r.db.PingContext(ctx) }

func (r *Repository) InsertEvents(ctx context.Context, events []event.Event) (InsertResult, error) {
	result := InsertResult{Accepted: len(events)}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	for _, e := range events {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO events
      (event_id,schema_version,event_type,occurred_at,instance_id,producer_id,process_id,sequence,source,payload_json,received_at)
      VALUES(?,?,?,?,?,?,?,?,?,?,?)`, e.EventID, e.SchemaVersion, e.EventType, timestamp(e.OccurredAt), e.InstanceID,
			e.ProducerID, e.ProcessID, e.Sequence, e.Source, string(e.Payload), timestamp(time.Now()))
		if err != nil {
			return result, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			result.Duplicates++
			continue
		}
		if err := reduce(ctx, tx, e); err != nil {
			return result, fmt.Errorf("reduce %s: %w", e.EventType, err)
		}
		result.Inserted = append(result.Inserted, e)
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func reduce(ctx context.Context, tx *sql.Tx, e event.Event) error {
	p := event.PayloadMap(e.Payload)
	t := timestamp(e.OccurredAt)
	switch e.EventType {
	case "gateway.started":
		pid := any(nil)
		if e.ProcessID != nil {
			pid = *e.ProcessID
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO instances(instance_id,plugin_version,openclaw_version,process_id,status,started_at,last_seen_at,stopped_at)
      VALUES(?,?,?,?, 'up',?,?,NULL)
      ON CONFLICT(instance_id) DO UPDATE SET plugin_version=excluded.plugin_version,openclaw_version=excluded.openclaw_version,
      process_id=excluded.process_id,status='up',started_at=excluded.started_at,last_seen_at=excluded.last_seen_at,stopped_at=NULL`,
			e.InstanceID, event.String(p, "pluginVersion"), event.String(p, "openclawVersion"), pid, t, t)
		return err
	case "gateway.heartbeat":
		_, err := tx.ExecContext(ctx, `INSERT INTO instances(instance_id,process_id,status,last_seen_at) VALUES(?,?,'up',?)
      ON CONFLICT(instance_id) DO UPDATE SET process_id=COALESCE(excluded.process_id,process_id),status='up',last_seen_at=excluded.last_seen_at`, e.InstanceID, e.ProcessID, t)
		return err
	case "gateway.stopped", "gateway.crashed":
		status := "stopped"
		if e.EventType == "gateway.crashed" {
			status = "crashed"
		}
		_, err := tx.ExecContext(ctx, `UPDATE instances SET status=?,last_seen_at=?,stopped_at=?
      WHERE instance_id=? AND (started_at IS NULL OR started_at<=?)`, status, t, t, e.InstanceID, t)
		return err
	case "session.started", "session.completed", "session.failed":
		id := event.String(p, "sessionId")
		if id == "" {
			return nil
		}
		status := statusFor(e.EventType)
		if e.EventType == "session.started" {
			_, err := tx.ExecContext(ctx, `INSERT INTO sessions(instance_id,session_id,session_key_hash,status,started_at)
          VALUES(?,?,?,?,?) ON CONFLICT(instance_id,session_id) DO UPDATE SET session_key_hash=COALESCE(NULLIF(excluded.session_key_hash,''),session_key_hash),status='active',started_at=COALESCE(started_at,excluded.started_at)`,
				e.InstanceID, id, event.String(p, "sessionKeyHash"), status, t)
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO sessions(instance_id,session_id,session_key_hash,status,ended_at,end_reason,message_count)
        VALUES(?,?,?,?,?,?,?) ON CONFLICT(instance_id,session_id) DO UPDATE SET status=excluded.status,ended_at=excluded.ended_at,end_reason=excluded.end_reason,message_count=excluded.message_count`,
			e.InstanceID, id, event.String(p, "sessionKeyHash"), status, t, event.String(p, "reason"), int64(event.Float(p, "messageCount")))
		return err
	case "agent.started", "agent.completed", "agent.failed":
		id := event.String(p, "runId")
		if id == "" {
			return nil
		}
		status := statusFor(e.EventType)
		_, err := tx.ExecContext(ctx, `INSERT INTO agent_runs(instance_id,run_id,session_id,provider,model,channel,trigger,status,started_at,ended_at,duration_ms,error_category)
        VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(instance_id,run_id) DO UPDATE SET
        session_id=COALESCE(NULLIF(excluded.session_id,''),session_id),provider=COALESCE(NULLIF(excluded.provider,''),provider),
        model=COALESCE(NULLIF(excluded.model,''),model),channel=COALESCE(NULLIF(excluded.channel,''),channel),trigger=COALESCE(NULLIF(excluded.trigger,''),trigger),
        status=CASE WHEN excluded.status='active' AND status!='unknown' THEN status ELSE excluded.status END,
        started_at=COALESCE(started_at,excluded.started_at),ended_at=COALESCE(excluded.ended_at,ended_at),duration_ms=COALESCE(excluded.duration_ms,duration_ms),error_category=COALESCE(NULLIF(excluded.error_category,''),error_category)`,
			e.InstanceID, id, event.String(p, "sessionId"), event.String(p, "provider"), event.String(p, "model"), event.String(p, "channel"), event.String(p, "trigger"), status,
			nullTime(e.EventType == "agent.started", t), nullTime(e.EventType != "agent.started", t), nullFloat(p, "durationMs"), event.String(p, "errorCategory"))
		return err
	case "llm.started", "llm.completed", "llm.failed":
		id := event.String(p, "callId")
		if id == "" {
			return nil
		}
		status := statusFor(e.EventType)
		_, err := tx.ExecContext(ctx, `INSERT INTO llm_calls(instance_id,call_id,run_id,session_id,provider,model,status,started_at,ended_at,duration_ms,error_category,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,cost_usd)
        VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(instance_id,call_id) DO UPDATE SET
        run_id=COALESCE(NULLIF(excluded.run_id,''),run_id),session_id=COALESCE(NULLIF(excluded.session_id,''),session_id),provider=COALESCE(NULLIF(excluded.provider,''),provider),model=COALESCE(NULLIF(excluded.model,''),model),
        status=CASE WHEN excluded.status='active' AND status!='unknown' THEN status ELSE excluded.status END,started_at=COALESCE(started_at,excluded.started_at),ended_at=COALESCE(excluded.ended_at,ended_at),
        duration_ms=COALESCE(excluded.duration_ms,duration_ms),error_category=COALESCE(NULLIF(excluded.error_category,''),error_category),
        input_tokens=MAX(input_tokens,excluded.input_tokens),output_tokens=MAX(output_tokens,excluded.output_tokens),cache_read_tokens=MAX(cache_read_tokens,excluded.cache_read_tokens),
        cache_write_tokens=MAX(cache_write_tokens,excluded.cache_write_tokens),cost_usd=MAX(cost_usd,excluded.cost_usd)`,
			e.InstanceID, id, event.String(p, "runId"), event.String(p, "sessionId"), event.String(p, "provider"), event.String(p, "model"), status,
			nullTime(e.EventType == "llm.started", t), nullTime(e.EventType != "llm.started", t), nullFloat(p, "durationMs"), event.String(p, "errorCategory"),
			event.Float(p, "inputTokens"), event.Float(p, "outputTokens"), event.Float(p, "cacheReadTokens"), event.Float(p, "cacheWriteTokens"), event.Float(p, "costUsd"))
		return err
	case "tool.started", "tool.completed", "tool.failed":
		return reduceTool(ctx, tx, e, p, t, false)
	case "mcp.started", "mcp.completed", "mcp.failed":
		return reduceTool(ctx, tx, e, p, t, true)
	case "subagent.started", "subagent.completed", "subagent.failed":
		id := event.String(p, "subagentId")
		if id == "" {
			id = event.String(p, "runId")
		}
		if id == "" {
			return nil
		}
		status := statusFor(e.EventType)
		_, err := tx.ExecContext(ctx, `INSERT INTO subagent_runs(instance_id,subagent_id,parent_run_id,child_session_hash,agent_id,mode,provider,model,status,started_at,ended_at,outcome)
        VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(instance_id,subagent_id) DO UPDATE SET parent_run_id=COALESCE(NULLIF(excluded.parent_run_id,''),parent_run_id),
        child_session_hash=COALESCE(NULLIF(excluded.child_session_hash,''),child_session_hash),status=excluded.status,started_at=COALESCE(started_at,excluded.started_at),ended_at=COALESCE(excluded.ended_at,ended_at),outcome=COALESCE(NULLIF(excluded.outcome,''),outcome)`,
			e.InstanceID, id, event.String(p, "parentRunId"), event.String(p, "childSessionHash"), event.String(p, "agentId"), event.String(p, "mode"), event.String(p, "provider"), event.String(p, "model"), status,
			nullTime(e.EventType == "subagent.started", t), nullTime(e.EventType != "subagent.started", t), event.String(p, "outcome"))
		return err
	case "resource.sampled":
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO resource_samples(event_id,instance_id,process_id,sampled_at,cpu_seconds_total,resident_memory_bytes,virtual_memory_bytes,threads,open_fds,read_bytes,write_bytes)
        VALUES(?,?,?,?,?,?,?,?,?,?,?)`, e.EventID, e.InstanceID, e.ProcessID, t, event.Float(p, "cpuSecondsTotal"), int64(event.Float(p, "residentMemoryBytes")),
			int64(event.Float(p, "virtualMemoryBytes")), int64(event.Float(p, "threads")), int64(event.Float(p, "openFds")), int64(event.Float(p, "readBytesTotal")), int64(event.Float(p, "writeBytesTotal")))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE instances SET last_seen_at=? WHERE instance_id=? AND (last_seen_at IS NULL OR last_seen_at<?)`, t, e.InstanceID, t)
		return err
	}
	return nil
}

func reduceTool(ctx context.Context, tx *sql.Tx, e event.Event, p map[string]any, t string, mcp bool) error {
	id := event.String(p, "toolCallId")
	if id == "" {
		return nil
	}
	status := statusFor(e.EventType)
	started := strings.HasSuffix(e.EventType, ".started")
	if mcp {
		_, err := tx.ExecContext(ctx, `INSERT INTO mcp_calls(instance_id,call_id,run_id,session_id,tool_name,owner,status,started_at,ended_at,duration_ms,error_category)
      VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(instance_id,call_id) DO UPDATE SET run_id=COALESCE(NULLIF(excluded.run_id,''),run_id),session_id=COALESCE(NULLIF(excluded.session_id,''),session_id),
      tool_name=COALESCE(NULLIF(excluded.tool_name,''),tool_name),owner=COALESCE(NULLIF(excluded.owner,''),owner),status=excluded.status,started_at=COALESCE(started_at,excluded.started_at),
      ended_at=COALESCE(excluded.ended_at,ended_at),duration_ms=COALESCE(excluded.duration_ms,duration_ms),error_category=COALESCE(NULLIF(excluded.error_category,''),error_category)`,
			e.InstanceID, id, event.String(p, "runId"), event.String(p, "sessionId"), event.String(p, "toolName"), event.String(p, "toolOwner"), status,
			nullTime(started, t), nullTime(!started, t), nullFloat(p, "durationMs"), event.String(p, "errorCategory"))
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO tool_calls(instance_id,tool_call_id,run_id,session_id,tool_name,tool_source,tool_owner,status,started_at,ended_at,duration_ms,error_category)
    VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(instance_id,tool_call_id) DO UPDATE SET run_id=COALESCE(NULLIF(excluded.run_id,''),run_id),session_id=COALESCE(NULLIF(excluded.session_id,''),session_id),
    tool_name=COALESCE(NULLIF(excluded.tool_name,''),tool_name),tool_source=COALESCE(NULLIF(excluded.tool_source,''),tool_source),tool_owner=COALESCE(NULLIF(excluded.tool_owner,''),tool_owner),
    status=excluded.status,started_at=COALESCE(started_at,excluded.started_at),ended_at=COALESCE(excluded.ended_at,ended_at),duration_ms=COALESCE(excluded.duration_ms,duration_ms),error_category=COALESCE(NULLIF(excluded.error_category,''),error_category)`,
		e.InstanceID, id, event.String(p, "runId"), event.String(p, "sessionId"), event.String(p, "toolName"), event.String(p, "toolSource"), event.String(p, "toolOwner"), status,
		nullTime(started, t), nullTime(!started, t), nullFloat(p, "durationMs"), event.String(p, "errorCategory"))
	return err
}

func statusFor(eventType string) string {
	if strings.HasSuffix(eventType, ".started") {
		return "active"
	}
	if strings.HasSuffix(eventType, ".failed") {
		return "failed"
	}
	return "completed"
}

func nullTime(ok bool, value string) any {
	if ok {
		return value
	}
	return nil
}
func nullFloat(m map[string]any, key string) any {
	if _, ok := m[key]; !ok {
		return nil
	}
	return event.Float(m, key)
}
func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func (r *Repository) ActiveProcesses(ctx context.Context) ([]ProcessRef, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT instance_id,process_id FROM instances WHERE status='up' AND process_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProcessRef
	for rows.Next() {
		var v ProcessRef
		if err := rows.Scan(&v.InstanceID, &v.ProcessID); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) DBSize() int64 {
	st, err := os.Stat(r.path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func (r *Repository) Count(ctx context.Context, table string) (int64, error) {
	allowed := map[string]bool{"events": true, "sessions": true, "agent_runs": true, "llm_calls": true, "tool_calls": true, "mcp_calls": true, "resource_samples": true}
	if !allowed[table] {
		return 0, errors.New("invalid table")
	}
	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n)
	return n, err
}

func JSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
