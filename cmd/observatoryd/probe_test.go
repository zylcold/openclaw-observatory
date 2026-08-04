package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zylcold/openclaw-observatory/internal/server"
	"github.com/zylcold/openclaw-observatory/internal/storage"
)

func TestProbeGatewayRecordsHTTPResponsiveness(t *testing.T) {
	repo, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	srv := server.New(repo, slog.Default())
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
			Request:    request,
		}, nil
	})}
	probeGatewayOnce(context.Background(), client, srv, "http://127.0.0.1:18789/health", slog.Default())
	probe := srv.GatewayProbe()
	if !probe.Responsive || probe.StatusCode != http.StatusOK || probe.LastSuccessAt == nil {
		t.Fatalf("unexpected probe snapshot: %#v", probe)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
