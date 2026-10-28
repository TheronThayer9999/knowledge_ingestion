package loader

import (
	"context"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/question"
	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/agent/synthesis"
	"knowledge_ingestion/src/agent/usecase"
	"knowledge_ingestion/src/common/logs"
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

// newProviderAgents dựng agent cho từng provider trong llms.providers (map
// slug → cấu hình) qua eino.BuildProviderAgents — thêm backend mới (Groq,
// DeepSeek, ...) chỉ cần config, không sửa usecase/prompt. Provider nào thiếu
// cấu hình (vd thiếu key) thì boot vẫn lên, request nào chọn nó mới 400.
func newProviderAgents(
	cfg config.IConfig, prompts synthesis.Prompts,
	hybrid *retrieval.HybridSearcher,
	categoryRepo domain.ICategoryRepositoryImpl,
	vectors domain.IVectorStore,
	articleRepo domain.IArticleRepository,
	pageIssues domain.ArticlePageIssues,
	pageText domain.ArticlePageText,
	settings domain.ILLMProviderSettingRepository) (*ProviderAgents, error) {

	tools, err := eino.NewDefaultTools(eino.ToolDeps{
		Hybrid:       hybrid,
		Vectors:      vectors,
		Categories:   categoryRepo,
		Articles:     articleRepo,
		Pages:        pageIssues,
		PageText:     pageText,
		ChartEnabled: chartEnabled(cfg),
	})
	if err != nil {
		return nil, err
	}
	llms := cfg.GetLLMs()
	// Dựng provider từ config ∪ bảng cài đặt (usecase.ProviderConfigs) — thêm
	// backend mới (Groq, DeepSeek, ...) chỉ cần khai báo config, còn từ Đợt 28
	// nấc 3 admin bấm "Thêm provider" trong popup là có luôn không sửa file.
	// Provider thiếu cấu hình (vd thiếu key) thì boot vẫn lên, request nào chọn
	// nó mới 400; provider admin tắt không được dựng agent.
	overrides := providerSettingOverrides(settings)
	resolved := usecase.ProviderConfigs(llms, overrides, cfg.GetJWT().Secret)
	providers := make([]eino.LLMProvider, 0, len(resolved))
	for _, item := range resolved {
		providers = append(providers, eino.NewConfigProvider(item.Slug, item.Config))
	}
	return eino.BuildProviderAgents(
		eino.InstructionSet{Base: prompts.System, Sheets: prompts.Sheets},
		llms.DefaultProvider, tools, providers...)
}

// providerSettingOverrides đọc cài đặt provider một lần lúc boot, khoá theo
// slug đã chuẩn hoá. Lỗi đọc thì WARN rồi chạy đúng bằng config — key DB là
// lớp phủ, không phải điều kiện sống của app.
func providerSettingOverrides(repo domain.ILLMProviderSettingRepository) map[string]*domain.LLMProviderSetting {
	rows, err := repo.List(context.Background())
	if err != nil {
		logs.Warnw("llm: không đọc được cài đặt provider lúc boot, dùng config", "error", err)
		return nil
	}
	out := make(map[string]*domain.LLMProviderSetting, len(rows))
	for _, row := range rows {
		out[eino.NormalizeSlug(row.Slug)] = row
	}
	return out
}

// newHybridSearcher dựng pipeline hybrid dùng chung cho cả 2 đường /search
// (gọi trực tiếp) và /chat (bọc thành tool Eino) — cùng 1 pipeline, limit
// kẹp trần 1 chỗ trong SearchHybrid, không fork logic search.
func newHybridSearcher(embedder domain.Embedder, vectors domain.IVectorStore, articleRepo domain.IArticleRepository) *retrieval.HybridSearcher {
	return retrieval.NewHybridSearcher(embedder, vectors, articleRepo)
}

// newQuestionClassifier dựng bộ dán nhãn dùng chung cho /search (chọn route)
// và /chat (nhãn response sau run) — dùng client của provider mặc định (việc
// dán nhãn rẻ, không cần theo provider của request), 1 prompt file, 1 điểm sửa.
func newQuestionClassifier(agents *ProviderAgents, classify question.Prompts) *question.LLMClassifier {
	model, ok := agents.Models[agents.DefaultProvider]
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
