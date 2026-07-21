package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zylcold/openclaw-observatory/internal/event"
	"github.com/zylcold/openclaw-observatory/internal/storage"
)

const (
	Version      = "0.4.0"
	APIVersion   = 3
	queryTimeout = 30 * time.Second
)

var BuildID = "dev"

var Capabilities = []string{"agent-stats-v3", "session-waterfall-v3", "timeseries-v3", "dashboard-config-v3", "disk-space-v3", "cost-trends-v4", "cursor-pagination-v4", "tool-observability-v4", "trace-span-v6", "anomaly-signals-v6", "cost-attribution-v6"}

type Server struct {
	repo           *storage.Repository
	hub            *Hub
	log            *slog.Logger
	ready          atomic.Bool
	backpressureMu sync.Mutex
	backpressured  map[string]bool
}

func New(repo *storage.Repository, logger *slog.Logger) *Server {
	s := &Server{repo: repo, hub: NewHub(), log: logger, backpressured: make(map[string]bool)}
	s.ready.Store(true)
	return s
}
func (s *Server) Hub() *Hub { return s.hub }

func (s *Server) IngestHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/events", s.ingest)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]any{"ok": true}) })
	return mux
}

func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /ready", s.readyHandler)
	mux.HandleFunc("GET /metrics", s.metrics)
	mux.HandleFunc("GET /api/v1/status", s.status)
	mux.HandleFunc("GET /api/v1/dashboard", s.dashboard)
	mux.HandleFunc("GET /api/v1/instances", s.instances)
	mux.HandleFunc("GET /api/v1/sessions", s.sessions)
	mux.HandleFunc("GET /api/v1/sessions/", s.sessionDetail)
	mux.HandleFunc("GET /api/v1/runs", s.runs)
	mux.HandleFunc("GET /api/v1/runs/", s.runDetail)
	mux.HandleFunc("GET /api/v1/agents/stats", s.agentStats)
	mux.HandleFunc("GET /api/v1/subagents", s.subagents)
	mux.HandleFunc("GET /api/v1/mcp/calls", s.mcpCalls)
	mux.HandleFunc("GET /api/v1/llm/calls", s.llmCalls)
	mux.HandleFunc("GET /api/v1/timeseries", s.timeseries)
	mux.HandleFunc("GET /api/v1/errors/stats", s.errorStats)
	mux.HandleFunc("GET /api/v1/resources", s.resources)
	mux.HandleFunc("GET /api/v1/tools/stats", s.toolStats)
	mux.HandleFunc("GET /api/v1/models/stats", s.modelStats)
	mux.HandleFunc("GET /api/v1/events", s.events)
	mux.HandleFunc("GET /api/v1/cost/trends", s.costTrends)
	mux.HandleFunc("GET /api/v1/cost/summary", s.costSummary)
	mux.HandleFunc("GET /api/v1/stream", s.stream)
	return securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		isQuery := r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/") && r.URL.Path != "/api/v1/stream"
		if isQuery {
			ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
			defer cancel()
			r = r.WithContext(ctx)
		}
		mux.ServeHTTP(w, r)
		if isQuery {
			s.repo.RecordQueryDuration(time.Since(started))
		}
	}))
}

func (s *Server) Insert(ctx context.Context, events []event.Event) error {
	started := time.Now()
	res, err := s.repo.InsertEvents(ctx, events)
	if err != nil {
		return err
	}
	s.warnSlowInsert(events, time.Since(started))
	for _, e := range res.Inserted {
		b, _ := json.Marshal(e)
		s.hub.Publish(b)
	}
	s.observeBackpressure(res.Inserted)
	return nil
}

func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, event.MaxBatchBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		apiError(w, 413, "batch_too_large", err.Error())
		return
	}
	if len(body) > event.MaxBatchBytes {
		apiError(w, 413, "batch_too_large", "batch exceeds 1 MiB")
		return
	}
	var batch []event.Event
	if err := json.Unmarshal(body, &batch); err != nil {
		apiError(w, 400, "invalid_json", "expected a JSON event array")
		return
	}
	if len(batch) == 0 || len(batch) > 1000 {
		apiError(w, 400, "invalid_batch", "batch must contain 1 to 1000 events")
		return
	}
	now := time.Now()
	for i, e := range batch {
		if err := e.Validate(now); err != nil {
			status := 400
			if strings.Contains(err.Error(), "schemaVersion") {
				status = 422
			}
			apiError(w, status, "invalid_event", fmt.Sprintf("event %d: %v", i, err))
			return
		}
	}
	started := time.Now()
	result, err := s.repo.InsertEvents(r.Context(), batch)
	if err != nil {
		s.log.Error("insert events", "error", err)
		apiError(w, 500, "storage_error", "failed to persist events")
		return
	}
	s.warnSlowInsert(batch, time.Since(started))
	for _, e := range result.Inserted {
		b, _ := json.Marshal(e)
		s.hub.Publish(b)
	}
	s.observeBackpressure(result.Inserted)
	writeJSON(w, 202, result)
}

func (s *Server) warnSlowInsert(events []event.Event, elapsed time.Duration) {
	if elapsed > time.Second {
		s.log.Warn("slow event batch insert", "events", len(events), "duration", elapsed)
	}
}

func (s *Server) observeBackpressure(events []event.Event) {
	for _, e := range events {
		if e.EventType != "gateway.heartbeat" {
			continue
		}
		payload := event.PayloadMap(e.Payload)
		capacity, depth := event.Float(payload, "queueCapacity"), event.Float(payload, "queueDepth")
		if capacity <= 0 {
			continue
		}
		saturated := depth >= capacity*0.8
		s.backpressureMu.Lock()
		wasSaturated := s.backpressured[e.InstanceID]
		if saturated {
			s.backpressured[e.InstanceID] = true
		} else {
			delete(s.backpressured, e.InstanceID)
		}
		s.backpressureMu.Unlock()
		if saturated && !wasSaturated {
			s.log.Warn("plugin event queue nearing capacity", "instance_id", e.InstanceID, "queue_depth", depth, "queue_capacity", capacity)
		} else if !saturated && wasSaturated {
			s.log.Info("plugin event queue recovered", "instance_id", e.InstanceID, "queue_depth", depth, "queue_capacity", capacity)
		}
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "ok", "version": Version, "time": time.Now().UTC()})
}
func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
	readiness, err := s.repo.Readiness(r.Context())
	if !s.ready.Load() || err != nil {
		writeJSON(w, 503, map[string]any{"status": "not_ready"})
		return
	}
	response := map[string]any{"status": "ready", "eventDelaySeconds": readiness.EventDelay.Seconds()}
	if readiness.LastEventReceivedAt != nil {
		response["lastEventReceivedAt"] = readiness.LastEventReceivedAt.UTC()
	}
	writeJSON(w, 200, response)
}
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	v, err := s.statusData(r.Context())
	if err != nil {
		apiError(w, 500, "storage_error", "failed to query status")
		return
	}
	data(w, v)
}

func (s *Server) statusData(ctx context.Context) (map[string]any, error) {
	v, err := s.repo.Status(ctx)
	if err != nil {
		return nil, err
	}
	schemaVersion, err := s.repo.SchemaVersion(ctx)
	if err != nil {
		return nil, err
	}
	v["apiVersion"] = APIVersion
	v["schemaVersion"] = schemaVersion
	v["capabilities"] = Capabilities
	v["buildId"] = BuildID
	v["daemon"] = map[string]any{"version": Version, "ready": s.ready.Load(), "buildId": BuildID}
	v["time"] = time.Now().UTC()
	return v, nil
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	o.Limit = 200
	bucket, bucketSeconds, err := timeseriesOptions(r, &o)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	status, err := s.statusData(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query status")
		return
	}
	timeseries, err := s.repo.TimeSeries(r.Context(), o, bucketSeconds)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query timeseries")
		return
	}
	timeseries["bucket"] = bucket
	models, err := s.repo.ModelStats(r.Context(), o)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query model stats")
		return
	}
	tools, err := s.repo.ToolStats(r.Context(), o)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query tool stats")
		return
	}
	agents, err := s.repo.AgentStats(r.Context(), o)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query agent stats")
		return
	}
	agentModels, err := s.repo.AgentModelStats(r.Context(), o)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query agent model stats")
		return
	}
	lifetime, err := s.repo.LifetimeStats(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query lifetime stats")
		return
	}
	sessions, err := s.repo.ListSessions(r.Context(), o)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query sessions")
		return
	}
	llmCalls, err := s.repo.ListLLMCalls(r.Context(), o)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query LLM calls")
		return
	}
	errors, err := s.repo.ErrorStats(r.Context(), o)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query error stats")
		return
	}
	anomalies, err := s.repo.RecentAnomalies(r.Context(), o)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query recent anomalies")
		return
	}
	subagents, err := s.repo.ListSubagentRuns(r.Context(), o)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query subagents")
		return
	}
	mcpCalls, err := s.repo.ListMCPCalls(r.Context(), o)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query MCP calls")
		return
	}
	cost7d := o
	cost7d.To = time.Now().UTC().Format(time.RFC3339Nano)
	cost7d.From = time.Now().UTC().AddDate(0, 0, -6).Format(time.RFC3339Nano)
	costTrends, err := s.repo.CostTrends(r.Context(), cost7d, "day")
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query cost trends")
		return
	}
	costSummary, err := s.repo.CostSummary(r.Context(), o)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query cost summary")
		return
	}
	cost30d := o
	cost30d.To = time.Now().UTC().Format(time.RFC3339Nano)
	cost30d.From = time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339Nano)
	costTrends30d, err := s.repo.CostTrends(r.Context(), cost30d, "day")
	if err != nil {
		apiError(w, http.StatusInternalServerError, "storage_error", "failed to query 30-day cost trends")
		return
	}
	data(w, map[string]any{
		"status": status, "timeseries": timeseries, "models": models, "tools": tools, "agents": agents,
		"agentModels": agentModels,
		"lifetime": lifetime,
		"sessions": sessions, "llmCalls": llmCalls, "errors": errors, "anomalies": anomalies, "subagents": subagents, "mcpCalls": mcpCalls,
		"costTrends": costTrends, "costSummary": costSummary, "costTrends30d": costTrends30d,
	})
}
func (s *Server) instances(w http.ResponseWriter, r *http.Request) {
	v, err := s.repo.ListInstances(r.Context())
	list(w, v, err, 500)
}
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	v, e := s.repo.ListSessions(r.Context(), o)
	list(w, v, e, o.Limit)
}
func (s *Server) runs(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	v, e := s.repo.ListRuns(r.Context(), o)
	list(w, v, e, o.Limit)
}
func (s *Server) resources(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	v, e := s.repo.ListResources(r.Context(), o)
	list(w, v, e, o.Limit)
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	o.EventType = r.URL.Query().Get("eventType")
	v, e := s.repo.ListEvents(r.Context(), o)
	list(w, v, e, o.Limit)
}
func (s *Server) sessionDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	v, err := s.repo.SessionDetail(r.Context(), id)
	detail(w, v, err)
}
func (s *Server) runDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/runs/")
	v, err := s.repo.RunDetail(r.Context(), id)
	detail(w, v, err)
}
func (s *Server) toolStats(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	v, e := s.repo.ToolStats(r.Context(), o)
	list(w, v, e, 200)
}
func (s *Server) modelStats(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	v, e := s.repo.ModelStats(r.Context(), o)
	list(w, v, e, 200)
}

func (s *Server) agentStats(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	v, e := s.repo.AgentStats(r.Context(), o)
	list(w, v, e, 200)
}

func (s *Server) subagents(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	v, e := s.repo.ListSubagentRuns(r.Context(), o)
	list(w, v, e, o.Limit)
}

func (s *Server) mcpCalls(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	v, e := s.repo.ListMCPCalls(r.Context(), o)
	list(w, v, e, o.Limit)
}

func (s *Server) llmCalls(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	v, e := s.repo.ListLLMCalls(r.Context(), o)
	list(w, v, e, o.Limit)
}

func (s *Server) errorStats(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	v, e := s.repo.ErrorStats(r.Context(), o)
	list(w, v, e, 200)
}

func (s *Server) timeseries(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	bucket, bucketSeconds, err := timeseriesOptions(r, &o)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	v, err := s.repo.TimeSeries(r.Context(), o, bucketSeconds)
	if err != nil {
		apiError(w, 500, "storage_error", "query failed")
		return
	}
	v["bucket"] = bucket
	data(w, v)
}

func timeseriesOptions(r *http.Request, o *storage.ListOptions) (string, int64, error) {
	now := time.Now().UTC()
	if o.To == "" {
		o.To = now.Format(time.RFC3339Nano)
	}
	if o.From == "" {
		o.From = now.Add(-time.Hour).Format(time.RFC3339Nano)
	}
	from, err := time.Parse(time.RFC3339, o.From)
	if err != nil {
		return "", 0, fmt.Errorf("from must be RFC3339")
	}
	to, err := time.Parse(time.RFC3339, o.To)
	if err != nil {
		return "", 0, fmt.Errorf("to must be RFC3339")
	}
	if !from.Before(to) {
		return "", 0, fmt.Errorf("from must be before to")
	}
	buckets := map[string]int64{"1m": 60, "5m": 300, "1h": 3600, "1d": 86400}
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		bucket = "1m"
	}
	bucketSeconds, ok := buckets[bucket]
	if !ok {
		return "", 0, fmt.Errorf("bucket must be one of 1m, 5m, 1h, 1d")
	}
	if to.Sub(from) > time.Duration(bucketSeconds*2000)*time.Second {
		return "", 0, fmt.Errorf("time range produces more than 2000 buckets")
	}
	return bucket, bucketSeconds, nil
}

func (s *Server) costTrends(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "day"
	}
	if period != "day" && period != "week" && period != "month" {
		apiError(w, 400, "invalid_query", "period must be day, week, or month")
		return
	}
	v, e := s.repo.CostTrends(r.Context(), o, period)
	list(w, v, e, 200)
}

func (s *Server) costSummary(w http.ResponseWriter, r *http.Request) {
	o, err := options(r)
	if err != nil {
		apiError(w, 400, "invalid_query", err.Error())
		return
	}
	v, e := s.repo.CostSummary(r.Context(), o)
	if e != nil {
		apiError(w, 500, "storage_error", "query failed")
		return
	}
	data(w, v)
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		apiError(w, 500, "stream_unsupported", "streaming unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(ch)
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	f.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case b, ok := <-ch:
			if !ok {
				return
			}
			var meta struct {
				EventID string `json:"eventId"`
			}
			_ = json.Unmarshal(b, &meta)
			fmt.Fprintf(w, "event: monitor-event\nid: %s\ndata: %s\n\n", meta.EventID, b)
			f.Flush()
		case <-tick.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			f.Flush()
		}
	}
}

func options(r *http.Request) (storage.ListOptions, error) {
	q := r.URL.Query()
	n := 100
	if q.Get("limit") != "" {
		v, e := strconv.Atoi(q.Get("limit"))
		if e != nil || v < 1 || v > 500 {
			return storage.ListOptions{}, errors.New("limit must be between 1 and 500")
		}
		n = v
	}
	for _, k := range []string{"from", "to"} {
		if v := q.Get(k); v != "" {
			if _, e := time.Parse(time.RFC3339, v); e != nil {
				return storage.ListOptions{}, fmt.Errorf("%s must be RFC3339", k)
			}
		}
	}
	return storage.ListOptions{Limit: n, Cursor: q.Get("cursor"), InstanceID: q.Get("instanceId"), AgentID: q.Get("agentId"), From: q.Get("from"), To: q.Get("to"), Status: q.Get("status")}, nil
}

func list(w http.ResponseWriter, v []map[string]any, err error, limit int) {
	if err != nil {
		apiError(w, 500, "storage_error", "query failed")
		return
	}
	// Cursor pagination: if we got limit+1 rows, trim to limit and encode nextCursor.
	nextCursor := ""
	if len(v) > limit {
		v = v[:limit]
		if len(v) > 0 {
			last := v[len(v)-1]
			pos := map[string]any{}
			if t, ok := last["startedAt"].(string); ok && t != "" {
				pos["t"] = t
			} else if t, ok := last["occurredAt"].(string); ok && t != "" {
				pos["t"] = t
			} else if t, ok := last["sampledAt"].(string); ok && t != "" {
				pos["t"] = t
			}
			if id, ok := last["eventId"].(string); ok {
				pos["id"] = id
			} else if id, ok := last["sessionId"].(string); ok {
				pos["id"] = id
			} else if id, ok := last["runId"].(string); ok {
				pos["id"] = id
			}
			nextCursor = storage.EncodeCursor(pos)
		}
	}
	writeJSON(w, 200, map[string]any{"data": v, "page": map[string]any{"limit": limit, "nextCursor": nextCursor}})
}
func detail(w http.ResponseWriter, v map[string]any, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, 404, "not_found", "object not found")
		return
	}
	if err != nil {
		apiError(w, 500, "storage_error", "query failed")
		return
	}
	data(w, v)
}
func data(w http.ResponseWriter, v any) { writeJSON(w, 200, map[string]any{"data": v}) }
func apiError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": msg, "requestId": event.NewID()}})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	snap, err := s.repo.Metrics(r.Context(), float64(time.Now().Unix()))
	if err != nil {
		apiError(w, 500, "metrics_error", "failed to aggregate metrics")
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	emit := func(name, typ, help string, rows []storage.MetricRow) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
		for _, row := range rows {
			fmt.Fprintf(w, "%s%s %s\n", name, labels(row.Labels), number(row.Value))
		}
	}
	emit("openclaw_gateway_up", "gauge", "Whether the observed Gateway is up.", snap.GatewayUp)
	emit("openclaw_gateway_uptime_seconds", "gauge", "Gateway uptime in seconds.", snap.Uptime)
	emit("openclaw_gateway_restarts_total", "counter", "Observed Gateway restarts.", snap.Restarts)
	emit("openclaw_sessions_active", "gauge", "Active sessions.", snap.SessionsActive)
	emit("openclaw_agent_runs_active", "gauge", "Active agent runs.", snap.RunsActive)
	emit("openclaw_agent_runs_total", "counter", "Agent runs by terminal status.", snap.Runs)
	emit("openclaw_llm_requests_total", "counter", "LLM calls.", snap.LLM)
	for i := range snap.LLMTokensInput {
		snap.LLMTokensInput[i].Labels["direction"] = "input"
	}
	for i := range snap.LLMTokensOutput {
		snap.LLMTokensOutput[i].Labels["direction"] = "output"
	}
	emit("openclaw_llm_tokens_total", "counter", "LLM tokens by direction.", append(snap.LLMTokensInput, snap.LLMTokensOutput...))
	emit("openclaw_llm_cost_usd_total", "counter", "Reported LLM cost in USD.", snap.LLMCost)
	emit("openclaw_tool_calls_total", "counter", "Tool calls.", snap.Tools)
	emit("openclaw_tool_errors_total", "counter", "Tool errors.", snap.ToolErrors)
	emit("openclaw_monitor_events_received_total", "counter", "Accepted unique events.", snap.Received)
	emit("openclaw_monitor_events_dropped_total", "counter", "Plugin or monitor drops.", snap.Dropped)
	emit("openclaw_monitor_event_queue_depth", "gauge", "Plugin queue depth reported on the latest heartbeat.", snap.QueueDepth)
	writeMetrics := s.repo.WriteMetrics()
	emitSummary := func(name, help string, seconds float64, count uint64) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s summary\n", name, help, name)
		fmt.Fprintf(w, "%s_sum %s\n%s_count %d\n", name, number(seconds), name, count)
	}
	emitSummary("openclaw_monitor_insert_duration_seconds", "Time spent inserting raw events into SQLite.", writeMetrics.InsertSeconds, writeMetrics.InsertCount)
	emitSummary("openclaw_monitor_reduce_duration_seconds", "Time spent reducing events into projections.", writeMetrics.ReduceSeconds, writeMetrics.ReduceCount)
	emitSummary("openclaw_monitor_commit_duration_seconds", "Time spent committing SQLite write transactions.", writeMetrics.CommitSeconds, writeMetrics.CommitCount)
	emitSummary("openclaw_monitor_query_duration_seconds", "End-to-end duration of REST API queries.", writeMetrics.QuerySeconds, writeMetrics.QueryCount)
	resources := map[string]string{"cpuSecondsTotal": "openclaw_process_cpu_seconds_total", "residentMemoryBytes": "openclaw_process_resident_memory_bytes", "virtualMemoryBytes": "openclaw_process_virtual_memory_bytes", "threads": "openclaw_process_threads", "openFds": "openclaw_process_open_fds", "readBytesTotal": "openclaw_process_read_bytes_total", "writeBytesTotal": "openclaw_process_write_bytes_total", "diskTotalBytes": "openclaw_host_disk_total_bytes", "diskAvailableBytes": "openclaw_host_disk_available_bytes"}
	for kind, name := range resources {
		var rows []storage.MetricRow
		for _, row := range snap.Resources {
			if row.Labels["kind"] == kind {
				delete(row.Labels, "kind")
				rows = append(rows, row)
			}
		}
		typ := "gauge"
		if strings.HasSuffix(name, "_total") {
			typ = "counter"
		}
		emit(name, typ, "Latest sampled process value.", rows)
	}
}
func labels(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	p := make([]string, 0, len(keys))
	for _, k := range keys {
		p = append(p, k+`="`+escapeLabel(m[k])+`"`)
	}
	return "{" + strings.Join(p, ",") + "}"
}
func escapeLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
	if len(s) > 128 {
		s = s[:128]
	}
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s)
}
func number(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
