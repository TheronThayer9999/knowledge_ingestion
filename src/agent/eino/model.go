package eino

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	ollamamodel "github.com/cloudwego/eino-ext/components/model/ollama"
	openrouter "github.com/cloudwego/eino-ext/components/model/openai"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
)

// Provider name request chọn qua field `provider` của ChatRequest — thêm
// backend mới thì implement LLMProvider (provider.go) + 1 dòng đăng ký ở
// loader, usecase và prompt file không phải sửa.
const (
	ProviderOllama     = "ollama"
	ProviderOpenRouter = "openrouter"
)

// NewChatModel dựng ChatModel Eino trên Ollama bằng lib eino-ext — protocol
// /api/chat, tool-calling, message mapping, callbacks đều do framework lo.
// Wiring giữ lại vì là đặc thù triển khai: cắt hậu tố /api* khỏi base_url
// (vận hành hay copy URL dính hậu tố), Bearer key cho Ollama Cloud qua
// HTTPClient, default temperature/max_tokens từ config. Model cụ thể KHÔNG
// kẹt ở constructor — theo từng request qua
// adk.WithChatModelOptions(model.WithModel(...)) lúc agent Run. Trả interface
// ToolCallingChatModel để loader đổi backend (ollama/openrouter) mà không
// sửa agent, classifier hay usecase.
func NewChatModel(cfg config.LLMConfig) (einomodel.ToolCallingChatModel, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("eino ollama: base_url is required")
	}
	timeout := cfg.TimeoutSec
	if timeout <= 0 {
		timeout = 120
	}
	return ollamamodel.NewChatModel(context.Background(), &ollamamodel.ChatModelConfig{
		BaseURL: trimAPIURL(cfg.BaseURL),
		HTTPClient: &http.Client{
			Timeout:   time.Duration(timeout) * time.Second,
			Transport: authTransport(cfg.APIKey),
		},
		Model: cfg.Model,
		Options: &ollamamodel.Options{
			Temperature: float32(cfg.Temperature),
			NumPredict:  cfg.MaxTokens,
		},
	})
}

// ProviderAgents là registry agent chat theo backend LLM — request chọn qua
// field `provider` (mặc định theo cấu hình). Mỗi provider 1 ChatAgent riêng
// (cùng bộ tool, client khác nhau) dựng 1 lần lúc startup qua
// BuildProviderAgents; thêm backend mới không đụng tới struct này.
type ProviderAgents struct {
	Agents map[string]*ChatAgent
	// Models là client thô theo provider — classifier vòng 1 dùng chung client
	// mặc định, khỏi tốn thêm lượt dựng.
	Models map[string]einomodel.ToolCallingChatModel
	// DefaultProvider là provider khi request không ép (lấy từ llm.provider).
	DefaultProvider string
	// DefaultModel là model mặc định theo provider khi request không ép model.
	DefaultModel map[string]string
}

// Resolve chọn agent + model cho 1 request: provider rỗng thì mặc
// định, model rỗng thì model mặc định của provider đó. Provider chưa cấu
// hình (vd openrouter thiếu key) thì lỗi rõ để caller map 400, không 500.
func (p *ProviderAgents) Resolve(provider, model string) (*ChatAgent, string, error) {
	if p == nil {
		return nil, "", fmt.Errorf("eino agent: chưa được dựng")
	}
	name := strings.ToLower(strings.TrimSpace(provider))
	if name == "" {
		name = p.DefaultProvider
	}
	agent, ok := p.Agents[name]
	if !ok {
		return nil, "", fmt.Errorf("eino agent: provider %q chưa cấu hình (mới có: %s)", name, availableProviders(p.Agents))
	}
	if strings.TrimSpace(model) == "" {
		model = p.DefaultModel[name]
	}
	if strings.TrimSpace(model) == "" {
		return nil, "", fmt.Errorf("eino agent: provider %q chưa có model mặc định, request phải ép model", name)
	}
	return agent, model, nil
}

// retryTransport bọc Transport khác, retry khi gặp 429/5xx — model free
// (OpenRouter shared pool) hay trả 429 theo cửa sổ phút; retry vài lần với
// backoff là qua, khỏi fail cả lượt chat. Tôn trọng Retry-After của server
// (trần 60s). Body request được đệm để gửi lại được. Lỗi khác và hết lượt
// thì trả về như cũ để caller map 500/400.
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	baseDelay  time.Duration
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	maxRetries := t.maxRetries
	if maxRetries <= 0 {
		maxRetries = 4
	}
	delay := t.baseDelay
	if delay <= 0 {
		delay = 2 * time.Second
	}
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
	}
	for attempt := 0; ; attempt++ {
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.ContentLength = int64(len(body))
		}
		resp, err := base.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return resp, nil
		}
		if attempt >= maxRetries {
			return resp, nil
		}
		wait := delay << attempt
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, convErr := strconv.Atoi(strings.TrimSpace(ra)); convErr == nil && secs > 0 {
				wait = time.Duration(secs) * time.Second
			}
		}
		if wait > 60*time.Second {
			wait = 60 * time.Second
		}
		_ = resp.Body.Close()
		logs.Warnw("openrouter: dính giới hạn, thử lại", "attempt", attempt+1, "status", resp.StatusCode, "wait_ms", wait.Milliseconds())
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}
}

// NewOpenRouterChatModel dựng ChatModel Eino trên OpenRouter (OpenAI-compatible
// /api/v1/chat/completions) bằng lib openai của eino-ext — cùng interface
// ToolCallingChatModel với Ollama nên agent/tool/history dùng chung, chỉ khác
// backend. Key rỗng thì lỗi ngay ở đây để loader bỏ qua provider này (boot
// vẫn lên, request nào chọn openrouter mới báo chưa cấu hình).
func NewOpenRouterChatModel(cfg config.OpenRouterConfig) (einomodel.ToolCallingChatModel, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("eino openrouter: api_key is required")
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	timeout := cfg.TimeoutSec
	if timeout <= 0 {
		timeout = 120
	}
	temp := float32(0)
	maxTokens := 2048
	return openrouter.NewChatModel(context.Background(), &openrouter.ChatModelConfig{
		APIKey:      strings.TrimSpace(cfg.APIKey),
		BaseURL:     base,
		Model:       cfg.Model,
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		HTTPClient: &http.Client{
			Timeout:   time.Duration(timeout) * time.Second,
			Transport: &retryTransport{base: http.DefaultTransport, maxRetries: 4, baseDelay: 2 * time.Second},
		},
	})
}

// trimAPIURL cắt hậu tố API khỏi base_url đã cấu hình — lib framework parse
// URL nguyên văn nên phải dọn trước, không thì request thành /api/api/chat.
func trimAPIURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	for _, suffix := range []string{"/api/chat", "/api/tags", "/api/embed", "/api"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return base
}

// authTransport gắn Bearer token cho Ollama Cloud — lib chính thức/ext không
// có khái niệm API key nên key đi theo Transport; local không key thì transport
// nguyên bản.
func authTransport(apiKey string) http.RoundTripper {
	if strings.TrimSpace(apiKey) == "" {
		return http.DefaultTransport
	}
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.Header.Set("Authorization", "Bearer "+apiKey)
		return http.DefaultTransport.RoundTrip(req)
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
