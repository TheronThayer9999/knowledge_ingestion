package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
)

// compile-time check: Client phải implement domain.ILLM
var _ domain.ILLM = (*Client)(nil)

// Client gọi Ollama chat — model truyền theo mỗi request (config chỉ giữ
// default kết nối + sinh câu trả lời), list qua GET /api/tags.
type Client struct {
	baseURL     string
	temperature float64
	maxTokens   int
	http        *http.Client
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

type chatRequest struct {
	Model    string               `json:"model"`
	Messages []domain.ChatMessage `json:"messages"`
	Stream   bool                 `json:"stream"`
	Options  map[string]any       `json:"options,omitempty"`
}

type chatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func New(cfg config.LLMConfig) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("ollama: base_url is required")
	}

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	// Cùng pattern client embed: người vận hành hay copy URL dính hậu tố API.
	for _, suffix := range []string{"/api/chat", "/api/tags", "/api"} {
		baseURL = strings.TrimSuffix(baseURL, suffix)
	}

	timeout := cfg.TimeoutSec
	if timeout <= 0 {
		timeout = 120
	}

	return &Client{
		baseURL:     baseURL,
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
		http:        &http.Client{Timeout: time.Duration(timeout) * time.Second},
	}, nil
}

// ListModels lấy danh sách model đang có trên Ollama để user chọn.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("ollama: create tags request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: call tags api: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ollama: tags api returned %d: %s", resp.StatusCode, msg)
	}

	var out tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama: decode tags response: %w", err)
	}

	names := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		if m.Name != "" {
			names = append(names, m.Name)
		}
	}
	return names, nil
}

// Chat gọi đúng model user truyền theo request — rỗng thì lỗi ngay thay vì
// tự đoán model, để không bao giờ trả lời bằng model không ai chọn.
func (c *Client) Chat(ctx context.Context, messages []domain.ChatMessage, opts domain.ChatOptions) (string, error) {
	if opts.Model == "" {
		return "", fmt.Errorf("ollama: model is required per request (lấy danh sách qua ListModels)")
	}
	if len(messages) == 0 {
		return "", fmt.Errorf("ollama: messages is required")
	}

	temperature := opts.Temperature
	if temperature <= 0 {
		temperature = c.temperature
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = c.maxTokens
	}

	body, err := json.Marshal(chatRequest{		Model:    opts.Model,
		Messages: messages,
		Stream:   false,
		Options: map[string]any{
			"temperature":  temperature,
			"num_predict":  maxTokens,
		},
	})
	if err != nil {
		return "", fmt.Errorf("ollama: marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ollama: create chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: call chat api: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("ollama: chat api returned %d: %s", resp.StatusCode, msg)
	}

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("ollama: decode chat response: %w", err)
	}
	return out.Message.Content, nil
}
