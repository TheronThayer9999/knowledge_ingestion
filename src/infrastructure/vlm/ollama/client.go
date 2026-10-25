// Package ollama là adapter đọc ảnh qua Ollama /api/chat (payload `images`
// base64) — dùng cho tài liệu phức tạp: bảng nhiều cột, biểu mẫu, bản scan.
//
// Package này CỐ Ý không import `src/config`: loader map config → Config struct,
// giống cách extractor nhận extractor.OCRConfig. Nhờ vậy (a) test không cần
// config file, (b) sau này chuyển sang sidecar Python chỉ cần thêm một
// implementation khác của domain.IVLMReader/IVLMAsker.
package ollama

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/domain"
)

// Sentinel lỗi của adapter. Consumer so bằng errors.Is; IsPermanentError quyết
// định có retry hay không.
var (
	// ErrEmptyAnswer: model trả content rỗng — coi như lỗi tạm thời (model bị
	// cắt do hết context, thiếu ảnh...) nên retry được.
	ErrEmptyAnswer = errors.New("vlm: model trả về nội dung rỗng")
	// ErrNotConfigured: thiếu base_url/model — lỗi cấu hình, không retry.
	ErrNotConfigured = errors.New("vlm: chưa cấu hình base_url hoặc model")
	// ErrMissingPrompt: prompt bắt buộc rỗng — lỗi cấu hình, không retry.
	ErrMissingPrompt = errors.New("vlm: thiếu prompt")
)

// Config là cấu hình transport của reader.
type Config struct {
	BaseURL string
	Model   string
	APIKey  string // Ollama Cloud dùng Bearer; local để trống
	Timeout time.Duration
	// MaxTokens trần output (num_predict). 0 = để backend tự quyết.
	MaxTokens int
	// Temperature mặc định phải là 0: đây là bước TRÍCH XUẤT, cần tất định,
	// sáng tạo là lỗi.
	Temperature float64
	// MaxImageBytes trần ảnh gửi lên. 0 = không kiểm (không khuyến nghị).
	MaxImageBytes int
}

// Prompts giữ nội dung prompt đã load từ prompts/*.md. Adapter chỉ biết text,
// không biết file nằm ở đâu — cùng lý do Config không import config.
type Prompts struct {
	// Transcribe: đọc cả trang thành markdown, giữ bảng dạng bảng.
	Transcribe string
	// Ask: trả lời đúng một câu hỏi trỏ vào ảnh.
	Ask string
}

// Reader hiện thực CẢ HAI vai domain.IVLMReader và domain.IVLMAsker trên cùng
// một instance (chung http.Client, chung config); fx bind từng vai riêng để
// consumer chỉ thấy method thuộc vai của mình (ISP).
type Reader struct {
	cfg     Config
	prompts Prompts
	http    *http.Client
}

// Khẳng định lúc biên dịch: đổi chữ ký interface ở domain mà quên sửa adapter
// thì `go build` gãy ngay, không đợi tới lúc chạy.
var (
	_ domain.IVLMReader = (*Reader)(nil)
	_ domain.IVLMAsker  = (*Reader)(nil)
)

// NewReader dựng reader từ config + prompt đã load. Chỉ validate cấu hình,
// KHÔNG gọi mạng — nhờ vậy app boot được khi Ollama chưa chạy.
func NewReader(cfg Config, prompts Prompts) (*Reader, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	model := strings.TrimSpace(cfg.Model)
	if base == "" || model == "" {
		return nil, fmt.Errorf("%w (base_url=%q model=%q)", ErrNotConfigured, cfg.BaseURL, cfg.Model)
	}
	if strings.TrimSpace(prompts.Transcribe) == "" || strings.TrimSpace(prompts.Ask) == "" {
		return nil, ErrMissingPrompt
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}
	if cfg.Temperature < 0 {
		cfg.Temperature = 0
	}
	cfg.BaseURL, cfg.Model = base, model
	return &Reader{cfg: cfg, prompts: prompts, http: &http.Client{Timeout: cfg.Timeout}}, nil
}

// ModelName trả model đang dùng (đã trim) — ghi vào trace để biết bản trích do
// model nào đọc.
func (r *Reader) ModelName() string { return r.cfg.Model }

// Transcribe đọc cả trang thành markdown.
func (r *Reader) Transcribe(ctx context.Context, img domain.ImageRef, hint string) (domain.Reading, error) {
	prompt := r.prompts.Transcribe
	if h := strings.TrimSpace(hint); h != "" {
		prompt += "\n\nGợi ý riêng cho trang này: " + h
	}
	text, err := r.chat(ctx, img, prompt)
	if err != nil {
		return domain.Reading{}, err
	}
	return domain.Reading{Text: text, Model: r.cfg.Model}, nil
}

// Ask hỏi đúng một câu về ảnh — dùng khi agent cần xác minh một con số cụ thể
// thay vì đọc cả trang.
func (r *Reader) Ask(ctx context.Context, img domain.ImageRef, question string) (string, error) {
	q := strings.TrimSpace(question)
	if q == "" {
		return "", fmt.Errorf("%w: câu hỏi rỗng", ErrMissingPrompt)
	}
	return r.chat(ctx, img, r.prompts.Ask+"\n\nCâu hỏi: "+q)
}

// IsPermanentError phân loại lỗi để consumer quyết định retry hay bỏ.
//
// 4xx là vĩnh viễn (sai model, ảnh hỏng, key sai) TRỪ 408 (timeout phía
// server) và 429 (rate limit) — hai mã này retry là có nghĩa.
func (r *Reader) IsPermanentError(err error) bool {
	if err == nil {
		return false
	}
	for _, target := range []error{
		domain.ErrImageEmpty,
		domain.ErrImageTooLarge,
		domain.ErrImageFormat,
		ErrNotConfigured,
		ErrMissingPrompt,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	var se *statusError
	if errors.As(err, &se) {
		if se.code == http.StatusRequestTimeout || se.code == http.StatusTooManyRequests {
			return false
		}
		return se.code >= 400 && se.code < 500
	}
	return false
}

// statusError giữ HTTP status để IsPermanentError phân loại mà không phải
// parse message.
type statusError struct {
	code int
	body string
}

func (e *statusError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("vlm: backend trả HTTP %d", e.code)
	}
	return fmt.Sprintf("vlm: backend trả HTTP %d: %s", e.code, e.body)
}

// doChat là payload /api/chat của Ollama. Chỉ khai báo field mình dùng —
// Ollama bỏ qua field lạ, và giữ struct hẹp giúp đọc code biết chính xác cái gì
// được gửi đi.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  chatOptions   `json:"options"`
}

type chatMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

type chatOptions struct {
	Temperature float64 `json:"temperature"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Done    bool        `json:"done"`
	Error   string      `json:"error"`
}

// chat gửi 1 lượt hỏi–đáp kèm ảnh. Dùng net/http thuần như
// infrastructure/embedding/ollama: cùng backend, cùng style, không thêm
// dependency.
func (r *Reader) chat(ctx context.Context, img domain.ImageRef, prompt string) (string, error) {
	if img.IsZero() {
		return "", domain.ErrImageEmpty
	}
	if r.cfg.MaxImageBytes > 0 && img.Size() > r.cfg.MaxImageBytes {
		return "", fmt.Errorf("%w: %d > %d byte", domain.ErrImageTooLarge, img.Size(), r.cfg.MaxImageBytes)
	}

	payload := chatRequest{
		Model:  r.cfg.Model,
		Stream: false, // đọc cả trang cần đủ text mới dùng được — không stream
		Messages: []chatMessage{{
			Role:    "user",
			Content: prompt,
			Images:  []string{base64.StdEncoding.EncodeToString(img.Bytes())},
		}},
		Options: chatOptions{Temperature: r.cfg.Temperature, NumPredict: r.cfg.MaxTokens},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("vlm: mã hoá request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("vlm: dựng request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if r.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.cfg.APIKey)
	}

	start := time.Now()
	resp, err := r.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("vlm: gọi %s: %w", r.cfg.BaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("vlm: đọc phản hồi: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", &statusError{code: resp.StatusCode, body: snippet(raw)}
	}

	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("vlm: giải mã phản hồi: %w", err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("vlm: backend báo lỗi: %s", out.Error)
	}
	text := strings.TrimSpace(out.Message.Content)
	if text == "" {
		return "", ErrEmptyAnswer
	}
	logs.Debug("vlm read xong",
		"model", r.cfg.Model, "page", img.PageNum(), "bytes", img.Size(),
		"out_chars", len(text), "ms", time.Since(start).Milliseconds())
	return text, nil
}

// snippet cắt body lỗi để log không phình vì HTML trang lỗi của proxy.
func snippet(b []byte) string {
	const max = 300
	s := strings.TrimSpace(string(b))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
