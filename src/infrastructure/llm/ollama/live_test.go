package ollama

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
)

// TestLiveCloud kiểm tra Ollama Cloud thật (LLM cloud, embedding local nên
// test này chỉ đụng cloud) — SKIP khi key rỗng (cả file lẫn env
// OLLAMA_API_KEY), giống pattern postgres test. Tốn vài token cho đúng 1 câu
// Chat ngắn.
func TestLiveCloud(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", "..", "..", "..", "configs", "config.json"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	lc := cfg.GetLLM()
	if lc.APIKey == "" {
		t.Skip("no llm api key (file + env OLLAMA_API_KEY đều rỗng), skipping live cloud test")
	}
	if lc.Model == "" {
		t.Fatal("llm.model rỗng — chưa chọn model mặc định")
	}

	c, err := New(lc)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. ListModels phải thấy đúng model config đang trỏ (không tốn token).
	models, err := c.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels (cloud %s): %v", lc.BaseURL, err)
	}
	t.Logf("cloud models: %v", models)
	found := false
	for _, m := range models {
		if m == lc.Model || strings.HasPrefix(m, lc.Model+":") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("model %q không có trên cloud — kiểm tra tên model", lc.Model)
	}

	// 2. Chat 1 câu chào để xác nhận model trả lời được (vài token).
	out, err := c.Chat(ctx,
		[]domain.ChatMessage{{Role: "user", Content: "xin chào"}},
		domain.ChatOptions{Model: lc.Model, MaxTokens: 20})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	t.Logf("chat out: %q", out)
	if strings.TrimSpace(out) == "" {
		t.Fatal("chat trả về rỗng")
	}
}
