package webserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServesFrontendAndProxiesBackend(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("new frontend"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app-123.js"), []byte("export {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"path":"`+r.URL.Path+`"}`)
	}))
	defer backend.Close()
	handler, err := New(Config{Root: root, BackendURL: backend.URL, BuildID: "web-test-build"})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		path        string
		body        string
		cachePrefix string
	}{
		{path: "/", body: "new frontend", cachePrefix: "no-cache"},
		{path: "/assets/app-123.js", body: "export {}", cachePrefix: "public"},
		{path: "/api/v1/status", body: `{"path":"/api/v1/status"}`},
		{path: "/metrics", body: `{"path":"/metrics"}`},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != tc.body {
			t.Fatalf("%s: status=%d body=%q", tc.path, res.Code, res.Body.String())
		}
		if tc.cachePrefix != "" && !strings.HasPrefix(res.Header().Get("Cache-Control"), tc.cachePrefix) {
			t.Fatalf("%s: unexpected cache control %q", tc.path, res.Header().Get("Cache-Control"))
		}
		if res.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("%s: security headers missing", tc.path)
		}
		if res.Header().Get("X-Observatory-Web-Service-Build") != "web-test-build" {
			t.Fatalf("%s: web service build header missing", tc.path)
		}
	}
}
