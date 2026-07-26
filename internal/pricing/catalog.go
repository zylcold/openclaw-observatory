package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultModelsURL = "https://openrouter.ai/api/v1/models"

type Price struct {
	Prompt     float64 `json:"prompt"`
	Completion float64 `json:"completion"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

type cacheFile struct {
	UpdatedAt time.Time        `json:"updatedAt"`
	Prices    map[string]Price `json:"prices"`
}

type Catalog struct {
	mu        sync.RWMutex
	prices    map[string]Price
	updatedAt time.Time
}

func NewCatalog() *Catalog {
	return &Catalog{prices: fallbackPrices()}
}

func (c *Catalog) Resolve(provider, model string) (Price, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, key := range lookupKeys(provider, model) {
		if price, ok := c.prices[key]; ok {
			return price, true
		}
	}
	return Price{}, false
}

func (c *Catalog) Estimate(provider, model string, input, output, cacheRead, cacheWrite float64) (float64, bool) {
	price, ok := c.Resolve(provider, model)
	if !ok {
		return 0, false
	}
	cacheReadPrice := price.CacheRead
	if cacheReadPrice == 0 {
		cacheReadPrice = price.Prompt
	}
	cacheWritePrice := price.CacheWrite
	if cacheWritePrice == 0 {
		cacheWritePrice = price.Prompt
	}
	return input*price.Prompt + output*price.Completion + cacheRead*cacheReadPrice + cacheWrite*cacheWritePrice, true
}

func (c *Catalog) Status() (models int, updatedAt time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.prices), c.updatedAt
}

func (c *Catalog) Load(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cached cacheFile
	if err := json.Unmarshal(body, &cached); err != nil {
		return err
	}
	if len(cached.Prices) == 0 {
		return errors.New("pricing cache is empty")
	}
	c.replace(cached.Prices, cached.UpdatedAt)
	return nil
}

func (c *Catalog) Refresh(ctx context.Context, client *http.Client, modelsURL, apiKey, cachePath string) error {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenRouter models API returned %s", res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return err
	}
	prices, err := parseModels(body)
	if err != nil {
		return err
	}
	updatedAt := time.Now().UTC()
	c.replace(prices, updatedAt)
	if cachePath != "" {
		return saveCache(cachePath, cacheFile{UpdatedAt: updatedAt, Prices: prices})
	}
	return nil
}

func (c *Catalog) replace(prices map[string]Price, updatedAt time.Time) {
	merged := fallbackPrices()
	for key, price := range prices {
		merged[key] = price
	}
	c.mu.Lock()
	c.prices = merged
	c.updatedAt = updatedAt
	c.mu.Unlock()
}

func parseModels(body []byte) (map[string]Price, error) {
	var response struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
				CacheRead  string `json:"input_cache_read"`
				CacheWrite string `json:"input_cache_write"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	prices := make(map[string]Price)
	for _, model := range response.Data {
		id := strings.ToLower(strings.TrimSpace(model.ID))
		if id == "" {
			continue
		}
		price := Price{
			Prompt:     parsePrice(model.Pricing.Prompt),
			Completion: parsePrice(model.Pricing.Completion),
			CacheRead:  parsePrice(model.Pricing.CacheRead),
			CacheWrite: parsePrice(model.Pricing.CacheWrite),
		}
		if price.Prompt == 0 && price.Completion == 0 {
			continue
		}
		prices[id] = price
		if parts := strings.SplitN(id, "/", 2); len(parts) == 2 {
			prices[normaliseProvider(parts[0])+"|"+parts[1]] = price
		}
	}
	if len(prices) == 0 {
		return nil, errors.New("OpenRouter models response contained no usable prices")
	}
	return prices, nil
}

func parsePrice(raw string) float64 {
	value, _ := strconv.ParseFloat(raw, 64)
	if value < 0 {
		return 0
	}
	return value
}

func lookupKeys(provider, model string) []string {
	provider = normaliseProvider(provider)
	model = strings.ToLower(strings.TrimSpace(model))
	key := provider + "|" + model
	keys := []string{key}
	if alias, ok := aliases[key]; ok {
		keys = append(keys, alias)
	}
	if alias, ok := modelAliases[model]; ok {
		keys = append(keys, alias)
	}
	return keys
}

func normaliseProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "qwen", "alibaba", "dashscope":
		return "bailian"
	case "z-ai", "zhipu":
		return "zai"
	case "xiaomi":
		return "mimo"
	default:
		return provider
	}
}

var aliases = map[string]string{
	"bailian|qwen3.7-plus":     "qwen/qwen3.7-plus",
	"bailian|qwen3-coder-plus": "qwen/qwen3-coder-plus",
	"bailian|qwen3-coder-next": "qwen/qwen3-coder-next",
	"bailian|qwen3-max":        "qwen/qwen3-max",
	"zai|glm-5.2":              "z-ai/glm-5.2",
	"zai|glm-5-turbo":          "z-ai/glm-5-turbo",
	"zai|glm-5":                "z-ai/glm-5",
	"zai|glm-4.7":              "z-ai/glm-4.7",
	"mimo|mimo-v2.5-pro":       "xiaomi/mimo-v2.5-pro",
	"mimo|mimo-v2.5":           "xiaomi/mimo-v2.5",
}

var modelAliases = map[string]string{
	"qwen3.7-plus":     "qwen/qwen3.7-plus",
	"qwen3-coder-plus": "qwen/qwen3-coder-plus",
	"qwen3-coder-next": "qwen/qwen3-coder-next",
	"qwen3-max":        "qwen/qwen3-max",
	"glm-5.2":          "z-ai/glm-5.2",
	"glm-5-turbo":      "z-ai/glm-5-turbo",
	"glm-5":            "z-ai/glm-5",
	"glm-4.7":          "z-ai/glm-4.7",
	"mimo-v2.5-pro":    "xiaomi/mimo-v2.5-pro",
	"mimo-v2.5":        "xiaomi/mimo-v2.5",
}

func fallbackPrices() map[string]Price {
	prices := map[string]Price{
		"bailian|qwen3.7-plus":     {Prompt: 0.00000032, Completion: 0.00000128, CacheRead: 0.000000064, CacheWrite: 0.0000004},
		"bailian|qwen3-coder-plus": {Prompt: 0.00000065, Completion: 0.00000325, CacheRead: 0.00000013, CacheWrite: 0.0000008125},
		"bailian|qwen3-coder-next": {Prompt: 0.00000011, Completion: 0.0000008, CacheRead: 0.00000007},
		"zai|glm-5.2":              {Prompt: 0.000000798, Completion: 0.000002508, CacheRead: 0.0000001482},
		"zai|glm-5-turbo":          {Prompt: 0.0000012, Completion: 0.000004, CacheRead: 0.00000024},
		"zai|glm-5":                {Prompt: 0.00000095, Completion: 0.00000315, CacheRead: 0.00000019},
		"zai|glm-4.7":              {Prompt: 0.0000004, Completion: 0.00000175, CacheRead: 0.00000008},
		"mimo|mimo-v2.5-pro":       {Prompt: 0.000000435, Completion: 0.00000087, CacheRead: 0.0000000036},
		"mimo|mimo-v2.5":           {Prompt: 0.00000014, Completion: 0.00000028, CacheRead: 0.0000000028},
	}
	for model, alias := range modelAliases {
		if price, ok := prices[providerKeyForModel(model)]; ok {
			prices[alias] = price
		}
	}
	return prices
}

func providerKeyForModel(model string) string {
	switch {
	case strings.HasPrefix(model, "qwen"):
		return "bailian|" + model
	case strings.HasPrefix(model, "glm"):
		return "zai|" + model
	case strings.HasPrefix(model, "mimo"):
		return "mimo|" + model
	default:
		return model
	}
}

func saveCache(path string, cached cacheFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(cached)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".openrouter-pricing-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
