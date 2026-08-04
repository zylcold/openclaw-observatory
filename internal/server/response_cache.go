package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	responseCacheFreshTTL       = time.Minute
	responseCacheStaleTTL       = 5 * time.Minute
	responseCacheRefreshTimeout = 25 * time.Second
	responseCacheColdWait       = 500 * time.Millisecond
	responseCacheRetryDelay     = 30 * time.Second
)

// swrResponseCache stores a completed HTTP response, so cache hits do not
// re-encode JSON or rerun full aggregate queries.
type swrResponseCache struct {
	mu          sync.Mutex
	body        []byte
	contentType string
	freshUntil  time.Time
	staleUntil  time.Time
	nextRetryAt time.Time
	inFlight    chan struct{}
}

type capturedResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newCapturedResponseWriter() *capturedResponseWriter {
	return &capturedResponseWriter{header: make(http.Header)}
}

func (w *capturedResponseWriter) Header() http.Header { return w.header }

func (w *capturedResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *capturedResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	s.serveCachedResponse(w, r, &s.statusCache, "X-Observatory-Status-Cache", statusFallback(s), func(ctx context.Context) ([]byte, string, error) {
		return captureHandlerResponse(r, ctx, s.statusUncached)
	})
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	s.serveCachedResponse(w, r, &s.metricsCache, "X-Observatory-Metrics-Cache", metricsFallback(), func(ctx context.Context) ([]byte, string, error) {
		return captureHandlerResponse(r, ctx, s.metricsUncached)
	})
}

func (s *Server) serveCachedResponse(w http.ResponseWriter, r *http.Request, cache *swrResponseCache, headerName string, fallback []byte, build func(context.Context) ([]byte, string, error)) {
	now := time.Now()
	cache.mu.Lock()
	if len(cache.body) > 0 && now.Before(cache.staleUntil) {
		if now.Before(cache.freshUntil) {
			body, contentType := cache.body, cache.contentType
			cache.mu.Unlock()
			writeCachedResponse(w, headerName, "HIT", contentType, body)
			return
		}
		if cache.inFlight == nil && !now.Before(cache.nextRetryAt) {
			s.startCachedResponseRefreshLocked(cache, build)
		}
		body, contentType := cache.body, cache.contentType
		cache.mu.Unlock()
		writeCachedResponse(w, headerName, "STALE", contentType, body)
		return
	}
	if cache.inFlight == nil && !now.Before(cache.nextRetryAt) {
		s.startCachedResponseRefreshLocked(cache, build)
	}
	flight := cache.inFlight
	cache.mu.Unlock()

	if flight != nil {
		timer := time.NewTimer(responseCacheColdWait)
		defer timer.Stop()
		select {
		case <-flight:
			cache.mu.Lock()
			body, contentType := cache.body, cache.contentType
			valid := len(body) > 0 && time.Now().Before(cache.staleUntil)
			cache.mu.Unlock()
			if valid {
				writeCachedResponse(w, headerName, "MISS", contentType, body)
				return
			}
		case <-r.Context().Done():
		case <-timer.C:
		}
	}
	writeCachedResponse(w, headerName, "WARMING", fallbackContentType(fallback), fallback)
}

func (s *Server) startCachedResponseRefreshLocked(cache *swrResponseCache, build func(context.Context) ([]byte, string, error)) {
	cache.inFlight = make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), responseCacheRefreshTimeout)
		defer cancel()
		body, contentType, err := build(ctx)
		cache.mu.Lock()
		defer cache.mu.Unlock()
		now := time.Now()
		if err == nil && len(body) > 0 {
			cache.body = append([]byte(nil), body...)
			cache.contentType = contentType
			cache.freshUntil = now.Add(responseCacheFreshTTL)
			cache.staleUntil = now.Add(responseCacheStaleTTL)
			cache.nextRetryAt = time.Time{}
		} else {
			cache.nextRetryAt = now.Add(responseCacheRetryDelay)
			s.log.Warn("cached response refresh failed", "error", err)
		}
		close(cache.inFlight)
		cache.inFlight = nil
	}()
}

func captureHandlerResponse(r *http.Request, ctx context.Context, handler func(http.ResponseWriter, *http.Request)) ([]byte, string, error) {
	capture := newCapturedResponseWriter()
	handler(capture, r.Clone(ctx))
	if capture.status == 0 {
		capture.status = http.StatusOK
	}
	if capture.status != http.StatusOK {
		return nil, "", fmt.Errorf("cached handler returned HTTP %d", capture.status)
	}
	contentType := capture.Header().Get("Content-Type")
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	return append([]byte(nil), capture.body.Bytes()...), contentType, nil
}

func writeCachedResponse(w http.ResponseWriter, headerName, state, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set(headerName, state)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func statusFallback(s *Server) []byte {
	return mustJSON(map[string]any{"data": s.liveSummaryData()})
}

func metricsFallback() []byte {
	return []byte("# HELP openclaw_observatory_metrics_warming Whether the full metrics snapshot is still warming.\n# TYPE openclaw_observatory_metrics_warming gauge\nopenclaw_observatory_metrics_warming 1\n")
}

func fallbackContentType(body []byte) string {
	if len(body) > 0 && body[0] == '{' {
		return "application/json; charset=utf-8"
	}
	return "text/plain; version=0.0.4; charset=utf-8"
}
