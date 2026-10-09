package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
)

// ErrBadResponse báo Ollama trả về sai contract (sai model, sai dim, thiếu
// vector) hoặc từ chối 4xx — retry cũng vậy nên service cho failed luôn thay
// vì đốt 10 cycle backoff. Lỗi 5xx/timeout/decode không wrap sentinel này,
// vẫn retry transient bình thường.
var ErrBadResponse = errors.New("ollama: bad response")

// IsPermanent báo lỗi có retry cũng vậy không — service gặp thì failed luôn.
func IsPermanent(err error) bool {
	return errors.Is(err, ErrBadResponse)
}

// compile-time check: Client phải implement domain.Embedder
var _ domain.Embedder = (*Client)(nil)

type Client struct {
	baseURL string
	model   string
	dim     int
	http    *http.Client
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func New(cfg config.EmbeddingConfig) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("ollama: base_url is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("ollama: model is required")
	}
	if cfg.Dim <= 0 {
		return nil, fmt.Errorf("ollama: dim must be > 0")
	}

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	// Người vận hành hay copy URL test từ browser (dính /api/tags) — cắt hậu
	// tố API để request không thành /api/tags/api/embed rồi 404 oan.
	for _, suffix := range []string{"/api/embed", "/api/tags", "/api"} {
		baseURL = strings.TrimSuffix(baseURL, suffix)
	}

	return &Client{
		baseURL: baseURL,
		model:   cfg.Model,
		dim:     cfg.Dim,
		http:    &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (c *Client) Dimension() int                  { return c.dim }
func (c *Client) ModelName() string               { return c.model }
func (c *Client) IsPermanentError(err error) bool { return IsPermanent(err) }

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	vectors, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

func (c *Client) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(embedRequest{Model: c.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: call embed api: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		err := fmt.Errorf("ollama: embed api returned %d: %s", resp.StatusCode, msg)
		// 4xx (trừ 429 hết quota/tốc độ) là client sai — retry vô ích.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			err = fmt.Errorf("%w: %w", ErrBadResponse, err)
		}
		return nil, err
	}

	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama: decode response: %w", err)
	}

	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("%w: expected %d embeddings, got %d", ErrBadResponse, len(texts), len(out.Embeddings))
	}
	for i, v := range out.Embeddings {
		if len(v) != c.dim {
			return nil, fmt.Errorf("%w: embedding %d has dim %d, config expects %d", ErrBadResponse, i, len(v), c.dim)
		}
	}

	return out.Embeddings, nil
}
