package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/zylcold/openclaw-observatory/internal/event"
	"github.com/zylcold/openclaw-observatory/internal/storage"
)

const (
	dashboardSnapshotFreshTTL       = time.Minute
	dashboardSnapshotStaleTTL       = 5 * time.Minute
	dashboardSnapshotRefreshTimeout = 60 * time.Second
	dashboardSnapshotColdWait       = 2 * time.Second
	dashboardSnapshotRetryDelay     = 30 * time.Second
	dashboardSnapshotMaxEntries     = 32
)

type dashboardCacheEntry struct {
	body        []byte
	data        map[string]any
	bucket      int64
	freshUntil  time.Time
	staleUntil  time.Time
	nextRetryAt time.Time
	inFlight    chan struct{}
	lastErr     error
}

type dashboardCacheStats struct {
	mu             sync.Mutex
	hits           uint64
	stale          uint64
	misses         uint64
	coalesced      uint64
	coldFallbacks  uint64
	refreshes      uint64
	refreshFailed  uint64
	moduleFailures uint64
	cancellations  uint64
	refreshSeconds float64
}

type dashboardCacheStatsSnapshot struct {
	Hits           uint64
	Stale          uint64
	Misses         uint64
	Coalesced      uint64
	ColdFallbacks  uint64
	Refreshes      uint64
	RefreshFailed  uint64
	ModuleFailures uint64
	Cancellations  uint64
	RefreshSeconds float64
}

func (m *dashboardCacheStats) add(field *uint64) {
	m.mu.Lock()
	*field++
	m.mu.Unlock()
}

func (m *dashboardCacheStats) recordRefresh(elapsed time.Duration, failed bool) {
	m.mu.Lock()
	m.refreshes++
	m.refreshSeconds += elapsed.Seconds()
	if failed {
		m.refreshFailed++
	}
	m.mu.Unlock()
}

func (m *dashboardCacheStats) snapshot() dashboardCacheStatsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return dashboardCacheStatsSnapshot{
		Hits:           m.hits,
		Stale:          m.stale,
		Misses:         m.misses,
		Coalesced:      m.coalesced,
		ColdFallbacks:  m.coldFallbacks,
		Refreshes:      m.refreshes,
		RefreshFailed:  m.refreshFailed,
		ModuleFailures: m.moduleFailures,
		Cancellations:  m.cancellations,
		RefreshSeconds: m.refreshSeconds,
	}
}

type liveInstance struct {
	InstanceID      string     `json:"instanceId"`
	Status          string     `json:"status"`
	LastSeenAt      time.Time  `json:"lastSeenAt"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
	LastResourceAt  *time.Time `json:"lastResourceAt,omitempty"`
	QueueDepth      float64    `json:"queueDepth,omitempty"`
	QueueCapacity   float64    `json:"queueCapacity,omitempty"`
}

type liveSummary struct {
	lastEventReceivedAt time.Time
	instances           map[string]liveInstance
}

func (s *Server) observeLiveSummary(events []event.Event) {
	if len(events) == 0 {
		return
	}
	s.liveSummaryMu.Lock()
	defer s.liveSummaryMu.Unlock()
	if s.liveSummary.instances == nil {
		s.liveSummary.instances = make(map[string]liveInstance)
	}
	for _, e := range events {
		occurredAt := e.OccurredAt.UTC()
		if occurredAt.After(s.liveSummary.lastEventReceivedAt) {
			s.liveSummary.lastEventReceivedAt = occurredAt
		}
		instance := s.liveSummary.instances[e.InstanceID]
		instance.InstanceID = e.InstanceID
		if instance.Status == "" {
			instance.Status = "unknown"
		}
		if occurredAt.After(instance.LastSeenAt) {
			instance.LastSeenAt = occurredAt
		}
		switch e.EventType {
		case "gateway.started", "gateway.heartbeat":
			instance.Status = "up"
		case "gateway.stopped":
			instance.Status = "stopped"
		case "gateway.crashed":
			instance.Status = "crashed"
		case "resource.sampled":
			at := occurredAt
			instance.LastResourceAt = &at
		}
		if e.EventType == "gateway.heartbeat" {
			at := occurredAt
			instance.LastHeartbeatAt = &at
			payload := event.PayloadMap(e.Payload)
			instance.QueueDepth = event.Float(payload, "queueDepth")
			instance.QueueCapacity = event.Float(payload, "queueCapacity")
		}
		s.liveSummary.instances[e.InstanceID] = instance
	}
}

// PrimeLiveSummary is a bounded startup read. The public summary endpoint never
// performs this query; it only serves the in-memory state maintained above.
func (s *Server) PrimeLiveSummary(ctx context.Context) {
	instances, err := s.analyticsRepo.ListInstances(ctx)
	if err != nil {
		s.log.Warn("prime live summary", "error", err)
		return
	}
	s.liveSummaryMu.Lock()
	defer s.liveSummaryMu.Unlock()
	if s.liveSummary.instances == nil {
		s.liveSummary.instances = make(map[string]liveInstance)
	}
	for _, row := range instances {
		instanceID, _ := row["instanceId"].(string)
		if instanceID == "" {
			continue
		}
		instance := s.liveSummary.instances[instanceID]
		instance.InstanceID = instanceID
		if status, _ := row["status"].(string); status != "" {
			instance.Status = status
		}
		if raw, _ := row["lastSeenAt"].(string); raw != "" {
			if parsed, parseErr := time.Parse(time.RFC3339Nano, raw); parseErr == nil {
				instance.LastSeenAt = parsed.UTC()
				if parsed.After(s.liveSummary.lastEventReceivedAt) {
					s.liveSummary.lastEventReceivedAt = parsed.UTC()
				}
			}
		}
		s.liveSummary.instances[instanceID] = instance
	}
}

func (s *Server) liveSummaryData() map[string]any {
	s.liveSummaryMu.RLock()
	lastEvent := s.liveSummary.lastEventReceivedAt
	instances := make([]liveInstance, 0, len(s.liveSummary.instances))
	for _, instance := range s.liveSummary.instances {
		instances = append(instances, instance)
	}
	s.liveSummaryMu.RUnlock()
	sort.Slice(instances, func(i, j int) bool { return instances[i].InstanceID < instances[j].InstanceID })

	now := time.Now().UTC()
	data := map[string]any{
		"apiVersion":    APIVersion,
		"schemaVersion": storage.CurrentSchemaVersion,
		"capabilities":  Capabilities,
		"buildId":       BuildID,
		"daemon":        map[string]any{"version": Version, "ready": s.ready.Load(), "buildId": BuildID},
		"gatewayProbe":  s.GatewayProbe(),
		"instances":     instances,
		"time":          now,
	}
	if !lastEvent.IsZero() {
		data["lastEventReceivedAt"] = lastEvent
		delay := now.Sub(lastEvent)
		if delay < 0 {
			delay = 0
		}
		data["eventDelaySeconds"] = delay.Seconds()
	}
	return data
}

func (s *Server) dashboardSnapshot(w http.ResponseWriter, r *http.Request) {
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
	key, cacheOptions := dashboardSnapshotCacheKey(o, bucketSeconds)
	body, cacheState := s.cachedDashboardSnapshot(r.Context(), key, cacheOptions, bucket, bucketSeconds)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Observatory-Dashboard-Cache", cacheState)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) cachedDashboardSnapshot(ctx context.Context, key string, o storage.ListOptions, bucket string, bucketSeconds int64) ([]byte, string) {
	now := time.Now()
	s.dashboardCacheMu.Lock()
	s.pruneDashboardCacheLocked(now)
	entry := s.dashboardCache[key]
	if entry != nil && len(entry.body) > 0 {
		if now.Before(entry.freshUntil) {
			s.dashboardStats.add(&s.dashboardStats.hits)
			body := entry.body
			s.dashboardCacheMu.Unlock()
			return body, "HIT"
		}
		if entry.inFlight == nil && !now.Before(entry.nextRetryAt) {
			s.startDashboardRefreshLocked(key, entry, o, bucket, bucketSeconds)
		}
		s.dashboardStats.add(&s.dashboardStats.stale)
		body := entry.body
		s.dashboardCacheMu.Unlock()
		return body, "STALE"
	}

	if entry == nil {
		entry = &dashboardCacheEntry{}
		s.dashboardCache[key] = entry
	}
	if entry.inFlight == nil && !now.Before(entry.nextRetryAt) {
		s.startDashboardRefreshLocked(key, entry, o, bucket, bucketSeconds)
		s.dashboardStats.add(&s.dashboardStats.misses)
	} else {
		s.dashboardStats.add(&s.dashboardStats.coalesced)
	}
	flight := entry.inFlight
	s.dashboardCacheMu.Unlock()

	if flight != nil {
		timer := time.NewTimer(s.dashboardColdWait())
		defer timer.Stop()
		select {
		case <-flight:
			s.dashboardCacheMu.Lock()
			body := entry.body
			s.dashboardCacheMu.Unlock()
			if len(body) > 0 {
				return body, "MISS"
			}
		case <-ctx.Done():
			s.dashboardStats.add(&s.dashboardStats.cancellations)
		case <-timer.C:
		}
	}
	s.dashboardStats.add(&s.dashboardStats.coldFallbacks)
	if body, ok := s.dashboardLastBody[strconv.FormatInt(bucketSeconds, 10)]; ok && len(body) > 0 {
		return body, "STALE"
	}
	return mustJSON(map[string]any{"data": s.emptyDashboard(o, bucket, bucketSeconds, "warming")}), "WARMING"
}

// dashboardColdWait bounds how long a cold request waits for its snapshot
// build. It is derived from the measured average refresh duration so the first
// request for a cache key typically returns real data instead of a warming
// placeholder, while still bounding the request's worst-case latency.
func (s *Server) dashboardColdWait() time.Duration {
	stats := s.dashboardStats.snapshot()
	if stats.Refreshes == 0 {
		return dashboardSnapshotColdWait
	}
	avgSeconds := stats.RefreshSeconds / float64(stats.Refreshes)
	wait := time.Duration((avgSeconds + 1) * float64(time.Second))
	const maxWait = 8 * time.Second
	if wait < dashboardSnapshotColdWait {
		wait = dashboardSnapshotColdWait
	}
	if wait > maxWait {
		wait = maxWait
	}
	return wait
}

func (s *Server) startDashboardRefreshLocked(key string, entry *dashboardCacheEntry, o storage.ListOptions, bucket string, bucketSeconds int64) {
	entry.inFlight = make(chan struct{})
	entry.bucket = bucketSeconds
	fallback := entry.data
	go func() {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), dashboardSnapshotRefreshTimeout)
		defer cancel()
		payload := s.buildDashboardSnapshot(ctx, o, bucket, bucketSeconds, fallback)
		body, err := json.Marshal(map[string]any{"data": payload})
		s.finishDashboardRefresh(key, entry, body, payload, err)
		s.dashboardStats.recordRefresh(time.Since(started), err != nil)
	}()
}

func (s *Server) finishDashboardRefresh(key string, entry *dashboardCacheEntry, body []byte, payload map[string]any, err error) {
	s.dashboardCacheMu.Lock()
	defer s.dashboardCacheMu.Unlock()
	now := time.Now()
	if err == nil {
		entry.body = body
		entry.data = payload
		entry.freshUntil = now.Add(dashboardSnapshotFreshTTL)
		entry.staleUntil = now.Add(dashboardSnapshotStaleTTL)
		entry.nextRetryAt = time.Time{}
		entry.lastErr = nil
		if entry.bucket > 0 {
			// Keep the most recent successful body per bucket so a cadence or
			// filter change never briefly blanks the dashboard to "warming".
			s.dashboardLastBody[strconv.FormatInt(entry.bucket, 10)] = body
		}
	} else {
		entry.lastErr = err
		entry.nextRetryAt = now.Add(dashboardSnapshotRetryDelay)
		s.log.Warn("dashboard snapshot refresh failed", "error", err)
	}
	if entry.inFlight != nil {
		close(entry.inFlight)
		entry.inFlight = nil
	}
	if err != nil && len(entry.body) == 0 && s.dashboardCache[key] == entry {
		// Keep the failed entry through nextRetryAt so rapid browser refreshes do
		// not start a thundering herd of identical SQLite work.
		entry.staleUntil = now.Add(dashboardSnapshotRetryDelay)
	}
}

func (s *Server) pruneDashboardCacheLocked(now time.Time) {
	for key, entry := range s.dashboardCache {
		if entry.inFlight != nil {
			continue
		}
		if len(entry.body) == 0 && !entry.nextRetryAt.IsZero() && now.After(entry.nextRetryAt.Add(2*dashboardSnapshotRetryDelay)) {
			delete(s.dashboardCache, key)
			continue
		}
		if len(entry.body) > 0 && now.After(entry.staleUntil.Add(15*time.Minute)) {
			delete(s.dashboardCache, key)
		}
	}
	for len(s.dashboardCache) > dashboardSnapshotMaxEntries {
		var oldestKey string
		var oldest time.Time
		for key, entry := range s.dashboardCache {
			if entry.inFlight != nil {
				continue
			}
			if oldestKey == "" || entry.staleUntil.Before(oldest) {
				oldestKey = key
				oldest = entry.staleUntil
			}
		}
		if oldestKey == "" {
			return
		}
		delete(s.dashboardCache, oldestKey)
	}
}

func (s *Server) dashboardCacheEntryCount() int {
	s.dashboardCacheMu.Lock()
	defer s.dashboardCacheMu.Unlock()
	return len(s.dashboardCache)
}

func dashboardSnapshotCacheKey(o storage.ListOptions, bucketSeconds int64) (string, storage.ListOptions) {
	cacheOptions := o
	from, fromErr := time.Parse(time.RFC3339, o.From)
	to, toErr := time.Parse(time.RFC3339, o.To)
	if fromErr == nil && toErr == nil && to.After(from) {
		cadence := dashboardSnapshotCadence(bucketSeconds)
		if absDuration(time.Since(to)) <= 2*cadence {
			normalizedTo := to.UTC().Truncate(cadence)
			cacheOptions.From = normalizedTo.Add(-to.Sub(from)).Format(time.RFC3339Nano)
			cacheOptions.To = normalizedTo.Format(time.RFC3339Nano)
		}
	}
	key := fmt.Sprintf("instance=%q|agent=%q|status=%q|from=%q|to=%q|bucket=%d|limit=%d", cacheOptions.InstanceID, cacheOptions.AgentID, cacheOptions.Status, cacheOptions.From, cacheOptions.To, bucketSeconds, cacheOptions.Limit)
	return key, cacheOptions
}

// costTrendOptions scopes the cost-trend module to the dashboard's requested
// range. Windows of two days or less use hourly periods so short filters (1h/
// 6h/24h) produce a readable trend instead of a single aggregate day bucket.
// Requests without a parsable range fall back to the legacy rolling 7-day window.
func costTrendOptions(o storage.ListOptions) (storage.ListOptions, string) {
	from, fromErr := time.Parse(time.RFC3339, o.From)
	to, toErr := time.Parse(time.RFC3339, o.To)
	if fromErr == nil && toErr == nil && to.After(from) {
		if to.Sub(from) <= 48*time.Hour {
			return o, "hour"
		}
		return o, "day"
	}
	now := time.Now().UTC()
	window := o
	window.To = now.Format(time.RFC3339Nano)
	window.From = now.AddDate(0, 0, -6).Format(time.RFC3339Nano)
	return window, "day"
}

func dashboardSnapshotCadence(bucketSeconds int64) time.Duration {
	switch {
	case bucketSeconds <= 60:
		return 15 * time.Second
	case bucketSeconds <= 300:
		return 30 * time.Second
	default:
		return time.Minute
	}
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func (s *Server) buildDashboardSnapshot(ctx context.Context, o storage.ListOptions, bucket string, bucketSeconds int64, fallback map[string]any) map[string]any {
	payload := s.emptyDashboard(o, bucket, bucketSeconds, "fresh")
	delete(payload, "degraded")
	degraded := make(map[string]string)
	payload["status"] = s.liveSummaryData()

	s.dashboardModule(ctx, payload, fallback, degraded, "timeseries", emptyTimeSeries(o, bucket, bucketSeconds), func(ctx context.Context) (any, error) {
		value, err := s.analyticsRepo.TimeSeries(ctx, o, bucketSeconds)
		if err == nil {
			value["bucket"] = bucket
		}
		return value, err
	})
	s.dashboardModule(ctx, payload, fallback, degraded, "models", emptyRows(), func(ctx context.Context) (any, error) { return s.analyticsRepo.ModelStats(ctx, o) })
	s.dashboardModule(ctx, payload, fallback, degraded, "tools", emptyRows(), func(ctx context.Context) (any, error) { return s.analyticsRepo.ToolStats(ctx, o) })
	s.dashboardModule(ctx, payload, fallback, degraded, "agents", emptyRows(), func(ctx context.Context) (any, error) { return s.analyticsRepo.AgentStats(ctx, o) })
	s.dashboardModule(ctx, payload, fallback, degraded, "agentModels", emptyRows(), func(ctx context.Context) (any, error) { return s.analyticsRepo.AgentModelStats(ctx, o) })
	s.dashboardModule(ctx, payload, fallback, degraded, "lifetime", emptyLifetime(), func(ctx context.Context) (any, error) { return s.analyticsRepo.LifetimeStats(ctx) })
	s.dashboardModule(ctx, payload, fallback, degraded, "sessions", emptyRows(), func(ctx context.Context) (any, error) { return s.analyticsRepo.ListSessions(ctx, o) })
	s.dashboardModule(ctx, payload, fallback, degraded, "llmCalls", emptyRows(), func(ctx context.Context) (any, error) { return s.analyticsRepo.ListLLMCalls(ctx, o) })
	s.dashboardModule(ctx, payload, fallback, degraded, "errors", emptyRows(), func(ctx context.Context) (any, error) { return s.analyticsRepo.ErrorStats(ctx, o) })
	s.dashboardModule(ctx, payload, fallback, degraded, "anomalies", emptyRows(), func(ctx context.Context) (any, error) { return s.analyticsRepo.RecentAnomalies(ctx, o) })
	s.dashboardModule(ctx, payload, fallback, degraded, "subagents", emptyRows(), func(ctx context.Context) (any, error) { return s.analyticsRepo.ListSubagentRuns(ctx, o) })
	s.dashboardModule(ctx, payload, fallback, degraded, "mcpCalls", emptyRows(), func(ctx context.Context) (any, error) { return s.analyticsRepo.ListMCPCalls(ctx, o) })

	now := time.Now().UTC()
	costOpts, costPeriod := costTrendOptions(o)
	s.dashboardModule(ctx, payload, fallback, degraded, "costTrends", emptyRows(), func(ctx context.Context) (any, error) { return s.analyticsRepo.CostTrends(ctx, costOpts, costPeriod) })
	s.dashboardModule(ctx, payload, fallback, degraded, "costSummary", emptyCostSummary(), func(ctx context.Context) (any, error) { return s.analyticsRepo.CostSummary(ctx, o) })
	cost30d := o
	cost30d.To = now.Format(time.RFC3339Nano)
	cost30d.From = now.AddDate(0, 0, -30).Format(time.RFC3339Nano)
	s.dashboardModule(ctx, payload, fallback, degraded, "costTrends30d", emptyRows(), func(ctx context.Context) (any, error) { return s.analyticsRepo.CostTrends(ctx, cost30d, "day") })

	if len(degraded) > 0 {
		payload["degraded"] = degraded
		payload["snapshot"] = map[string]any{"state": "degraded", "generatedAt": now}
	} else {
		payload["snapshot"] = map[string]any{"state": "fresh", "generatedAt": now}
	}
	return payload
}

func (s *Server) dashboardModule(ctx context.Context, payload, fallback map[string]any, degraded map[string]string, name string, empty any, load func(context.Context) (any, error)) {
	value, err := load(ctx)
	if err == nil {
		payload[name] = value
		return
	}
	s.dashboardStats.add(&s.dashboardStats.moduleFailures)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		s.dashboardStats.add(&s.dashboardStats.cancellations)
	}
	if previous, ok := fallback[name]; ok && previous != nil {
		payload[name] = previous
		degraded[name] = "stale"
		return
	}
	payload[name] = empty
	degraded[name] = "unavailable"
}

func (s *Server) emptyDashboard(o storage.ListOptions, bucket string, bucketSeconds int64, state string) map[string]any {
	return map[string]any{
		"status":        s.liveSummaryData(),
		"timeseries":    emptyTimeSeries(o, bucket, bucketSeconds),
		"models":        emptyRows(),
		"tools":         emptyRows(),
		"agents":        emptyRows(),
		"agentModels":   emptyRows(),
		"lifetime":      emptyLifetime(),
		"sessions":      emptyRows(),
		"llmCalls":      emptyRows(),
		"errors":        emptyRows(),
		"anomalies":     emptyRows(),
		"subagents":     emptyRows(),
		"mcpCalls":      emptyRows(),
		"costTrends":    emptyRows(),
		"costSummary":   emptyCostSummary(),
		"costTrends30d": emptyRows(),
		"degraded":      map[string]string{"dashboard": state},
		"snapshot":      map[string]any{"state": state, "generatedAt": time.Now().UTC()},
	}
}

func emptyRows() []map[string]any { return []map[string]any{} }

func emptyTimeSeries(o storage.ListOptions, bucket string, bucketSeconds int64) map[string]any {
	return map[string]any{
		"bucket":        bucket,
		"bucketSeconds": bucketSeconds,
		"from":          o.From,
		"to":            o.To,
		"points":        emptyRows(),
		"models":        emptyRows(),
		"agents":        emptyRows(),
		"tools":         emptyRows(),
	}
}

func emptyLifetime() map[string]any {
	return map[string]any{"totalRequests": 0, "totalTokens": 0, "totalCostUsd": 0}
}

func emptyCostSummary() map[string]any {
	return map[string]any{"totalRequests": 0, "totalCost": 0, "totalTokens": 0}
}

func mustJSON(value any) []byte {
	body, err := json.Marshal(value)
	if err != nil {
		return []byte(`{"data":{"degraded":{"dashboard":"encoding_failed"}}}`)
	}
	return body
}
