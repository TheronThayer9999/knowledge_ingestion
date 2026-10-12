package loader

import (
	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/question"
	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/agent/synthesis"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"

	ollamamodel "github.com/cloudwego/eino-ext/components/model/ollama"
)

// Wiring agent tập trung ở file này — thêm tool/prompt/model mới cho agent
// thì sửa ở đây, không lẫn vào wiring infra/API trong loader.go.

// newEinoChatModel dựng ChatModel Eino trên Ollama (lib eino-ext) — New chỉ
// dựng HTTP client, không dial nên boot không fail-fast vì Ollama; agent lỗi
// thì caller map 500, classify lỗi thì về lookup.
func newEinoChatModel(cfg config.IConfig) (*ollamamodel.ChatModel, error) {
	return eino.NewChatModel(cfg.GetLLM())
}

// newChatAgent dựng agent agentic một lần lúc startup: instruction từ file
// chat_system.md + các tool Eino. Thêm tool mới = bọc 1 file trong eino (xem
// hướng dẫn đầu knowledge_search.go) + thêm 1 dòng ở đây, usecase và prompt
// file không phải sửa (LLM tự thấy tool qua ToolInfo).
func newChatAgent(model *ollamamodel.ChatModel, prompts synthesis.Prompts, hybrid *retrieval.HybridSearcher, categoryRepo domain.ICategoryRepositoryImpl, vectors domain.IVectorStore, articleRepo domain.IArticleRepository) (*eino.ChatAgent, error) {
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
	return eino.NewChatAgent(model, prompts.System, search, cats, ctxTool, info)
}

// newHybridSearcher dựng pipeline hybrid dùng chung cho cả 2 đường /search
// (gọi trực tiếp) và /chat (bọc thành tool Eino) — cùng 1 pipeline, limit
// kẹp trần 1 chỗ trong SearchHybrid, không fork logic search.
func newHybridSearcher(embedder domain.Embedder, vectors domain.IVectorStore, articleRepo domain.IArticleRepository) *retrieval.HybridSearcher {
	return retrieval.NewHybridSearcher(embedder, vectors, articleRepo)
}

// newQuestionClassifier dựng bộ dán nhãn dùng chung cho /search (chọn route)
// và /chat (nhãn response sau run) — chung 1 ChatModel với agent, 1 prompt
// file, 1 điểm sửa.
func newQuestionClassifier(model *ollamamodel.ChatModel, classify question.Prompts) *question.LLMClassifier {
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
