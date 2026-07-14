package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func TestMetricsExposeWriteTimings(t *testing.T) {
	repo, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	srv := New(repo, slog.Default())
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
	} {
		if !strings.Contains(res.Body.String(), metric) {
			t.Fatalf("metric %q missing from response:\n%s", metric, res.Body.String())
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
	srv.PublicHandler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/ready", nil))
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
