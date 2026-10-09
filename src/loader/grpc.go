package loader

import (
	"knowledge_ingestion/src/controller/grpchandler"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/controller/services"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/embedding"
	"knowledge_ingestion/src/infrastructure/postgres"
	"knowledge_ingestion/src/infrastructure/qdrant"
	"knowledge_ingestion/src/infrastructure/repository"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// LoadGRPC providers tối thiểu cho binary agent-gateway (cmd/agent-gateway)
// chạy KnowledgeService: auth + search + rebuild-vectors. Không boot gin
// router, middleware HTTP, redis/rabbitmq/s3 để binary nhẹ và deploy độc
// lập với API. Sau loader API đổi gì bên hạ tầng (pool, tracing...) gRPC
// cũng ăn theo, khỏi copy-paste fx.Provide ở mỗi main. Config (cần đường
// dẫn file từ flag) thì mỗi main tự Provide.
func LoadGRPC() []fx.Option {
	return []fx.Option{
		fx.Provide(postgres.NewConnection),
		fx.Provide(func(db postgres.IDB) *gorm.DB { return db.GetDB() }), // unwrap IDB -> *gorm.DB cho repository
		fx.Invoke(func(postgres.IDB) {}),                                 // fail-fast khi DB chết
		fx.Provide(embedding.NewEmbedder),
		fx.Invoke(func(domain.Embedder) {}), // fail-fast khi Ollama config sai
		fx.Provide(qdrant.NewConnection),    // ensureCollection lúc dựng, fail-fast như API
		fx.Provide(repository.NewUserRepository),
		fx.Provide(repository.NewArticleRepository),
		fx.Provide(repository.NewArticleChunkRepository),
		fx.Provide(middlewares.NewAuthMiddleware),
		fx.Provide(middlewares.NewCurrentUser),
		fx.Provide(services.NewSearchService),
		fx.Provide(services.NewArticleAdminService),
		fx.Provide(grpchandler.NewServer),
	}
}
