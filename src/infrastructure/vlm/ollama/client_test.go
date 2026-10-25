package ollama

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"knowledge_ingestion/src/domain"
)

// fakeOllama dựng server /api/chat giả và giữ lại request cuối để test kiểm
// payload — không cần model thật, không cần mạng.
type fakeOllama struct {
	status  int
	content string
	last    chatRequest
	calls   int
}

func (f *fakeOllama) handler(w http.ResponseWriter, r *http.Request) {
	f.calls++
	if r.URL.Path != "/api/chat" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &f.last)
	if f.status != 0 && f.status != http.StatusOK {
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(chatResponse{
		Message: chatMessage{Role: "assistant", Content: f.content},
		Done:    true,
	})
}

func newReader(t *testing.T, fake *fakeOllama, mutate func(*Config)) *Reader {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	cfg := Config{
		BaseURL:       srv.URL + "/", // dấu / cuối phải được trim, không thành "//api/chat"
		Model:         "qwen2.5vl:7b",
		Timeout:       5 * time.Second,
		MaxImageBytes: 1 << 20,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	r, err := NewReader(cfg, Prompts{Transcribe: "đọc cả trang", Ask: "trả lời câu hỏi"})
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	return r
}

func testImage(t *testing.T) domain.ImageRef {
	t.Helper()
	img, err := domain.NewImageRef(7, "image/png", []byte("png-bytes"))
	if err != nil {
		t.Fatalf("NewImageRef: %v", err)
	}
	return img
}

func TestTranscribe(t *testing.T) {
	fake := &fakeOllama{content: "| STT | Mức phạt |\n|---|---|\n| 1 | 200.000 |"}
	r := newReader(t, fake, nil)

	got, err := r.Transcribe(context.Background(), testImage(t), "trang có bảng 2 cột")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if !strings.Contains(got.Text, "200.000") {
		t.Errorf("text = %q, muốn giữ nội dung bảng", got.Text)
	}
	if got.Model != "qwen2.5vl:7b" {
		t.Errorf("model = %q", got.Model)
	}

	if fake.last.Model != "qwen2.5vl:7b" {
		t.Errorf("request model = %q", fake.last.Model)
	}
	if fake.last.Stream {
		t.Error("stream = true, muốn false (đọc cả trang cần đủ text mới dùng được)")
	}
	if len(fake.last.Messages) != 1 {
		t.Fatalf("messages = %d, muốn 1", len(fake.last.Messages))
	}
	msg := fake.last.Messages[0]
	if msg.Role != "user" {
		t.Errorf("role = %q", msg.Role)
	}
	if !strings.Contains(msg.Content, "đọc cả trang") || !strings.Contains(msg.Content, "bảng 2 cột") {
		t.Errorf("content thiếu prompt hoặc hint: %q", msg.Content)
	}
	if len(msg.Images) != 1 {
		t.Fatalf("images = %d, muốn 1", len(msg.Images))
	}
	raw, err := base64.StdEncoding.DecodeString(msg.Images[0])
	if err != nil {
		t.Fatalf("images[0] không phải base64: %v", err)
	}
	if string(raw) != "png-bytes" {
		t.Errorf("ảnh gửi lên = %q", raw)
	}
}

func TestAskBuildsQuestion(t *testing.T) {
	fake := &fakeOllama{content: "200.000 đồng"}
	r := newReader(t, fake, nil)

	got, err := r.Ask(context.Background(), testImage(t), "Mức phạt dòng 1 là bao nhiêu?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got != "200.000 đồng" {
		t.Errorf("answer = %q", got)
	}
	content := fake.last.Messages[0].Content
	if !strings.Contains(content, "trả lời câu hỏi") || !strings.Contains(content, "Mức phạt dòng 1 là bao nhiêu?") {
		t.Errorf("content = %q, muốn gồm prompt + câu hỏi", content)
	}
}

func TestAskEmptyQuestion(t *testing.T) {
	r := newReader(t, &fakeOllama{}, nil)
	if _, err := r.Ask(context.Background(), testImage(t), "  "); !errors.Is(err, ErrMissingPrompt) {
		t.Fatalf("err = %v, muốn ErrMissingPrompt", err)
	}
}

func TestEmptyAnswerIsRetryable(t *testing.T) {
	r := newReader(t, &fakeOllama{content: "   "}, nil)
	_, err := r.Transcribe(context.Background(), testImage(t), "")
	if !errors.Is(err, ErrEmptyAnswer) {
		t.Fatalf("err = %v, muốn ErrEmptyAnswer", err)
	}
	if r.IsPermanentError(err) {
		t.Error("content rỗng phải retry được, không phải lỗi vĩnh viễn")
	}
}

func TestStatusClassification(t *testing.T) {
	tests := []struct {
		status    int
		permanent bool
	}{
		{http.StatusBadRequest, true},          // sai model/ảnh hỏng ⇒ bỏ luôn
		{http.StatusUnauthorized, true},        // key sai
		{http.StatusRequestTimeout, false},     // 408 retry được
		{http.StatusTooManyRequests, false},    // 429 retry được
		{http.StatusServiceUnavailable, false}, // 503 backend bận
		{http.StatusInternalServerError, false},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			r := newReader(t, &fakeOllama{status: tc.status}, nil)
			_, err := r.Transcribe(context.Background(), testImage(t), "")
			if err == nil {
				t.Fatal("muốn lỗi")
			}
			if got := r.IsPermanentError(err); got != tc.permanent {
				t.Errorf("HTTP %d: permanent = %v, muốn %v (err: %v)", tc.status, got, tc.permanent, err)
			}
		})
	}
}

func TestImageTooLarge(t *testing.T) {
	fake := &fakeOllama{content: "x"}
	r := newReader(t, fake, func(c *Config) { c.MaxImageBytes = 2 })
	_, err := r.Transcribe(context.Background(), testImage(t), "")
	if !errors.Is(err, domain.ErrImageTooLarge) {
		t.Fatalf("err = %v, muốn ErrImageTooLarge", err)
	}
	if !r.IsPermanentError(err) {
		t.Error("ảnh quá trần là lỗi vĩnh viễn (retry vẫn thế)")
	}
	if fake.calls != 0 {
		t.Errorf("calls = %d, không được gửi request khi ảnh vượt trần", fake.calls)
	}
}

func TestNewReaderValidation(t *testing.T) {
	okPrompts := Prompts{Transcribe: "t", Ask: "a"}
	if _, err := NewReader(Config{BaseURL: "", Model: "m"}, okPrompts); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("thiếu base_url: err = %v", err)
	}
	if _, err := NewReader(Config{BaseURL: "http://x", Model: ""}, okPrompts); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("thiếu model: err = %v", err)
	}
	if _, err := NewReader(Config{BaseURL: "http://x", Model: "m"}, Prompts{Transcribe: "t"}); !errors.Is(err, ErrMissingPrompt) {
		t.Errorf("thiếu prompt: err = %v", err)
	}
}

// TestNewReaderTrimsBaseURL khoá lỗi hay gặp: base_url có "/" cuối thành
// "//api/chat", Ollama trả 404 mà log lại trông như sai model.
func TestNewReaderTrimsBaseURL(t *testing.T) {
	r, err := NewReader(
		Config{BaseURL: "http://localhost:11434/", Model: "m"},
		Prompts{Transcribe: "t", Ask: "a"},
	)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if r.cfg.BaseURL != "http://localhost:11434" {
		t.Errorf("baseURL = %q", r.cfg.BaseURL)
	}
}
