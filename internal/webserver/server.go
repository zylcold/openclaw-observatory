package webserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Root       string
	BackendURL string
	BuildID    string
}

func New(config Config) (http.Handler, error) {
	root, err := filepath.Abs(config.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve web root: %w", err)
	}
	backend, err := url.Parse(config.BackendURL)
	if err != nil || backend.Scheme == "" || backend.Host == "" {
		return nil, fmt.Errorf("invalid backend URL %q", config.BackendURL)
	}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.FlushInterval = -1
	proxy.ModifyResponse = func(response *http.Response) error {
		for _, header := range []string{"Content-Security-Policy", "Referrer-Policy", "X-Content-Type-Options", "X-Frame-Options"} {
			response.Header.Del(header)
		}
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, proxyErr error) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
			"code": "backend_unavailable", "message": proxyErr.Error(),
		}})
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", proxy)
	for _, path := range []string{"/health", "/ready", "/metrics"} {
		mux.Handle(path, proxy)
	}
	mux.Handle("/", staticHandler(root))
	return securityHeaders(mux, config.BuildID), nil
}

func staticHandler(root string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+r.URL.Path)), "/")
		if rel == "" {
			rel = "index.html"
		}
		fullPath := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(fullPath)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		if rel == "index.html" || strings.HasSuffix(rel, ".html") {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		} else if strings.HasPrefix(rel, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFile(w, r, fullPath)
	})
}

func securityHeaders(next http.Handler, buildID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'")
		w.Header().Set("X-Observatory-Web-Service-Build", buildID)
		next.ServeHTTP(w, r)
	})
}
