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
	if len(body.Data.Capabilities) != 1 || body.Data.Capabilities[0] != "agent-timeline-v2" {
		t.Fatalf("unexpected capabilities: %#v", body.Data.Capabilities)
	}
}
