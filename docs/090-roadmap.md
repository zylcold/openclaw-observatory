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

## Phase 2 — Product Dashboard (In Progress)

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
- ✅ Schema migration framework (v1 → v3, agent_id column, indexes)
- ✅ Agent ID extraction from session key in forwarder
- ✅ API version 2 with compatibility banner
- ✅ Mobile responsive layout, pinch-zoom disabled
- ✅ Chinese documentation (README.zh.md + docs/zh/*)
- ✅ Cloudflare Tunnel deployment + Access protection

### Remaining

- ⬜ Session/run detail pages (waterfall view of LLM + Tool calls within a session)
- ⬜ Interactive resource charts tied to time range selection
- ⬜ Retention controls and database maintenance UX
- ⬜ Time range selector actually drives all chart data windows
- ⬜ Subagent and MCP call exposure (tables exist, no API/UI yet)

**Completion: ~70%**

---

## Phase 3 — v0.3 Observability Enhancement

> No backward compatibility constraints — API can break freely until first release.

### 3.1 Data Exposure

- `/agents/stats` — per-agent_id aggregation (runs, tokens, tools, duration, error rate)
- Session detail page — waterfall chart for LLM + Tool call timeline within a single session
- Expose `subagent_runs` and `mcp_calls` via API and UI
- Gateway heartbeat history and uptime trends

### 3.2 Time Series Aggregation

- `/timeseries` — time-bucket aggregation (1m/5m/1h/1d)
- Time range selector (1h/6h/24h/7d) truly drives all charts
- Historical trend line charts replace "latest N rows" mode
- Stacked area charts — multi-metric overlay (e.g. CPU+memory, per-model token trends)

### 3.3 Rich Chart Types

- Multi-metric combo charts — overlay different dimensions on one chart (e.g. LLM volume + latency + error rate)
- Heatmap — time × Agent activity matrix
- Pie/donut charts — token share, tool call distribution
- Scatter plots — per-LLM-call latency-vs-token scatter
- Agent comparison view — cross-agent runs/tokens/errors
- Session waterfall — LLM + Tool call timeline展开
- Error aggregation — grouped by `error_category`, top-N
- Cost analysis — by model/agent/time breakdown (when cost data available)

### 3.4 Configuration & Customization

- Dashboard JSON config (module visibility, ordering, refresh interval)
- Drag-and-drop module reordering
- Threshold alerting with visual highlight (e.g. CPU > 80%, error rate > 10%)
- Dark/light theme toggle

### 3.5 Frontend Engineering

- Migrate single HTML → Vite component architecture (Vite config already in place)
- Adopt lightweight chart library (e.g. Chart.js, lightweight-charts) to replace hand-drawn SVG
- Responsive layout optimization for tablet/mobile

**Constraints:** No compat burden. SQLite aggregation performance matters (leverage indexes + `strftime` bucketing). Frontend bundle < 200KB gzipped. Zero external runtime dependencies.

---

## Phase 4 — Advanced Observability

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
