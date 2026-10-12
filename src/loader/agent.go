package loader

import (
	"strings"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/question"
	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/agent/synthesis"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"

	einotool "github.com/cloudwego/eino/components/tool"
)

// Wiring agent tập trung ở file này — thêm tool/prompt/model mới cho agent
// thì sửa ở đây, không lẫn vào wiring infra/API trong loader.go.

// ProviderAgents là registry agent chat theo backend LLM — xem eino package
// (định nghĩa ở đó để usecase dùng được mà không cycle import với loader).
// Thêm backend mới thì thêm 1 case ở newProviderAgents, usecase và prompt
// file không phải sửa.
type ProviderAgents = eino.ProviderAgents

// newProviderAgents dựng agent cho từng backend đã đăng ký qua
// eino.BuildProviderAgents — thêm backend mới thì thêm 1 dòng đăng ký dưới
// đây, không sửa usecase/prompt. Provider nào thiếu cấu hình (vd openrouter
// thiếu key) thì boot vẫn lên, request nào chọn nó mới 400.
func newProviderAgents(
	cfg config.IConfig, prompts synthesis.Prompts,
	hybrid *retrieval.HybridSearcher,
	categoryRepo domain.ICategoryRepositoryImpl,
	vectors domain.IVectorStore,
	articleRepo domain.IArticleRepository) (*ProviderAgents, error) {

	search, err := eino.NewKnowledgeSearchTool(hybrid, vectors)
	if err != nil {
		return nil, err
	}
	cats, err := eino.NewListCategoriesTool(categoryRepo)
	if err != nil {
		return nil, err
	}
	ctxTool, err := eino.NewReadContextTool(vectors)
	if err != nil {
		return nil, err
	}
	info, err := eino.NewArticleInfoTool(articleRepo)
	if err != nil {
		return nil, err
	}
	sections, err := eino.NewListSectionsTool(vectors)
	if err != nil {
		return nil, err
	}
	llm := cfg.GetLLM()
	defaultProvider := eino.ProviderOllama
	if strings.TrimSpace(llm.Provider) != "" {
		defaultProvider = strings.ToLower(strings.TrimSpace(llm.Provider))
	}
	tools := []einotool.BaseTool{search, cats, ctxTool, info, sections}
	return eino.BuildProviderAgents(prompts.System, defaultProvider, tools,
		eino.NewOllamaProvider(llm),
		eino.NewOpenRouterProvider(cfg.GetOpenRouter()),
	)
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
