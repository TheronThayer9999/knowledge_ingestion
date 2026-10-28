package usecase

import (
	"testing"

	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
)

// TestBuildLLMOptionsAppliesDBSetting khoá quy tắc "config là mặc định, DB
// thắng khi có": model + models + cờ key của admin đều lấy từ bảng cài đặt,
// config_model vẫn hiện để popup phân biệt hai nguồn.
func TestBuildLLMOptionsAppliesDBSetting(t *testing.T) {
	catalog, agents := optionsFixture()
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
	res := buildLLMOptions(catalog, agents, settings, domain.RoleAdmin)
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
	catalog, agents := optionsFixture()
	res := buildLLMOptions(catalog, agents, nil, domain.RoleAdmin)
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
