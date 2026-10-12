package eino

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	ollamamodel "github.com/cloudwego/eino-ext/components/model/ollama"

	"knowledge_ingestion/src/config"
)

// NewChatModel dựng ChatModel Eino trên Ollama bằng lib eino-ext — protocol
// /api/chat, tool-calling, message mapping, callbacks đều do framework lo.
// Wiring giữ lại vì là đặc thù triển khai: cắt hậu tố /api* khỏi base_url
// (vận hành hay copy URL dính hậu tố), Bearer key cho Ollama Cloud qua
// HTTPClient, default temperature/max_tokens từ config. Model cụ thể KHÔNG
// kẹt ở constructor — theo từng request qua
// adk.WithChatModelOptions(model.WithModel(...)) lúc agent Run.
func NewChatModel(cfg config.LLMConfig) (*ollamamodel.ChatModel, error) {
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
