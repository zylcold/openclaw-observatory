package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zylcold/openclaw-observatory/internal/event"
	"github.com/zylcold/openclaw-observatory/internal/storage"
)

func TestStatusAdvertisesFrontendCompatibility(t *testing.T) {
	repo, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	res := httptest.NewRecorder()
	New(repo, slog.Default()).PublicHandler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", res.Code, res.Body.String())
	}
	var body struct {
		Data struct {
			APIVersion    int      `json:"apiVersion"`
			SchemaVersion int      `json:"schemaVersion"`
			Capabilities  []string `json:"capabilities"`
			BuildID       string   `json:"buildId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.APIVersion != APIVersion || body.Data.SchemaVersion != storage.CurrentSchemaVersion || body.Data.BuildID == "" {
		t.Fatalf("missing compatibility metadata: %#v", body.Data)
	}
	if len(body.Data.Capabilities) != len(Capabilities) || body.Data.Capabilities[0] != "agent-stats-v3" {
		t.Fatalf("unexpected capabilities: %#v", body.Data.Capabilities)
	}
}

func TestDashboardSnapshotNormalizesLiveRangeAndIgnoresHeartbeatInvalidation(t *testing.T) {
	repo, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	srv := New(repo, slog.Default())
	handler := srv.PublicHandler()

	anchor := time.Now().UTC().Truncate(30 * time.Second).Add(5 * time.Second)
	requestFor := func(to time.Time) *httptest.ResponseRecorder {
		values := url.Values{
			"from":   {to.Add(-time.Hour).Format(time.RFC3339Nano)},
			"to":     {to.Format(time.RFC3339Nano)},
			"bucket": {"1m"},
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/snapshot?"+values.Encode(), nil))
		return res
	}

	first := requestFor(anchor)
	if first.Code != http.StatusOK {
		t.Fatalf("unexpected first snapshot response: %d body=%s", first.Code, first.Body.String())
	}
	// The first request may return a bounded warming response on a busy CI host.
	// Wait briefly for its one background build instead of issuing a duplicate.
	deadline := time.Now().Add(2 * time.Second)
	for first.Header().Get("X-Observatory-Dashboard-Cache") == "WARMING" {
		if time.Now().After(deadline) {
			t.Fatal("snapshot did not finish its background build")
		}
		time.Sleep(25 * time.Millisecond)
		first = requestFor(anchor)
	}

	second := requestFor(anchor.Add(5 * time.Second))
	if got := second.Header().Get("X-Observatory-Dashboard-Cache"); got != "HIT" {
		t.Fatalf("moving live range missed normalized cache: got %q body=%s", got, second.Body.String())
	}
	if err := srv.Insert(t.Context(), []event.Event{{
		SchemaVersion: 1, EventID: "10000000-0000-4000-8000-000000000777", EventType: "gateway.heartbeat",
		OccurredAt: time.Now().UTC(), InstanceID: "test", ProducerID: "test", Sequence: 1, Source: "test", Payload: json.RawMessage(`{"queueDepth":1,"queueCapacity":10}`),
	}}); err != nil {
		t.Fatal(err)
	}
	afterHeartbeat := requestFor(anchor.Add(5 * time.Second))
	if got := afterHeartbeat.Header().Get("X-Observatory-Dashboard-Cache"); got != "HIT" {
		t.Fatalf("heartbeat should not invalidate dashboard snapshot: got %q", got)
	}
	if err := srv.Insert(t.Context(), []event.Event{{
		SchemaVersion: 1, EventID: "10000000-0000-4000-8000-000000000778", EventType: "resource.sampled",
		OccurredAt: time.Now().UTC(), InstanceID: "test", ProducerID: "test", Sequence: 2, Source: "daemon", Payload: json.RawMessage(`{"cpuSecondsTotal":1,"residentMemoryBytes":1024,"diskTotalBytes":2048,"diskAvailableBytes":1024}`),
	}}); err != nil {
		t.Fatal(err)
	}
	afterResource := requestFor(anchor.Add(5 * time.Second))
	if got := afterResource.Header().Get("X-Observatory-Dashboard-Cache"); got != "HIT" {
		t.Fatalf("resource sample should not invalidate dashboard snapshot: got %q", got)
	}
}

func TestMetricsExposeWriteTimings(t *testing.T) {
	repo, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	srv := New(repo, slog.Default())
	srv.RecordGatewayProbe(125*time.Millisecond, http.StatusOK, nil)
	if err := srv.Insert(t.Context(), []event.Event{{
		SchemaVersion: 1, EventID: "10000000-0000-4000-8000-000000000001", EventType: "gateway.heartbeat",
		OccurredAt: time.Now().UTC(), InstanceID: "test", ProducerID: "test", Sequence: 1, Source: "test", Payload: json.RawMessage(`{"queueDepth":42,"queueCapacity":100}`),
	}}); err != nil {
		t.Fatal(err)
	}
	status := httptest.NewRecorder()
	srv.PublicHandler().ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if status.Code != http.StatusOK {
		t.Fatalf("unexpected status response: %d", status.Code)
	}
	res := httptest.NewRecorder()
	srv.PublicHandler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected metrics status: %d", res.Code)
	}
	for _, metric := range []string{
		"openclaw_monitor_insert_duration_seconds_count 1",
		"openclaw_monitor_reduce_duration_seconds_count 1",
		"openclaw_monitor_commit_duration_seconds_count 1",
		"openclaw_monitor_event_queue_depth{instance=\"test\"} 42",
		"openclaw_monitor_query_duration_seconds_count 1",
		"openclaw_gateway_responsive{instance=\"test\"} 1",
		"openclaw_gateway_response_duration_seconds{instance=\"test\"} 0.125",
		"openclaw_gateway_probe_consecutive_failures{instance=\"test\"} 0",
		"openclaw_gateway_heartbeat_age_seconds{instance=\"test\"}",
		"# TYPE openclaw_llm_tokens_by_agent_model_total counter",
		"# TYPE openclaw_llm_cost_usd_by_agent_model_total counter",
		"# TYPE openclaw_llm_tokens_by_agent_model counter",
	} {
		if !strings.Contains(res.Body.String(), metric) {
			t.Fatalf("metric %q missing from response:\n%s", metric, res.Body.String())
		}
	}
}

func TestGatewayProbeFailureIsExposedInStatusAndMetrics(t *testing.T) {
	repo, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	srv := New(repo, slog.Default())
	if err := srv.Insert(t.Context(), []event.Event{{
		SchemaVersion: 1, EventID: "10000000-0000-4000-8000-000000000099", EventType: "gateway.heartbeat",
		OccurredAt: time.Now().UTC(), InstanceID: "test", ProducerID: "test", Sequence: 1, Source: "test", Payload: json.RawMessage(`{}`),
	}}); err != nil {
		t.Fatal(err)
	}
	srv.RecordGatewayProbe(3*time.Second, 0, context.DeadlineExceeded)

	status := httptest.NewRecorder()
	srv.PublicHandler().ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if !strings.Contains(status.Body.String(), `"gatewayProbe":{"responsive":false`) {
		t.Fatalf("status did not expose the failed probe: %s", status.Body.String())
	}

	metrics := httptest.NewRecorder()
	srv.PublicHandler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, metric := range []string{
		`openclaw_gateway_responsive{instance="test"} 0`,
		`openclaw_gateway_response_duration_seconds{instance="test"} 3`,
		`openclaw_gateway_probe_consecutive_failures{instance="test"} 1`,
	} {
		if !strings.Contains(metrics.Body.String(), metric) {
			t.Fatalf("metric %q missing from response:\n%s", metric, metrics.Body.String())
		}
	}
}

func TestMetricsExposeAgentModelAndCacheDimensions(t *testing.T) {
	repo, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	srv := New(repo, slog.Default())
	now := time.Now().UTC()
	events := []event.Event{
		{
			SchemaVersion: 1, EventID: "20000000-0000-4000-8000-000000000001", EventType: "session.started",
			OccurredAt: now, InstanceID: "test", ProducerID: "test", Sequence: 1, Source: "test",
			Payload: json.RawMessage(`{"sessionId":"session-metrics","agentId":"tom"}`),
		},
		{
			SchemaVersion: 1, EventID: "20000000-0000-4000-8000-000000000002", EventType: "agent.started",
			OccurredAt: now, InstanceID: "test", ProducerID: "test", Sequence: 2, Source: "test",
			Payload: json.RawMessage(`{"runId":"run-metrics","sessionId":"session-metrics","agentId":"tom"}`),
		},
		{
			SchemaVersion: 1, EventID: "20000000-0000-4000-8000-000000000003", EventType: "llm.completed",
			OccurredAt: now, InstanceID: "test", ProducerID: "test", Sequence: 3, Source: "test",
			Payload: json.RawMessage(`{"callId":"call-metrics","runId":"run-metrics","sessionId":"session-metrics","provider":"bailian","model":"qwen","inputTokens":2,"outputTokens":3,"cacheReadTokens":4,"cacheWriteTokens":5,"costUsd":0.25}`),
		},
	}
	if err := srv.Insert(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	srv.PublicHandler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected metrics status: %d", res.Code)
	}
	for _, metric := range []string{
		`openclaw_llm_tokens_total{direction="cache_read",instance="test",model="qwen",provider="bailian"} 4`,
		`openclaw_llm_tokens_total{direction="cache_write",instance="test",model="qwen",provider="bailian"} 5`,
		`openclaw_llm_tokens_by_agent_model_total{agentId="tom",instance="test",model="qwen"} 14`,
		`openclaw_llm_cost_usd_by_agent_model_total{agentId="tom",instance="test",model="qwen"} 0.25`,
	} {
		if !strings.Contains(res.Body.String(), metric) {
			t.Fatalf("metric %q missing from response:\n%s", metric, res.Body.String())
		}
	}
}

func TestMetricsEstimateMissingCostFromOpenRouterCatalog(t *testing.T) {
	repo, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	srv := New(repo, slog.Default())
	now := time.Now().UTC()
	events := []event.Event{
		{
			SchemaVersion: 1, EventID: "30000000-0000-4000-8000-000000000001", EventType: "agent.started",
			OccurredAt: now, InstanceID: "test", ProducerID: "test", Sequence: 1, Source: "test",
			Payload: json.RawMessage(`{"runId":"run-priced","sessionId":"session-priced","agentId":"tom"}`),
		},
		{
			SchemaVersion: 1, EventID: "30000000-0000-4000-8000-000000000002", EventType: "llm.completed",
			OccurredAt: now, InstanceID: "test", ProducerID: "test", Sequence: 2, Source: "test",
			Payload: json.RawMessage(`{"callId":"call-priced","runId":"run-priced","sessionId":"session-priced","provider":"bailian","model":"qwen3.7-plus","inputTokens":1000000,"outputTokens":1000000,"costUsd":0}`),
		},
	}
	if err := srv.Insert(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	srv.PublicHandler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected metrics status: %d", res.Code)
	}
	for _, metric := range []string{
		`openclaw_llm_cost_usd_total{instance="test",model="qwen3.7-plus",provider="bailian"} 1.6`,
		`openclaw_llm_cost_usd_24h{instance="test",model="qwen3.7-plus",provider="bailian"} 1.6`,
		`openclaw_llm_cost_usd_by_agent_model_total{agentId="tom",instance="test",model="qwen3.7-plus"} 1.6`,
		`openclaw_llm_cost_usd_by_agent_model_24h{agentId="tom",instance="test",model="qwen3.7-plus"} 1.6`,
		`openclaw_llm_cost_by_agent_model{agentId="tom",model="qwen3.7-plus"} 1.6`,
	} {
		if !strings.Contains(res.Body.String(), metric) {
			t.Fatalf("estimated metric %q missing from response:\n%s", metric, res.Body.String())
		}
	}
}

func TestReadyChecksSQLiteWriteTransactionAndReportsEventDelay(t *testing.T) {
	repo, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	srv := New(repo, slog.Default())
	if err := srv.Insert(t.Context(), []event.Event{{
		SchemaVersion: 1, EventID: "10000000-0000-4000-8000-000000000002", EventType: "gateway.heartbeat",
		OccurredAt: time.Now().UTC(), InstanceID: "test", ProducerID: "test", Sequence: 1, Source: "test", Payload: json.RawMessage(`{}`),
	}}); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	srv.PublicHandler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/ready/write", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected ready status: %d body=%s", res.Code, res.Body.String())
	}
	var body struct {
		Status              string  `json:"status"`
		LastEventReceivedAt string  `json:"lastEventReceivedAt"`
		EventDelaySeconds   float64 `json:"eventDelaySeconds"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ready" || body.LastEventReceivedAt == "" || body.EventDelaySeconds < 0 {
		t.Fatalf("unexpected readiness response: %#v", body)
	}
}

func TestV3AnalyticsRoutes(t *testing.T) {
	repo, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	handler := New(repo, slog.Default()).PublicHandler()
	for _, path := range []string{
		"/api/v1/dashboard?from=2026-07-10T00:00:00Z&to=2026-07-11T00:00:00Z&bucket=1h",
		"/api/v1/agents/stats?from=2026-07-10T00:00:00Z&to=2026-07-11T00:00:00Z",
		"/api/v1/subagents",
		"/api/v1/mcp/calls",
		"/api/v1/llm/calls",
		"/api/v1/errors/stats",
		"/api/v1/timeseries?from=2026-07-10T00:00:00Z&to=2026-07-11T00:00:00Z&bucket=1h",
	} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", path, res.Code, res.Body.String())
		}
	}

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard?from=2026-07-10T00:00:00Z&to=2026-07-11T00:00:00Z&bucket=1h", nil))
	var dashboard struct {
		Data struct {
			Status struct {
				APIVersion   int      `json:"apiVersion"`
				Capabilities []string `json:"capabilities"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if dashboard.Data.Status.APIVersion != APIVersion || len(dashboard.Data.Status.Capabilities) == 0 {
		t.Fatalf("dashboard status lacks compatibility metadata: %#v", dashboard.Data.Status)
	}

	res = httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/timeseries?bucket=2m", nil))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("invalid bucket returned %d: %s", res.Code, res.Body.String())
	}
}

func TestCostTrendOptionsFollowsRequestedRange(t *testing.T) {
	base := storage.ListOptions{InstanceID: "i", AgentID: "a"}
	parse := func(from, to string) storage.ListOptions {
		o := base
		o.From, o.To = from, to
		return o
	}
	now := time.Now().UTC()
	cases := []struct {
		name   string
		from   time.Time
		to     time.Time
		period string
	}{
		{"1h filter → hour", now.Add(-1 * time.Hour), now, "hour"},
		{"6h filter → hour", now.Add(-6 * time.Hour), now, "hour"},
		{"24h filter → hour", now.Add(-24 * time.Hour), now, "hour"},
		{"48h boundary → hour", now.Add(-48 * time.Hour), now, "hour"},
		{"7d filter → day", now.AddDate(0, 0, -7), now, "day"},
		{"30d filter → day", now.AddDate(0, 0, -30), now, "day"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := parse(tc.from.Format(time.RFC3339), tc.to.Format(time.RFC3339))
			got, period := costTrendOptions(o)
			if period != tc.period {
				t.Fatalf("period = %q, want %q", period, tc.period)
			}
			if got.From != o.From || got.To != o.To {
				t.Fatalf("range was rewritten: got %s..%s, want %s..%s", got.From, got.To, o.From, o.To)
			}
			if got.InstanceID != base.InstanceID || got.AgentID != base.AgentID {
				t.Fatalf("filters dropped: %+v", got)
			}
		})
	}
	t.Run("unparsable range falls back to rolling 7d", func(t *testing.T) {
		got, period := costTrendOptions(base)
		if period != "day" {
			t.Fatalf("period = %q, want day", period)
		}
		to, err := time.Parse(time.RFC3339Nano, got.To)
		if err != nil || time.Since(to) > time.Minute {
			t.Fatalf("fallback To not anchored to now: %v (%v)", got.To, err)
		}
		from, err := time.Parse(time.RFC3339Nano, got.From)
		if err != nil || to.Sub(from).Round(time.Hour) != 6*24*time.Hour {
			t.Fatalf("fallback window not 6 days: %v (%v)", to.Sub(from), err)
		}
	})
}
