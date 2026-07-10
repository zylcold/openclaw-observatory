package storage

const schemaV1 = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS instances (
  instance_id TEXT PRIMARY KEY,
  plugin_version TEXT,
  openclaw_version TEXT,
  process_id INTEGER,
  status TEXT NOT NULL DEFAULT 'unknown',
  started_at TEXT,
  last_seen_at TEXT,
  stopped_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_instances_status_seen ON instances(status, last_seen_at);

CREATE TABLE IF NOT EXISTS sessions (
  instance_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  session_key_hash TEXT,
  status TEXT NOT NULL DEFAULT 'unknown',
  started_at TEXT,
  ended_at TEXT,
  end_reason TEXT,
  message_count INTEGER,
  PRIMARY KEY(instance_id, session_id)
);
CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(instance_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(instance_id, status);

CREATE TABLE IF NOT EXISTS agent_runs (
  instance_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  session_id TEXT,
  provider TEXT,
  model TEXT,
  channel TEXT,
  trigger TEXT,
  status TEXT NOT NULL DEFAULT 'unknown',
  started_at TEXT,
  ended_at TEXT,
  duration_ms REAL,
  error_category TEXT,
  PRIMARY KEY(instance_id, run_id)
);
CREATE INDEX IF NOT EXISTS idx_runs_session ON agent_runs(instance_id, session_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_started ON agent_runs(instance_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_status ON agent_runs(instance_id, status);

CREATE TABLE IF NOT EXISTS llm_calls (
  instance_id TEXT NOT NULL,
  call_id TEXT NOT NULL,
  run_id TEXT,
  session_id TEXT,
  provider TEXT,
  model TEXT,
  status TEXT NOT NULL DEFAULT 'unknown',
  started_at TEXT,
  ended_at TEXT,
  duration_ms REAL,
  error_category TEXT,
  input_tokens REAL NOT NULL DEFAULT 0,
  output_tokens REAL NOT NULL DEFAULT 0,
  cache_read_tokens REAL NOT NULL DEFAULT 0,
  cache_write_tokens REAL NOT NULL DEFAULT 0,
  cost_usd REAL NOT NULL DEFAULT 0,
  PRIMARY KEY(instance_id, call_id)
);
CREATE INDEX IF NOT EXISTS idx_llm_run ON llm_calls(instance_id, run_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_model ON llm_calls(instance_id, provider, model, started_at DESC);

CREATE TABLE IF NOT EXISTS tool_calls (
  instance_id TEXT NOT NULL,
  tool_call_id TEXT NOT NULL,
  run_id TEXT,
  session_id TEXT,
  tool_name TEXT,
  tool_source TEXT,
  tool_owner TEXT,
  status TEXT NOT NULL DEFAULT 'unknown',
  started_at TEXT,
  ended_at TEXT,
  duration_ms REAL,
  error_category TEXT,
  PRIMARY KEY(instance_id, tool_call_id)
);
CREATE INDEX IF NOT EXISTS idx_tools_run ON tool_calls(instance_id, run_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_tools_name ON tool_calls(instance_id, tool_name, status, started_at DESC);

CREATE TABLE IF NOT EXISTS mcp_calls (
  instance_id TEXT NOT NULL,
  call_id TEXT NOT NULL,
  run_id TEXT,
  session_id TEXT,
  tool_name TEXT,
  owner TEXT,
  status TEXT NOT NULL DEFAULT 'unknown',
  started_at TEXT,
  ended_at TEXT,
  duration_ms REAL,
  error_category TEXT,
  PRIMARY KEY(instance_id, call_id)
);
CREATE INDEX IF NOT EXISTS idx_mcp_run ON mcp_calls(instance_id, run_id, started_at DESC);

CREATE TABLE IF NOT EXISTS subagent_runs (
  instance_id TEXT NOT NULL,
  subagent_id TEXT NOT NULL,
  parent_run_id TEXT,
  child_session_hash TEXT,
  agent_id TEXT,
  mode TEXT,
  provider TEXT,
  model TEXT,
  status TEXT NOT NULL DEFAULT 'unknown',
  started_at TEXT,
  ended_at TEXT,
  outcome TEXT,
  PRIMARY KEY(instance_id, subagent_id)
);
CREATE INDEX IF NOT EXISTS idx_subagent_parent ON subagent_runs(instance_id, parent_run_id, started_at DESC);

CREATE TABLE IF NOT EXISTS resource_samples (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT NOT NULL UNIQUE,
  instance_id TEXT NOT NULL,
  process_id INTEGER,
  sampled_at TEXT NOT NULL,
  cpu_seconds_total REAL,
  resident_memory_bytes INTEGER,
  virtual_memory_bytes INTEGER,
  threads INTEGER,
  open_fds INTEGER,
  read_bytes INTEGER,
  write_bytes INTEGER
);
CREATE INDEX IF NOT EXISTS idx_resources_instance_time ON resource_samples(instance_id, sampled_at DESC);

CREATE TABLE IF NOT EXISTS events (
  event_id TEXT PRIMARY KEY,
  schema_version INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  producer_id TEXT NOT NULL,
  process_id INTEGER,
  sequence INTEGER NOT NULL,
  source TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  received_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_instance_time ON events(instance_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_type_time ON events(event_type, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_producer_seq ON events(producer_id, sequence);

CREATE TABLE IF NOT EXISTS daily_stats (
  day TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  dimension_type TEXT NOT NULL,
  dimension_value TEXT NOT NULL,
  stats_json TEXT NOT NULL,
  PRIMARY KEY(day, instance_id, dimension_type, dimension_value)
);
`
