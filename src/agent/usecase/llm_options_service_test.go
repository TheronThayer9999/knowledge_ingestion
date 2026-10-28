package usecase

import (
	"encoding/json"
	"strings"
	"testing"

	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
)

// optionsFixture dựng catalog + registry giả: 2 provider dựng được agent
// (9router, ollama), groq thiếu key nên loader đã bỏ qua. ollama có client suy
// nghĩ RIÊNG, 9router không — y như buildThinkingAgent ngoài eino. Trả đúng
// *fakeRegistry để test CRUD có thể đánh dấu thêm provider vừa dựng được agent.
func optionsFixture() (*ProviderCatalog, *fakeRegistry) {
	agents := newFakeRegistry("9router", "9router", "ollama").withThinking("ollama")
	// repo nil: buildLLMOptions không đọc DB, settings truyền vào từng test.
	return newProviderCatalog(optionsLLMs(), nil, "test-secret"), agents
}

// optionsLLMs là khối config dùng chung giữa test options và test CRUD cấp
// service (test cần sửa DefaultProvider rồi dựng catalog/registry lại).
func optionsLLMs() config.LLMsConfig {
	return config.LLMsConfig{
		DefaultProvider: "9Router ",
		Providers: map[string]config.LLMProviderConfig{
			"9router": {
				Type: config.LLMTypeOpenAI, Label: "9Router",
				Model:   "kc/nvidia/nemotron-3.5-super-vl:free",
				BaseURL: "http://localhost:20128/v1/", APIKey: "secret-key",
			},
			"ollama": {
				Type: config.LLMTypeOllama, Model: "gemma4:31b",
				BaseURL: "https://ollama.com", APIKey: "ollama-key",
			},
			"groq": {
				Type: config.LLMTypeOpenAI, Model: "qwen/qwen3.8-27b",
				Models:  []string{" m1 ", "m2", "m1", ""},
				BaseURL: "https://api.groq.com/openai/v1",
			},
		},
	}
}

// TestBuildLLMOptionsUserView khoá hình dạng response cho user thường: sắp theo
// slug, label fallback, models fallback về [model], available/thinking đúng
// registry, KHÔNG có base_url/has_key.
func TestBuildLLMOptionsUserView(t *testing.T) {
	catalog, agents := optionsFixture()
	res := buildLLMOptions(catalog, agents, nil, domain.RoleUser)

	if res.DefaultProvider != "9router" || res.DefaultModel != "kc/nvidia/nemotron-3.5-super-vl:free" {
		t.Fatalf("default = %q/%q", res.DefaultProvider, res.DefaultModel)
	}
	if len(res.Providers) != 3 {
		t.Fatalf("providers = %d, muốn 3", len(res.Providers))
	}
	if res.Providers[0].Slug != "9router" || res.Providers[1].Slug != "groq" || res.Providers[2].Slug != "ollama" {
		t.Fatalf("thứ tự slug = %v", []string{res.Providers[0].Slug, res.Providers[1].Slug, res.Providers[2].Slug})
	}

	nine := res.Providers[0]
	if nine.Label != "9Router" || !nine.Available || nine.Thinking || !nine.IsDefault {
		t.Errorf("9router = %+v", nine)
	}
	if len(nine.Models) != 1 || nine.Models[0] != nine.DefaultModel {
		t.Errorf("models 9router = %v, muốn [default_model]", nine.Models)
	}
	if nine.BaseURL != "" || nine.HasKey != nil {
		t.Errorf("user thường không được thấy base_url/has_key: %+v", nine)
	}

	groq := res.Providers[1]
	if groq.Available || groq.Label != "groq" {
		t.Errorf("groq = %+v (label phải fallback về slug)", groq)
	}
	// Model mặc định chưa có trong danh sách nên được chèn lên đầu — menu chat
	// không được thiếu đúng model sẽ chạy khi request bỏ trống.
	if len(groq.Models) != 3 || groq.Models[0] != "qwen/qwen3.8-27b" ||
		groq.Models[1] != "m1" || groq.Models[2] != "m2" {
		t.Errorf("models groq = %v, muốn [default m1 m2] (trim/dedup)", groq.Models)
	}

	if !res.Providers[2].Thinking {
		t.Error("ollama có client suy nghĩ riêng nên thinking phải true")
	}
}

// TestBuildLLMOptionsAdminView: admin thấy base_url (chuẩn hoá bỏ "/" cuối) và
// has_key cho MỌI provider — kể cả provider thiếu key (has_key=false).
func TestBuildLLMOptionsAdminView(t *testing.T) {
	catalog, agents := optionsFixture()
	res := buildLLMOptions(catalog, agents, nil, domain.RoleAdmin)

	bySlug := make(map[string]bool, len(res.Providers))
	for _, p := range res.Providers {
		bySlug[p.Slug] = true
		if p.HasKey == nil {
			t.Fatalf("%s: admin phải nhận has_key", p.Slug)
		}
	}
	if got := res.Providers[0].BaseURL; got != "http://localhost:20128/v1" {
		t.Errorf("base_url = %q, muốn bỏ dấu / cuối", got)
	}
	if !*res.Providers[0].HasKey {
		t.Error("9router có key nên has_key phải true")
	}
	groq := res.Providers[1]
	if *groq.HasKey || groq.BaseURL != "https://api.groq.com/openai/v1" {
		t.Errorf("groq = %+v, muốn has_key=false + base_url nguyên văn", groq)
	}
	if !bySlug["ollama"] {
		t.Error("admin phải thấy đủ provider trong config")
	}
}

// TestLLMOptionsJSONNeverLeaksKey là chốt an toàn của Nấc 1: JSON trả trình
// duyệt không có api_key, và user thường không có base_url/has_key.
func TestLLMOptionsJSONNeverLeaksKey(t *testing.T) {
	catalog, agents := optionsFixture()

	userRaw, err := json.Marshal(buildLLMOptions(catalog, agents, nil, domain.RoleUser))
	if err != nil {
		t.Fatalf("marshal user: %v", err)
	}
	userJSON := string(userRaw)
	if strings.Contains(userJSON, "api_key") || strings.Contains(userJSON, "secret-key") {
		t.Fatalf("response user lộ key: %s", userJSON)
	}
	if strings.Contains(userJSON, "base_url") || strings.Contains(userJSON, "has_key") {
		t.Fatalf("response user lộ field admin: %s", userJSON)
	}

	adminRaw, err := json.Marshal(buildLLMOptions(catalog, agents, nil, domain.RoleAdmin))
	if err != nil {
		t.Fatalf("marshal admin: %v", err)
	}
	adminJSON := string(adminRaw)
	if strings.Contains(adminJSON, "api_key") || strings.Contains(adminJSON, "secret-key") {
		t.Fatalf("response admin lộ key: %s", adminJSON)
	}
	if !strings.Contains(adminJSON, `"has_key":false`) {
		t.Fatalf("admin phải thấy has_key=false của provider thiếu key: %s", adminJSON)
	}
}
