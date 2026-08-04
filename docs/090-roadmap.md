# Roadmap

## Phase 0 — Architecture and Contracts ✅

- ✅ Runtime and ownership model;
- ✅ Versioned event Schema and compatibility negotiation;
- ✅ Metrics and API contracts;
- ✅ Privacy threat model and cardinality policy.

Exit: valid Schema examples, linked documents, and reviewed compatibility map.

**Completion: 100%**

---

## Phase 1 — Local MVP ✅

- ✅ OpenClaw monitor plugin;
- ✅ Go daemon and Unix-socket ingestion;
- ✅ Idempotent SQLite projections;
- ✅ macOS resource sampling;
- ✅ `/metrics`, REST, SSE, built-in UI, and baseline Grafana dashboard;
- ✅ LaunchAgent installer and end-to-end smoke test.

Exit: a real OpenClaw run appears in SQLite/API without content capture and
daemon outage does not interrupt the run.

**Completion: 100%**

---

## Phase 2 — Product Dashboard ✅

### Completed

- ✅ Dark-themed real-time dashboard with KPI cards (Gateway status, sessions, runs, tokens)
- ✅ Resource monitoring charts (CPU%, memory RSS, threads, FDs, IO)
- ✅ LLM activity panel (requests by model, token usage, latency P50/P95/P99)
- ✅ Tool call statistics with success/failure breakdown
- ✅ Agent run timeline (Gantt-style)
- ✅ Real-time event stream + error browser with filters
- ✅ SSE live updates + 15s polling refresh
- ✅ Collapsible sections with localStorage persistence
- ✅ Time range selector (1h/6h/24h/7d) with localStorage
- ✅ Standalone web server with reverse proxy (`cmd/observatory-web`)
- ✅ Vite build pipeline with build ID injection
- ✅ Schema migration framework (v1 → v5, agent_id, aggregation indexes, disk capacity)
- ✅ Agent ID extraction from session key in forwarder
- ✅ API version 3 with compatibility banner
- ✅ Mobile responsive layout, pinch-zoom disabled
- ✅ Chinese documentation (README.zh.md + docs/zh/*)
- ✅ Cloudflare Tunnel deployment + Access protection

### Completed in v0.3

- ✅ Session waterfall for LLM, Tool, MCP, and Subagent activity
- ✅ Interactive historical charts driven by one time range
- ✅ Subagent and MCP API/UI exposure
- ✅ Dashboard JSON, drag ordering, thresholds, and themes

**Completion: 100%**

---

## Phase 3 — v0.3 Observability Enhancement ✅

> No backward compatibility constraints — API can break freely until first release.

### 3.1 Data Exposure

- ✅ `/agents/stats` — per-agent_id aggregation (runs, tokens, tools, duration, error rate)
- ✅ Session detail waterfall for LLM, Tool, MCP, and Subagent calls
- ✅ Expose `subagent_runs` and `mcp_calls` via API and UI

### 3.2 Time Series Aggregation

- ✅ `/timeseries` — time-bucket aggregation (1m/5m/1h/1d)
- ✅ Time range selector (1h/6h/24h/7d/30d) drives all charts
- ✅ Historical trend charts replace "latest N rows" mode
- ✅ Stacked area charts for resource and per-model token trends

### 3.3 Rich Chart Types

- ✅ Multi-metric combo charts for LLM volume, latency, and error rate
- ✅ Time × Agent activity heatmap
- ✅ Token and tool distribution doughnut charts
- ✅ Per-LLM-call latency-vs-token scatter
- ✅ Cross-agent runs/tokens/errors comparison
- ✅ LLM/Tool/MCP/Subagent session waterfall
- ✅ Error aggregation by `error_category`
- ✅ Cost analysis by model, agent, and time when providers report cost

### 3.4 Configuration & Customization

- ✅ Dashboard JSON config (module visibility, ordering, refresh interval)
- ✅ Drag-and-drop module reordering
- ✅ Configurable threshold alert highlighting
- ✅ Dark/light theme toggle

### 3.5 Frontend Engineering

- ✅ Migrate single HTML to Vite ES-module components
- ✅ Tree-shaken Chart.js bundle with DOM heatmap/waterfall specializations
- ✅ Responsive tablet/mobile layout
- ✅ 72 KiB gzipped JavaScript bundle with no external runtime CDN/dependency

**Constraints:** No compat burden. SQLite aggregation performance matters (leverage indexes + `strftime` bucketing). Frontend bundle < 200KB gzipped. Zero external runtime dependencies.

---

## Phase 4 — v0.4 Operational Hardening ✅

### 4.1 Data Retention

- ✅ Configurable retention policy (`--retention-events-days`, `--retention-samples-days`, `--retention-all-days`)
- ✅ Background cleaner runs every 6 hours, purges old events/resource_samples
- ✅ WAL mode + `PRAGMA synchronous=NORMAL` confirmed

### 4.2 Frontend Incremental Updates

- ✅ SSE-driven incremental chart updates (no full re-render/destroyCharts flicker)
- ✅ `updateChartData`/`updateDoughnut`/`updateScatter` for in-place Chart.js updates
- ✅ KPI cards and tables patched in-place via `updateNonChartDOM`

### 4.3 CI/CD

- ✅ GitHub Actions: Go test + vet with race detector, Vite build verification
- ✅ Concurrency group cancellation for rapid iteration

### 4.4 Cursor Pagination

- ✅ Cursor-based pagination for sessions, runs, events endpoints
- ✅ Opaque base64-encoded cursor using composite key (timestamp + id)
- ✅ `LIMIT+1` fetch strategy for has-more detection

### 4.5 URL State Sync

- ✅ Time range, instance filter, agent filter, and session written to URL query params
- ✅ Browser back/forward via popstate listener
- ✅ Shareable dashboard URLs

### 4.6 Cost Analysis Enhancement

- ✅ `/api/v1/cost/trends` — daily/weekly/monthly cost breakdown by model
- ✅ `/api/v1/cost/summary` — aggregate with day/week/month rolls
- ✅ Budget alert UI with configurable `costBudgetUsd` threshold
- ✅ Stacked bar chart for cost trends per model

---

## Phase 5 — v0.5 Stability & Resilience 🚧

> Focus: prevent data loss during daemon/plugin restart, recover gracefully from
> network hiccups, and harden against edge-case crashes.

### 5.1 Plugin → Daemon Reconnection

**Current gaps:**
- Forwarder connects to daemon via Unix socket; flush failure uses exponential backoff (250ms → 5s) but has no connection health check — it POSTs blindly after backoff
- `post()` timeouts are hardcoded at 250ms (force) and 1500ms; under daemon load this may cause false negatives
- When the queue fills during daemon outage, events are dropped (`queue_full`); critical events (`gateway.started/stopped`, `session.completed`) are not protected

**Plan:**
- [x] Probe socket connectivity (`fs.access` or `connect` check) before flush to avoid wasteful POSTs
- [x] Raise exponential backoff ceiling to 30s with jitter; fast-recover when daemon comes back
- [x] Critical event protection: when queue is full, drop lowest-priority events first; never drop `critical` priority events (`gateway.started/stopped`, `session.completed`)
- [x] Report queue depth via heartbeat events; daemon logs backpressure warnings based on `queueDepth`
- [x] Add configurable `queueCapacity` plugin option to allow tuning the queue limit

### 5.2 Daemon Crash Prevention & Recovery

**Current gaps:**
- Daemon crash relies on LaunchAgent restart; no crash diagnostics or self-healing
- SQLite WAL mode is safe, but abnormal exit may leave stale `-wal`/`-shm` sidecar files
- `main.go` exits on any HTTP server error via `errCh`; no recovery attempt
- Resource sampling (`ps`/`lsof`) failures are silently ignored — cannot distinguish daemon issues from OS problems

**Plan:**
- [ ] On startup, detect and clean up stale SQLite lock files (`.db-wal`, `.db-shm`)
- [x] HTTP server fatal error recovery: retry listen on transient errors (exclude bind conflicts)
- [x] Runtime crash output: write unhandled panic/fatal-error dumps to `logs/` with goroutine stacks
- [x] Enhanced health check: `/ready` validates SQLite writability and recent event latency
- [x] Process sampling error tracking: consecutive failure counter; after N failures, emit `gateway.crashed`

### 5.3 Data Write Hardening

**Current gaps:**
- `InsertEvents` runs insert + reduce in a single transaction; large batches hold the lock long
- `PRAGMA busy_timeout=5000` is only 5s; concurrent queries may hit SQLITE_BUSY
- `SetMaxOpenConns(1)` is required for SQLite single-writer but lacks connection health checks
- Retention purge uses row-by-row DELETE inside a transaction; slow for large datasets
- No write audit trail

**Plan:**
- [x] Raise `busy_timeout` to 30s to accommodate long-running queries
- [x] Batch splitting: when a single batch exceeds 50 events, split into smaller transactions to reduce lock hold time
- [x] Retention DELETE: use chunked deletion (`WHERE rowid IN (SELECT rowid FROM ... LIMIT 1000)`) instead of full-table scan
- [ ] Periodic VACUUM after retention job (off-peak, throttled)
- [x] Write performance metrics: expose `INSERT OR IGNORE` duration, reduce duration, commit duration to `/metrics`
- [ ] Optional write audit log (`--audit-log` flag): record accepted/duplicates/errors per batch

### 5.4 Frontend Offline Recovery

**Current gaps:**
- SSE `onerror` simply closes + reconnects after 5s; no distinction between network error, server 503, or clean shutdown
- `loadDashboard` uses bare `fetch()` with no timeout, retry, or AbortController
- When daemon is unreachable, the page shows blank/error with no friendly offline state
- Background auto-refresh retries at fixed intervals after failure with no backoff

**Plan:**
- [x] SSE reconnect with exponential backoff (1s → 2s → 4s → ... → 30s); reset on successful connect
- [x] SSE `onerror` checks `readyState`: CLOSED = reconnect, CONNECTING = wait
- [x] `fetch()` with AbortController + 10s timeout; retry once on timeout
- [x] Offline banner: show "Reconnecting..." status bar when daemon is unreachable; auto-dismiss on recovery
- [x] Background refresh backoff: double interval after 3 consecutive failures, cap at 60s
- [x] `navigator.onLine` listener: pause refresh when offline, trigger immediately on online
- [x] Data caching: keep last successful dashboard data on fetch failure (show "data may be stale" indicator)

### 5.5 Performance Optimization

**Current gaps:**
- `/agents/stats` uses 3 CTEs + multiple JOINs; may be slow with 30d range and large datasets
- `/timeseries` aggregates per bucket using `strftime`; up to 2000 buckets means 2000 `strftime` calls
- `agentStats` query `tool_events` CTE is a UNION ALL without pushdown filters — full table scan risk
- Resource sampling spawns `ps` + `lsof` subprocesses every 5s; `lsof` is slow on macOS
- Frontend sends 12 parallel fetch requests per dashboard load

**Plan:**
- [ ] `timeseries` optimization: precompute bucket boundaries, replace `strftime` with `CASE WHEN` expressions
- [x] `agent_stats`: push time-range filters into CTEs to reduce JOIN intermediate rows
- [ ] Replace `lsof` with faster FD counting (macOS: `proc_info` syscall; Linux: `/proc/<pid>/fd` readdir)
- [x] Add statement timeout for long-running queries (SQLite `busy_timeout` does not cover this)
- [x] Frontend: merge dashboard APIs into composite `/api/v1/dashboard` endpoint (single round-trip for KPIs + chart data)
- [x] SQLite query plan analysis: run `EXPLAIN QUERY PLAN` on critical queries and establish baselines

### 5.6 Monitoring & Alerting

**Current gaps:**
- `/metrics` exposes Prometheus format but has no built-in alert rules
- No daemon self-health metrics (event latency, queue backlog, query latency)
- Logging is `slog` to stderr only; no structured log file rotation

**Plan:**
- [x] New Prometheus metrics: `openclaw_monitor_insert_duration_seconds`, `openclaw_monitor_queue_depth`, `openclaw_monitor_query_duration_seconds`
- [x] Log rotation: write `slog` output to `logs/observatoryd-YYYY-MM-DD.log`, daily rotation, 7-day retention
- [x] Built-in alert thresholds: event queue > 80% capacity → WARN, insert latency > 1s → WARN
- [x] `status` API returns `eventQueueDepth`, `lastEventReceivedAt`, `dbSizeBytes`

**Exit criteria:**
- Zero critical event loss after daemon crash + restart
- Frontend reconnects and displays data within 5s of network recovery
- 30d `agents/stats` query completes in < 500ms
- Database write metrics exposed via `/metrics`


---

## Phase 6 — Advanced Observability

- Metadata-only session replay;
- OpenTelemetry trace export and correlation;
- Loki/Tempo integration;
- Authenticated remote mode;
- Multi-instance discovery and federation;
- Opt-in encrypted content capture only after a separate security design.

Exit: remote/multi-instance operation has authentication, authorization,
encryption, audit logging, and documented upgrade/rollback behavior.

---

## OpenClaw interfaces still requiring verification

- Behavior on versions older/newer than `2026.6.11`;
- Whether all external harnesses emit canonical `tool.execution.*` events;
- Stable MCP owner/server identity beyond authoritative `toolSource: "mcp"`;
- Exact terminal behavior for hard process kill and host sleep/wake;
- Provider-specific cost/token completeness.
