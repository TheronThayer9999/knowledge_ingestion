package loader

import (
	"context"

	einotool "github.com/cloudwego/eino/components/tool"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/question"
	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/agent/synthesis"
	"knowledge_ingestion/src/agent/usecase"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
)

// Wiring agent tập trung ở file này — thêm tool/prompt/model mới cho agent
// thì sửa ở đây, không lẫn vào wiring infra/API trong loader.go.

// ProviderAgents là registry agent chat theo backend LLM — xem eino package
// (định nghĩa ở đó để usecase dùng được mà không cycle import với loader).
// Thêm backend mới chỉ cần khai báo trong config llms.providers, usecase và
// prompt file không phải sửa.
type ProviderAgents = eino.ProviderAgents

// AgentTools là bộ tool chat — dùng CHUNG cho registry chung (provider của
// hệ thống) và registry riêng (kết nối của user), nên dựng đúng một lần.
// Tool stateless, phạm vi tin cậy đi qua ctx chứ không nằm trong tool.
type AgentTools = []einotool.BaseTool

// newAgentTools dựng bộ tool cho mọi agent chat. Tách riêng khỏi newProviderAgents
// vì registry RIÊNG của user cần ĐÚNG bộ tool này — dựng lần thứ hai thì hai
// registry lệch nhau, lượt chat trên kết nối riêng sẽ thiếu tool của agent thường.
func newAgentTools(cfg config.IConfig, hybrid *retrieval.HybridSearcher,
	categoryRepo domain.ICategoryRepositoryImpl,
	vectors domain.IVectorStore,
	articleRepo domain.IArticleRepository,
	pageIssues domain.ArticlePageIssues,
	pageText domain.ArticlePageText) (AgentTools, error) {
	return eino.NewDefaultTools(eino.ToolDeps{
		Hybrid:       hybrid,
		Vectors:      vectors,
		Categories:   categoryRepo,
		Articles:     articleRepo,
		Pages:        pageIssues,
		PageText:     pageText,
		ChartEnabled: chartEnabled(cfg),
	})
}

// newProviderAgents dựng agent cho từng provider HIỆU LỰC — config ∪ bảng cài
// đặt, luật gộp nằm trong usecase.ProviderCatalog — qua eino.NewProviderAgents:
// thêm backend mới (Groq, DeepSeek, ...) chỉ cần khai báo config, còn admin bấm
// "Thêm provider" trong popup là có luôn không sửa file. Provider thiếu cấu hình
// (vd thiếu key) thì boot vẫn lên, request nào chọn nó mới 400; provider admin
// tắt không được dựng agent.
func newProviderAgents(catalog *usecase.ProviderCatalog, prompts synthesis.Prompts,
	tools AgentTools) (*ProviderAgents, error) {
	resolved := catalog.Resolved(context.Background())
	providers := make([]eino.LLMProvider, 0, len(resolved))
	for _, item := range resolved {
		providers = append(providers, eino.NewConfigProvider(item.Slug, item.Config))
	}
	return eino.NewProviderAgents(
		eino.InstructionSet{Base: prompts.System, Sheets: prompts.Sheets},
		catalog.ConfiguredDefault(), tools, providers...)
}

// newUserAgents dựng registry agent cho các kết nối RIÊNG của user (BYOK).
// Rỗng lúc boot — kết nối sinh ra theo yêu cầu từng user, không khai báo
// trước trong config.
func newUserAgents(prompts synthesis.Prompts, tools AgentTools) *eino.UserAgents {
	return eino.NewUserAgents(
		eino.InstructionSet{Base: prompts.System, Sheets: prompts.Sheets}, tools)
}

// asProviderRegistry expose registry cho service CHỈ ĐỌC (bảng chọn model của
// UI). Cùng một instance với asProviderRegistryAdmin — fx dựng registry đúng một
// lần, hai key chỉ là hai vai (xem usecase/provider_registry.go).
func asProviderRegistry(agents *ProviderAgents) usecase.IProviderRegistry { return agents }

// asProviderRegistryAdmin expose phần GHI cho popup Nhà cung cấp AI: rebuild /
// tháo agent / đổi model mặc định khi admin lưu cấu hình lúc app đang chạy.
func asProviderRegistryAdmin(agents *ProviderAgents) usecase.IProviderRegistryAdmin { return agents }

// newHybridSearcher dựng pipeline hybrid dùng chung cho cả 2 đường /search
// (gọi trực tiếp) và /chat (bọc thành tool Eino) — cùng 1 pipeline, limit
// kẹp trần 1 chỗ trong SearchHybrid, không fork logic search.
func newHybridSearcher(embedder domain.Embedder, vectors domain.IVectorStore, articleRepo domain.IArticleRepository) *retrieval.HybridSearcher {
	return retrieval.NewHybridSearcher(embedder, vectors, articleRepo)
}

// newQuestionClassifier dựng bộ dán nhãn dùng chung cho /search (chọn route)
// và /chat (nhãn response sau run) — dùng client của provider mặc định (việc
// dán nhãn rẻ, không cần theo provider của request), 1 prompt file, 1 điểm sửa.
// Trả interface hẹp của usecase: service không biết tới kiểu cụ thể của question.
func newQuestionClassifier(agents *ProviderAgents, classify question.Prompts) usecase.IClassifier {
	model, ok := agents.RawModel(agents.DefaultProvider())
	if !ok {
		return question.NewLLMClassifier(nil, classify)
	}
	return question.NewLLMClassifier(model, classify)
}

// newClassifyPrompts load system prompt vòng 1 (phân loại câu hỏi) từ file
// lúc startup — cùng quy ước với prompt chat.
func newClassifyPrompts(cfg config.IConfig) (question.Prompts, error) {
	return question.LoadPrompts(cfg.GetAgent().PromptDir)
}

// newChatPrompts load system prompt chat từ file lúc startup — sửa
// prompts/chat_system.md rồi restart app là ăn, không cần build lại.
// File thiếu thì app fail-fast vì không còn bản mặc định trong code.
func newChatPrompts(cfg config.IConfig) (synthesis.Prompts, error) {
	return synthesis.LoadPrompts(cfg.GetAgent().PromptDir)
}

// chartEnabled đọc cờ `agent.chart_enabled`: bỏ trống trong config = BẬT (dù
// là tính năng mới), chỉ `false` tường minh mới tắt. Đặt ở loader thay vì
// config.Load vì cấu hình cũ (không có key) phải giữ nguyên hành vi mà không
// cần sửa file config nào.
func chartEnabled(cfg config.IConfig) bool {
	c := cfg.GetAgent().ChartEnabled
	return c == nil || *c
}
