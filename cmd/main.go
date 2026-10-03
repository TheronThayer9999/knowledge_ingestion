package main

import (
	"knowledge_ingestion/src/config"
	"os"

	"go.uber.org/fx"
)

func main() {
	fx.New(
		// Cung cấp Config - path có thể override qua env var
		fx.Provide(func() (config.IConfig, error) {
			path := os.Getenv("APP_CONFIG_PATH")
			if path == "" {
				path = "./configs/app-config.json"
			}
			return config.Load(path)
		}),

		// // Các provider khác sẽ tự động nhận IConfig qua constructor injection
		// fx.Provide(postgres.NewConnection),      // func(cfg config.IConfig) (*sql.DB, error)
		// fx.Provide(seaweedfs.NewStorage),         // func(cfg config.IConfig) (storage.IStorage, error)
		// fx.Provide(repository.NewKnowledgeRepo),  // func(db *sql.DB) repository.IKnowledgeRepo
		// fx.Provide(service.NewKnowledgeService),  // func(repo, storage) service.IKnowledgeService

		// Khởi động Gin Server
		//fx.Invoke(routers.StartServer), // func(lc fx.Lifecycle, cfg config.IConfig, svc service.IKnowledgeService)
	).Run()
}
