package loader

import (
	"knowledge_ingestion/src/infrastructure/postgres"
	"knowledge_ingestion/src/infrastructure/repository"
	"knowledge_ingestion/src/infrastructure/seaweedfs"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// LoadWorkerInfra providers hạ tầng dùng chung cho các binary ngoài API
// (hiện tại là cmd/worker) — tách riêng khỏi Load() để worker không boot
// nhầm gin router + middleware HTTP, mà sau loader API đổi gì bên hạ tầng
// (pool, tracing...) worker cũng ăn theo, khỏi copy-paste fx.Provide ở mỗi
// main. Config (cần đường dẫn file từ flag) thì mỗi main tự Provide.
func LoadWorkerInfra() []fx.Option {
	return []fx.Option{
		fx.Provide(postgres.NewConnection),
		fx.Provide(func(db postgres.IDB) *gorm.DB { return db.GetDB() }), // unwrap IDB -> *gorm.DB cho repository
		fx.Provide(seaweedfs.NewStorage),
		fx.Provide(repository.NewArticleRepository),
	}
}
