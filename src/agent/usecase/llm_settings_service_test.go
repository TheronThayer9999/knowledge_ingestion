package usecase

import (
	"strings"
	"testing"

	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
)

// TestBuildLLMOptionsAppliesDBSetting khoá quy tắc "config là mặc định, DB
// thắng khi có": model + models + cờ key của admin đều lấy từ bảng cài đặt,
// config_model vẫn hiện để popup phân biệt hai nguồn.
func TestBuildLLMOptionsAppliesDBSetting(t *testing.T) {
	llms, agents := optionsFixture()
	cipher, err := utils.SealString("db-key", "test-secret")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	settings := map[string]*domain.LLMProviderSetting{
		"groq": {
			Slug:         "groq",
			APIKeyCipher: cipher,
			Model:        "db-model",
			Models:       `["db-model","other"]`,
		},
	}
	res := buildLLMOptions(llms, agents, settings, domain.RoleAdmin)
	groq := res.Providers[1]
	if groq.Slug != "groq" {
		t.Fatalf("provider[1] = %q, muốn groq", groq.Slug)
	}
	if groq.DefaultModel != "db-model" {
		t.Errorf("default_model = %q, muốn model trong DB", groq.DefaultModel)
	}
	if groq.ConfigModel != "qwen/qwen3.8-27b" {
		t.Errorf("config_model = %q, muốn model trong config", groq.ConfigModel)
	}
	if len(groq.Models) != 2 || groq.Models[0] != "db-model" || groq.Models[1] != "other" {
		t.Errorf("models = %v, muốn danh sách đã tải từ DB", groq.Models)
	}
	if groq.KeySource != keySourceDB || groq.HasKey == nil || !*groq.HasKey {
		t.Errorf("key_source/has_key = %q/%v, muốn database/true", groq.KeySource, groq.HasKey)
	}
}

// TestBuildLLMOptionsKeySourceConfig: chưa có dòng DB thì key đến từ config —
// popup phải nói đúng nguồn để admin biết "Key đang lấy trong file config".
func TestBuildLLMOptionsKeySourceConfig(t *testing.T) {
	llms, agents := optionsFixture()
	res := buildLLMOptions(llms, agents, nil, domain.RoleAdmin)
	if got := res.Providers[0].KeySource; got != keySourceConfig {
		t.Errorf("key_source = %q, muốn %q", got, keySourceConfig)
	}
	if got := res.Providers[1].KeySource; got != "" {
		t.Errorf("groq chưa có key, key_source = %q", got)
	}
}

// TestMergeProviderSettingKeyWinsConfig: key DB thắng key config; cipher hỏng
// (đổi JWT_SECRET) thì quay về key config thay vì dựng client không key.
func TestMergeProviderSettingKeyWinsConfig(t *testing.T) {
	base := config.LLMProviderConfig{APIKey: "config-key", Model: "config-model"}

	cipher, err := utils.SealString("db-key", "test-secret")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	merged := MergeProviderSetting(base, &domain.LLMProviderSetting{
		Slug: "p", APIKeyCipher: cipher, Model: "db-model", Models: `["a","b"]`,
	}, "test-secret")
	if merged.APIKey != "db-key" || merged.Model != "db-model" {
		t.Errorf("merged = %+v, muốn key/model của DB", merged)
	}
	if len(merged.Models) != 2 {
		t.Errorf("models = %v, muốn danh sách DB", merged.Models)
	}

	broken := MergeProviderSetting(base, &domain.LLMProviderSetting{Slug: "p", APIKeyCipher: "không-phải-cipher"}, "test-secret")
	if broken.APIKey != "config-key" {
		t.Errorf("cipher hỏng phải fallback về key config, nhận %q", broken.APIKey)
	}
}

// TestEncodeDecodeModelList: cột jsonb không bao giờ bị ghi rỗng (NOT NULL
// default '[]'), và dữ liệu hỏng thì coi như không có để config thắng.
func TestEncodeDecodeModelList(t *testing.T) {
	if got := encodeModelList(nil); got != "[]" {
		t.Errorf("encode rỗng = %q, muốn []", got)
	}
	if got := encodeModelList([]string{"a", "b"}); got != `["a","b"]` {
		t.Errorf("encode = %q", got)
	}
	if got := decodeModelList(""); got != nil {
		t.Errorf("decode rỗng = %v", got)
	}
	if got := decodeModelList("[not json"); got != nil {
		t.Errorf("decode hỏng = %v, muốn nil", got)
	}
	if got := decodeModelList(`["a"]`); len(got) != 1 || got[0] != "a" {
		t.Errorf("decode = %v", got)
	}
}

// TestParseModelList nhận cả 2 khuôn: OpenAI {"data":[{"id"}]} và Ollama
// {"models":[{"name"}]}; kết quả sắp A→Z và dedup để popup ổn định.
func TestParseModelList(t *testing.T) {
	openaiBody := []byte(`{"object":"list","data":[{"id":"b"},{"id":"a"},{"id":"a"},{"id":""}]}`)
	models, err := parseModelList(config.LLMTypeOpenAI, openaiBody)
	if err != nil {
		t.Fatalf("openai: %v", err)
	}
	if strings.Join(models, ",") != "a,b" {
		t.Errorf("openai models = %v, muốn [a b]", models)
	}

	ollamaBody := []byte(`{"models":[{"name":"gemma4:31b"},{"name":"llama3.1:8b"}]}`)
	models, err = parseModelList(config.LLMTypeOllama, ollamaBody)
	if err != nil {
		t.Fatalf("ollama: %v", err)
	}
	if strings.Join(models, ",") != "gemma4:31b,llama3.1:8b" {
		t.Errorf("ollama models = %v", models)
	}

	if _, err := parseModelList(config.LLMTypeOpenAI, []byte(`{"data":[]}`)); err == nil {
		t.Error("danh sách rỗng phải là lỗi đọc được, không phải 200 rỗng")
	}
	if _, err := parseModelList(config.LLMTypeOpenAI, []byte(`<html>`)); err == nil {
		t.Error("body không phải JSON phải lỗi")
	}
}

// TestModelsURL: Ollama đi /api/tags sau khi cắt hậu tố /api khỏi base_url;
// OpenAI-compatible thiếu key thì báo ngay, khỏi gọi rồi nhận 401.
func TestModelsURL(t *testing.T) {
	url, err := modelsURL(config.LLMProviderConfig{Type: config.LLMTypeOllama, BaseURL: "http://localhost:11434/api/"})
	if err != nil || url != "http://localhost:11434/api/tags" {
		t.Errorf("ollama url = %q, err = %v", url, err)
	}

	url, err = modelsURL(config.LLMProviderConfig{
		Type: config.LLMTypeOpenAI, BaseURL: "https://api.groq.com/openai/v1/", APIKey: "k",
	})
	if err != nil || url != "https://api.groq.com/openai/v1/models" {
		t.Errorf("openai url = %q, err = %v", url, err)
	}

	if _, err := modelsURL(config.LLMProviderConfig{Type: config.LLMTypeOpenAI, BaseURL: "https://x/v1"}); err == nil {
		t.Error("openai thiếu key phải lỗi trước khi gọi")
	}
}
