package loader

import (
	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/agent/question"
	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/agent/synthesis"
	"knowledge_ingestion/src/agent/usecase"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/apis"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/controller/routers"
	"knowledge_ingestion/src/controller/services"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/caches"
	"knowledge_ingestion/src/infrastructure/embedding"
	"knowledge_ingestion/src/infrastructure/postgres"
	"knowledge_ingestion/src/infrastructure/qdrant"
	"knowledge_ingestion/src/infrastructure/rabbitmq"
	"knowledge_ingestion/src/infrastructure/repository"
	"knowledge_ingestion/src/infrastructure/seaweedfs"

	ollamamodel "github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/go-playground/validator/v10"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

func Load() []fx.Option {
	return []fx.Option{
		fx.Options(loadAdapter()...),
		fx.Options(loadService()...),
		fx.Options(loadMiddleware()...),
		fx.Options(loadValidator()...),
		fx.Options(loadEngine()...),
	}
}

func loadAdapter() []fx.Option {
	return []fx.Option{
		//pgsql
		fx.Provide(postgres.NewConnection),
		fx.Provide(func(db postgres.IDB) *gorm.DB { return db.GetDB() }), // unwrap IDB -> *gorm.DB cho repository
		fx.Invoke(func(postgres.IDB) {}),                                 // init database
		//redis
		fx.Provide(caches.NewConnection),
		fx.Invoke(func(caches.ICache) {}),
		//s3
		fx.Provide(seaweedfs.NewStorage),
		// embedding
		fx.Provide(embedding.NewEmbedder),
		fx.Invoke(func(domain.Embedder) {}),
		//rabbirMq
		fx.Provide(rabbitmq.NewConnection),
		//
		fx.Provide(repository.NewUserRepository),
		fx.Provide(repository.NewCategoryRepository),
		fx.Provide(repository.NewArticleRepository),
		fx.Provide(repository.NewArticleChunkRepository),
		fx.Provide(repository.NewUnitOfWork),
		// qdrant cho API rebuild-vectors (xóa vector cũ) — NewConnection
		// ensureCollection lúc dựng nên API boot giờ fail-fast khi Qdrant
		// chết, giống worker (đánh đổi để endpoint rebuild luôn sẵn sàng).
		fx.Provide(qdrant.NewConnection),
	}
}

func loadService() []fx.Option {
	return []fx.Option{
		fx.Provide(services.NewPingPongService),
		fx.Provide(services.NewUserService),
		fx.Provide(services.NewFileService),
		fx.Provide(services.NewCategoryService),
		fx.Provide(services.NewArticleService),
		fx.Provide(services.NewArticleAdminService),
		// agent không import middlewares — loader nối ICurrentUser với IUserResolver.
		fx.Provide(func(u middlewares.ICurrentUser) port.IUserResolver { return u }),
		fx.Provide(newHybridSearcher),
		fx.Provide(newEinoChatModel),
		fx.Provide(newClassifyPrompts),
		fx.Provide(newQuestionClassifier),
		fx.Provide(usecase.NewSearchService),
		fx.Provide(newChatPrompts),
		fx.Provide(newChatAgent),
		fx.Provide(usecase.NewChatService),
	}
}

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
func newChatAgent(model *ollamamodel.ChatModel, prompts synthesis.Prompts, hybrid *retrieval.HybridSearcher) (*eino.ChatAgent, error) {
	search, err := eino.NewKnowledgeSearchTool(hybrid)
	if err != nil {
		return nil, err
	}
	return eino.NewChatAgent(model, prompts.System, search)
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

// loadMiddleware đăng ký middleware HTTP — thêm middleware mới (rate limit...)
// thì Provide constructor ở đây, router/service nhận interface là có.
func loadMiddleware() []fx.Option {
	return []fx.Option{
		fx.Provide(middlewares.NewCORSMiddleware),
		fx.Provide(middlewares.NewTraceMiddleware),
		fx.Provide(middlewares.NewAuthMiddleware),
		fx.Provide(middlewares.NewCurrentUser),
	}
}

func loadValidator() []fx.Option {
	return []fx.Option{
		fx.Provide(validator.New),
	}
}

func loadEngine() []fx.Option {
	return []fx.Option{
		fx.Provide(apis.NewBaseController),
		fx.Provide(apis.NewPingPongAPI),
		fx.Provide(apis.NewUserAPI),
		fx.Provide(apis.NewFileAPI),
		fx.Provide(apis.NewCategoryAPI),
		fx.Provide(apis.NewArticleAPI),
		fx.Provide(apis.NewArticleAdminAPI),
		fx.Provide(apis.NewSearchAPI),
		fx.Provide(apis.NewChatAPI),
		fx.Provide(routers.NewRouter),
	}
}
