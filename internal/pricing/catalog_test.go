package pricing

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshResolveEstimateAndCache(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"qwen/qwen3.7-plus","pricing":{"prompt":"0.000001","completion":"0.000002","input_cache_read":"0.0000001","input_cache_write":"0.000003"}}]}`)),
			Header:     make(http.Header),
		}, nil
	})}

	cachePath := filepath.Join(t.TempDir(), "pricing.json")
	catalog := NewCatalog()
	if err := catalog.Refresh(context.Background(), client, "https://example.test/models", "", cachePath); err != nil {
		t.Fatal(err)
	}
	got, ok := catalog.Estimate("bailian", "qwen3.7-plus", 10, 20, 30, 40)
	if !ok || got != 0.000173 {
		t.Fatalf("unexpected estimate: %v ok=%v", got, ok)
	}

	loaded := NewCatalog()
	if err := loaded.Load(cachePath); err != nil {
		t.Fatal(err)
	}
	if got, ok := loaded.Resolve("bailian", "qwen3.7-plus"); !ok || got.Prompt != 0.000001 {
		t.Fatalf("cached price not loaded: %#v ok=%v", got, ok)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestFallbackAndUnknown(t *testing.T) {
	catalog := NewCatalog()
	if _, ok := catalog.Resolve("zai", "glm-5.2"); !ok {
		t.Fatal("expected GLM fallback price")
	}
	if _, ok := catalog.Resolve("bailian", "glm-4.7"); !ok {
		t.Fatal("expected model-based fallback when OpenClaw provider differs")
	}
	if _, ok := catalog.Resolve("unknown", "missing"); ok {
		t.Fatal("unexpected price for unknown model")
	}
}
