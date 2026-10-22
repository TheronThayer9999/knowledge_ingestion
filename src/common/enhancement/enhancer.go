package enhancement

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"

	"github.com/redis/go-redis/v9"
)

// Enhancer sinh metadata (keywords) cho chunk. Best-effort: lỗi thì trả nil,
// không làm fail job index.
type Enhancer interface {
	Keywords(ctx context.Context, text string) ([]string, error)
}

// Noop không làm gì — dùng khi tắt enhancement hoặc thiếu LLM.
type Noop struct{}

func (Noop) Keywords(ctx context.Context, text string) ([]string, error) { return nil, nil }

// OllamaEnhancer gọi Ollama /api/generate để rút keywords, cache Redis.
type OllamaEnhancer struct {
	endpoint string
	model    string
	cache    *redis.Client
	ttl      time.Duration
	client   *http.Client
}

func NewOllamaEnhancer(cfg config.IConfig, cache *redis.Client) Enhancer {
	llms := cfg.GetLLMs()
	// Dùng provider mặc định (ollama) nếu có.
	model := "llama3.2"
	endpoint := "http://localhost:11434"
	if p, ok := llms.Providers["ollama"]; ok {
		if p.Model != "" {
			model = p.Model
		}
		if p.BaseURL != "" {
			endpoint = strings.TrimRight(p.BaseURL, "/")
		}
	}
	if !cfg.GetWorker().EnhanceKeywords {
		return Noop{}
	}
	return &OllamaEnhancer{
		endpoint: endpoint,
		model:    model,
		cache:    cache,
		ttl:      30 * 24 * time.Hour,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *OllamaEnhancer) Keywords(ctx context.Context, text string) ([]string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	key := "enh:kw:" + sha256Hex(e.model + "|" + text)
	if e.cache != nil {
		if v, err := e.cache.Get(ctx, key).Result(); err == nil && v != "" {
			return splitKeywords(v), nil
		}
	}
	prompt := "Extract 5-8 concise keywords (comma-separated, no explanation) for the following text:\n" + text
	body, _ := json.Marshal(map[string]any{
		"model":  e.model,
		"prompt": prompt,
		"stream": false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		logs.Error(err, "enhance: gọi ollama lỗi")
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	kw := splitKeywords(out.Response)
	if e.cache != nil && len(kw) > 0 {
		_ = e.cache.Set(ctx, key, strings.Join(kw, ","), e.ttl).Err()
	}
	return kw, nil
}

func splitKeywords(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		k := strings.TrimSpace(p)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}