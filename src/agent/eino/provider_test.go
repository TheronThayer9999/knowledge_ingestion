package eino

import (
	"testing"

	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/config"
)

// nineRouterModel là model thật của 9router trong configs/config.local.json —
// có dấu "/" và hậu tố ":free", đúng loại id mà cách gộp "provider/model" của
// litellm sẽ cắt sai.
const nineRouterModel = "cl/nvidia/nemotron-3.5-super-vl:free"

// TestBuildProviderAgentsKeepsNineRouterModel khoá đường chạy local: 9router là
// provider mặc định của config.local.json, cả registry lẫn Resolve phải trả
// đúng model nguyên vẹn. Dựng client OpenAI-compatible chỉ validate key/base_url
// nên test không cần 9router đang bật.
func TestBuildProviderAgentsKeepsNineRouterModel(t *testing.T) {
	vectors := &bareVectors{}
	tools, err := NewDefaultTools(ToolDeps{
		Hybrid:     retrieval.NewHybridSearcher(nil, vectors, &fakeIssueArticles{}),
		Vectors:    vectors,
		Categories: &bareCategories{},
		Articles:   &fakeIssueArticles{},
		Pages:      &fakeIssuePages{},
		PageText:   &fakeTextPages{},
	})
	if err != nil {
		t.Fatalf("NewDefaultTools: %v", err)
	}

	agents, err := BuildProviderAgents(
		InstructionSet{Base: "luật chung", Sheets: "luật bảng tính"},
		"9router", tools,
		NewConfigProvider("9router", config.LLMProviderConfig{
			Type:    config.LLMTypeOpenAI,
			Model:   nineRouterModel,
			BaseURL: "http://localhost:20128/v1",
			APIKey:  "test-key",
		}),
	)
	if err != nil {
		t.Fatalf("BuildProviderAgents: %v", err)
	}
	if agents.DefaultProvider != "9router" {
		t.Fatalf("default provider = %q", agents.DefaultProvider)
	}
	if got := agents.DefaultModel["9router"]; got != nineRouterModel {
		t.Fatalf("default model = %q, muốn %q", got, nineRouterModel)
	}
	if _, ok := agents.Models["9router"]; !ok {
		t.Fatal("thiếu client thô cho classifier của provider mặc định")
	}

	// Request không ép gì → đúng agent + model của 9router.
	agent, model, err := agents.Resolve("", "", false)
	if err != nil {
		t.Fatalf("Resolve mặc định: %v", err)
	}
	if agent == nil {
		t.Fatal("Resolve trả agent nil")
	}
	if model != nineRouterModel {
		t.Fatalf("model = %q, muốn %q (không được cắt theo dấu /)", model, nineRouterModel)
	}

	// Slug được chuẩn hoá hoa/thường + khoảng trắng như mọi đường khác.
	if _, model, err = agents.Resolve("  9ROUTER ", "", false); err != nil {
		t.Fatalf("Resolve slug chuẩn hoá: %v", err)
	}
	if model != nineRouterModel {
		t.Fatalf("model sau chuẩn hoá = %q", model)
	}

	// thinking: OpenAI-compatible không có công tắc riêng → Resolve fallback
	// agent thường, không lỗi (9router vẫn chạy khi FE bật thinking).
	if _, _, err = agents.Resolve("9router", "", true); err != nil {
		t.Fatalf("Resolve thinking: %v", err)
	}

	// Model ép theo request vẫn thắng model mặc định.
	if _, model, err = agents.Resolve("9router", "kc/nvidia/nemotron-3-ultra-550b-a55b:free", false); err != nil {
		t.Fatalf("Resolve model ép: %v", err)
	}
	if model != "kc/nvidia/nemotron-3-ultra-550b-a55b:free" {
		t.Fatalf("model ép = %q", model)
	}
}

// TestBuildProviderAgentsSkipsProviderWithoutKey: provider thiếu key bị bỏ qua
// lúc boot (chỉ warn), request chọn nó mới 400 kèm danh sách provider đang có —
// đúng hành vi cũ, giữ lại để đổi wiring không âm thầm làm sập app.
func TestBuildProviderAgentsSkipsProviderWithoutKey(t *testing.T) {
	vectors := &bareVectors{}
	tools, err := NewDefaultTools(ToolDeps{
		Hybrid:     retrieval.NewHybridSearcher(nil, vectors, &fakeIssueArticles{}),
		Vectors:    vectors,
		Categories: &bareCategories{},
		Articles:   &fakeIssueArticles{},
		Pages:      &fakeIssuePages{},
		PageText:   &fakeTextPages{},
	})
	if err != nil {
		t.Fatalf("NewDefaultTools: %v", err)
	}
	agents, err := BuildProviderAgents(
		InstructionSet{Base: "luật chung"},
		"9router", tools,
		NewConfigProvider("9router", config.LLMProviderConfig{
			Type: config.LLMTypeOpenAI, Model: nineRouterModel,
			BaseURL: "http://localhost:20128/v1", APIKey: "test-key",
		}),
		NewConfigProvider("openrouter", config.LLMProviderConfig{
			Type: config.LLMTypeOpenAI, Model: "x/y:free",
			BaseURL: "https://openrouter.ai/api/v1", // thiếu api_key
		}),
	)
	if err != nil {
		t.Fatalf("BuildProviderAgents: %v", err)
	}
	if _, ok := agents.Agents["openrouter"]; ok {
		t.Fatal("provider thiếu key không được dựng agent")
	}
	if _, _, err := agents.Resolve("openrouter", "", false); err == nil {
		t.Fatal("Resolve provider chưa cấu hình phải lỗi để caller map 400")
	}
}
