package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadProjectConfig khóa cấu hình triển khai hiện tại: LLM lên Ollama
// Cloud, embedding ở local. Đổi config mà test này đỏ thì kiểm tra lại chủ
// trương trước khi sửa test.
func TestLoadProjectConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "configs", "config.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	ec := cfg.GetEmbedding()
	if ec.Provider != "ollama" {
		t.Errorf("embedding.provider = %q, want ollama", ec.Provider)
	}
	if ec.BaseURL == "" || isCloudURL(ec.BaseURL) {
		t.Errorf("embedding phải ở local, got base_url %q", ec.BaseURL)
	}
	if ec.Dim <= 0 {
		t.Errorf("embedding.dim = %d, want > 0", ec.Dim)
	}

	lms := cfg.GetLLMs()
	if lms.DefaultProvider != "ollama" {
		t.Errorf("llms.default_provider = %q, want ollama", lms.DefaultProvider)
	}
	oll, ok := lms.Providers["ollama"]
	if !ok {
		t.Fatal("llms.providers thiếu ollama")
	}
	if !isCloudURL(oll.BaseURL) {
		t.Errorf("ollama chạy Cloud, got base_url %q", oll.BaseURL)
	}
	if oll.Model == "" {
		t.Error("ollama.model rỗng — chưa chọn model mặc định")
	}
	if oll.MaxTokens <= 0 {
		t.Errorf("ollama.max_tokens = %d, want > 0", oll.MaxTokens)
	}
	// OpenRouter + Groq là backend OpenAI-compatible — phải có base_url và
	// key (key có thể trống ở máy khác, env OPENROUTER_API_KEY/GROQ_API_KEY
	// đè lại khi cần).
	for _, slug := range []string{"openrouter", "groq"} {
		p, ok := lms.Providers[slug]
		if !ok {
			t.Fatalf("llms.providers thiếu %s", slug)
		}
		if p.Type != "openai" {
			t.Errorf("%s.type = %q, want openai", slug, p.Type)
		}
		if p.BaseURL == "" {
			t.Errorf("%s.base_url rỗng", slug)
		}
		if p.Model == "" {
			t.Errorf("%s.model rỗng", slug)
		}
	}
}

// isCloudURL báo URL có trỏ lên Ollama Cloud không (không phải local).
func isCloudURL(u string) bool {
	return strings.Contains(u, "ollama.com")
}
