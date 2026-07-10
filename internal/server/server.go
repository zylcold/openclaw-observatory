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
	"sync/atomic"
	"time"

	"github.com/zylcold/openclaw-observatory/internal/event"
	"github.com/zylcold/openclaw-observatory/internal/storage"
	webassets "github.com/zylcold/openclaw-observatory/web"
)

const Version = "0.1.0"

type Server struct {
	repo  *storage.Repository
	hub   *Hub
	log   *slog.Logger
	ready atomic.Bool
}

func New(repo *storage.Repository, logger *slog.Logger) *Server {
	s := &Server{repo: repo, hub: NewHub(), log: logger}
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
	mux.HandleFunc("GET /api/v1/instances", s.instances)
	mux.HandleFunc("GET /api/v1/sessions", s.sessions)
	mux.HandleFunc("GET /api/v1/sessions/", s.sessionDetail)
	mux.HandleFunc("GET /api/v1/runs", s.runs)
	mux.HandleFunc("GET /api/v1/runs/", s.runDetail)
	mux.HandleFunc("GET /api/v1/resources", s.resources)
	mux.HandleFunc("GET /api/v1/tools/stats", s.toolStats)
	mux.HandleFunc("GET /api/v1/models/stats", s.modelStats)
	mux.HandleFunc("GET /api/v1/events", s.events)
	mux.HandleFunc("GET /api/v1/stream", s.stream)
	mux.Handle("GET /", http.FileServer(http.FS(webassets.Files)))
	return securityHeaders(mux)
}

func (s *Server) Insert(ctx context.Context, events []event.Event) error {
	res, err := s.repo.InsertEvents(ctx, events)
	if err != nil {
		return err
	}
	for _, e := range res.Inserted {
		b, _ := json.Marshal(e)
		s.hub.Publish(b)
	}
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
	result, err := s.repo.InsertEvents(r.Context(), batch)
	if err != nil {
		s.log.Error("insert events", "error", err)
		apiError(w, 500, "storage_error", "failed to persist events")
		return
	}
	for _, e := range result.Inserted {
		b, _ := json.Marshal(e)
		s.hub.Publish(b)
	}
	writeJSON(w, 202, result)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "ok", "version": Version, "time": time.Now().UTC()})
}
func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
	if !s.ready.Load() || s.repo.Ping(r.Context()) != nil {
		writeJSON(w, 503, map[string]any{"status": "not_ready"})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ready"})
}
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	v, err := s.repo.Status(r.Context())
	if err != nil {
		apiError(w, 500, "storage_error", "failed to query status")
		return
	}
	v["daemon"] = map[string]any{"version": Version, "ready": s.ready.Load()}
	v["time"] = time.Now().UTC()
	data(w, v)
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
	v, e := s.repo.ToolStats(r.Context())
	list(w, v, e, 200)
}
func (s *Server) modelStats(w http.ResponseWriter, r *http.Request) {
	v, e := s.repo.ModelStats(r.Context())
	list(w, v, e, 200)
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
	return storage.ListOptions{Limit: n, InstanceID: q.Get("instanceId"), From: q.Get("from"), To: q.Get("to"), Status: q.Get("status")}, nil
}

func list(w http.ResponseWriter, v []map[string]any, err error, limit int) {
	if err != nil {
		apiError(w, 500, "storage_error", "query failed")
		return
	}
	writeJSON(w, 200, map[string]any{"data": v, "page": map[string]any{"limit": limit, "nextCursor": nil}})
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'")
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
	resources := map[string]string{"cpuSecondsTotal": "openclaw_process_cpu_seconds_total", "residentMemoryBytes": "openclaw_process_resident_memory_bytes", "virtualMemoryBytes": "openclaw_process_virtual_memory_bytes", "threads": "openclaw_process_threads", "openFds": "openclaw_process_open_fds", "readBytesTotal": "openclaw_process_read_bytes_total", "writeBytesTotal": "openclaw_process_write_bytes_total"}
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
	fmt.Fprint(w, "# HELP openclaw_monitor_event_queue_depth Plugin queue depth (reported on heartbeat).\n# TYPE openclaw_monitor_event_queue_depth gauge\n")
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
