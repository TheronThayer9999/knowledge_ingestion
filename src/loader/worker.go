package loader

import (
	"time"

	"knowledge_ingestion/src/common/chunker"
	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/services"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/embedding"
	"knowledge_ingestion/src/infrastructure/postgres"
	"knowledge_ingestion/src/infrastructure/qdrant"
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
		fx.Provide(embedding.NewEmbedder),
		fx.Provide(qdrant.NewConnection),
		fx.Provide(repository.NewArticleRepository),
		fx.Provide(repository.NewArticleChunkRepository),
		fx.Provide(repository.NewUnitOfWork),
		// PurgeOptions cho janitor dọn chunk già — map từ config worker để đổi
		// config + restart là ăn ngay.
		fx.Provide(func(cfg config.IConfig) services.PurgeOptions {
			w := cfg.GetWorker()
			return services.PurgeOptions{
				ChunkRetention: time.Duration(w.ChunkRetentionHours) * time.Hour,
				TrimInterval:   time.Duration(w.TrimIntervalHours) * time.Hour,
			}
		}),
		// Chunker cho worker chunk phase 1 — dựng 1 lần từ config rồi tái dùng
		// (splitter Eino stateless, pool chunk gọi song song an toàn). Tầng
		// semantic cần domain.Embedder (client Ollama đã Provide ở trên).
		fx.Provide(func(cfg config.IConfig, embedder domain.Embedder) (chunker.Chunker, error) {
			switch cfg.GetWorker().Chunker {
			case "semantic":
				return chunker.NewSemantic(embedder, chunker.SemanticOption{})
			case "manual":
				return chunker.Manual{}, nil
			default: // "recursive"
				return chunker.NewRecursive(chunker.Option{})
			}
		}),
		// Role interface hẹp cho worker — cùng 1 instance ArticleRepository,
		// fx match theo type tường minh nên bind 1 dòng mỗi vai.
		fx.Provide(func(r domain.IArticleRepository) domain.ArticleChunkQueue { return r }),
		fx.Provide(func(r domain.IArticleRepository) domain.ArticleEmbedQueue { return r }),
		fx.Provide(func(r domain.IArticleRepository) domain.ArticleJanitor { return r }),
		// OCR cho worker chunk — map từ config sang struct của extractor để
		// extractor không phụ thuộc config (sau này tách OCR thành API riêng
		// cũng dùng struct này làm contract).
		fx.Provide(func(cfg config.IConfig) extractor.OCRConfig {
			o := cfg.GetOCR()
			if o.Enabled && o.RenderBinary == "" {
				// Warn lúc boot thay vì để PDF scan failed thầm lặng sau 10
				// lần thử — người vận hành thấy ngay là thiếu renderer.
				logs.Warnw("ocr bật nhưng thiếu render_binary — trang PDF scan sẽ bị skip",
					"lang", o.Lang)
			}
			return extractor.OCRConfig{
				Enabled:      o.Enabled,
				Binary:       o.Binary,
				Lang:         o.Lang,
				Timeout:      time.Duration(o.TimeoutSec) * time.Second,
				RenderBinary: o.RenderBinary,
				RenderDPI:    o.RenderDPI,
			}
		}),
	}
}
