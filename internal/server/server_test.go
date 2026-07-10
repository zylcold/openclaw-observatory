package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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

func TestV3AnalyticsRoutes(t *testing.T) {
	repo, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	handler := New(repo, slog.Default()).PublicHandler()
	for _, path := range []string{
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
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/timeseries?bucket=2m", nil))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("invalid bucket returned %d: %s", res.Code, res.Body.String())
	}
}
