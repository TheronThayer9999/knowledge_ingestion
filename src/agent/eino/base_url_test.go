package eino

import (
	"testing"

	"knowledge_ingestion/src/config"
)

// TestNormalizeBaseURL khoá luật "base URL là GỐC API, không phải endpoint":
// người dùng hay dán nguyên dòng curl của provider, dán thừa hậu tố thì
// request thành ".../chat/completions/chat/completions".
func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		name string
		kind string
		raw  string
		want string
	}{
		{"giữ nguyên base có /v1", config.LLMTypeOpenAI,
			"https://api.groq.com/openai/v1", "https://api.groq.com/openai/v1"},
		{"bỏ dấu / cuối", config.LLMTypeOpenAI,
			"https://openrouter.ai/api/v1/", "https://openrouter.ai/api/v1"},
		{"bỏ hậu tố chat/completions", config.LLMTypeOpenAI,
			"https://api.groq.com/openai/v1/chat/completions", "https://api.groq.com/openai/v1"},
		{"bỏ hậu tố models", config.LLMTypeOpenAI,
			"https://api.groq.com/openai/v1/models", "https://api.groq.com/openai/v1"},
		{"bỏ query của provider", config.LLMTypeOpenAI,
			"https://x.openai.azure.com/openai/v1?api-version=2024-05-01",
			"https://x.openai.azure.com/openai/v1"},
		{"ollama bỏ hậu tố /api", config.LLMTypeOllama,
			"http://localhost:11434/api", "http://localhost:11434"},
		{"ollama giữ base trần", config.LLMTypeOllama,
			"http://localhost:11434/", "http://localhost:11434"},
		{"rỗng thì rỗng", config.LLMTypeOpenAI, "  ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeBaseURL(tc.kind, tc.raw); got != tc.want {
				t.Errorf("base = %q, muốn %q", got, tc.want)
			}
		})
	}
}

// TestHasVersionPath: base đã có đoạn phiên bản thì không thêm /v1 nữa
// (thêm sẽ thành /v1/v1).
func TestHasVersionPath(t *testing.T) {
	cases := map[string]bool{
		"https://api.groq.com/openai/v1":      true,
		"https://api.groq.com/openai/v1beta":  true,
		"http://localhost:20128/v1":           true,
		"https://api.groq.com/openai":         false,
		"https://api.groq.com":                false,
		"https://generativelanguage/v1beta/x": false,
	}
	for base, want := range cases {
		if got := HasVersionPath(base); got != want {
			t.Errorf("HasVersionPath(%q) = %v, muốn %v", base, got, want)
		}
	}
}
