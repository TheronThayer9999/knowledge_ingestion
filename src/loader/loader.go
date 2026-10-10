package loader

import (
	"knowledge_ingestion/src/controller/apis"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/controller/routers"
	"knowledge_ingestion/src/controller/services"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/caches"
	"knowledge_ingestion/src/infrastructure/embedding"
	"knowledge_ingestion/src/infrastructure/llm"
	"knowledge_ingestion/src/infrastructure/postgres"
	"knowledge_ingestion/src/infrastructure/qdrant"
	"knowledge_ingestion/src/infrastructure/rabbitmq"
	"knowledge_ingestion/src/infrastructure/repository"
	"knowledge_ingestion/src/infrastructure/seaweedfs"

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
		// llm chat cho vòng 1 agentic (classifier) — New chỉ dựng HTTP client,
		// không dial nên boot không fail-fast vì Ollama; Classify lỗi thì về
		// lookup nên Ollama chết search vẫn chạy.
		fx.Provide(llm.NewLLM),
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
		fx.Provide(services.NewSearchService),
	}
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
		fx.Provide(routers.NewRouter),
	}
}
