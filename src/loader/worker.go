package loader

import (
	"context"
	"fmt"
	"strings"
	"time"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/prompts"
	"knowledge_ingestion/src/agent/usecase"
	"knowledge_ingestion/src/common/chunker"
	"knowledge_ingestion/src/common/enhancement"
	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/services"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/caches"
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
		fx.Provide(caches.NewConnection),
		fx.Provide(embedding.NewEmbedder),
		fx.Provide(qdrant.NewConnection),
		fx.Provide(repository.NewArticleRepository),
		fx.Provide(repository.NewRefreshTokenRepository),
		fx.Provide(repository.NewArticlePageRepository),
		fx.Provide(repository.NewArticleSectionRepository),
		// Hàng đợi tab Quét: worker claim theo TRANG ở đây (API chỉ tạo/đọc job).
		fx.Provide(repository.NewOCRJobRepository),
		// Audit chi phí LLM theo tài khoản — worker compile ghi số liệu mỗi lô
		// mục vào đây (bảng llm_usage_daily).
		fx.Provide(repository.NewLLMUsageRepository),
		// Cache embed — tránh gọi lại model cho chunk đã tính. Ý tưởng lấy từ
		// AnythingLLM (cachedVectorInformation) nhưng khoá theo NỘI DUNG chunk
		// thay vì cả file, nên trùng lặp giữa các bài cũng ăn.
		fx.Provide(repository.NewEmbedCacheRepository),
		// Nhịp sống worker (host:pid → last_seen_at) để API/UI biết worker
		// còn chạy — worker tự ghi, chỉ đọc ở API.
		fx.Provide(repository.NewWorkerHeartbeatRepository),
		fx.Provide(repository.NewUnitOfWork),
		// PurgeOptions cho janitor dọn chunk già — map từ config worker để đổi
		// config + restart là ăn ngay.
		fx.Provide(func(cfg config.IConfig) services.PurgeOptions {
			w := cfg.GetWorker()
			return services.PurgeOptions{
				TrimInterval: time.Duration(w.TrimIntervalHours) * time.Hour,
			}
		}),
		// Enhancement (keywords) cho chunk — best-effort, tắt mặc định.
		fx.Provide(func(cfg config.IConfig, cache caches.ICache) enhancement.Enhancer {
			return enhancement.NewOllamaEnhancer(cfg, cache.GetClient())
		}),
		// Chunker + OCR cho worker chunk phase 1.
		//
		// Chunker chiến lược cắt — dựng 1 lần từ config rồi tái dùng
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
		// OCR map từ config sang struct của extractor để extractor không phụ
		// thuộc config. API cũng dùng đúng mapper này cho tab Quét.
		fx.Provide(newOCRConfig),
		// Kiểm tra traineddata lúc boot: thiếu gói ngôn ngữ thì tesseract
		// exit != 0 cho MỌI trang (không phải "đọc kém chính xác") — job scan
		// chết sau 10 lần retry mà log chỉ nói "tesseract lỗi". Ở đây chỉ
		// WARN, không chặn boot: vẫn có thể chỉ dùng PDF có text layer.
		fx.Invoke(func(cfg config.IConfig) {
			o := cfg.GetOCR()
			if !o.Enabled || strings.TrimSpace(o.Binary) == "" {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			have, err := extractor.ListLangs(ctx, o.Binary)
			if err != nil {
				logs.Warnw("ocr: không đọc được danh sách ngôn ngữ",
					"binary", o.Binary, "error", err)
				return
			}
			if miss := extractor.MissingLangs(have, o.Lang); len(miss) > 0 {
				logs.Warnw("ocr: thiếu traineddata — trang scan sẽ lỗi, không chỉ kém chính xác",
					"missing", miss, "available", have, "lang", o.Lang)
				return
			}
			logs.Infow("ocr: đủ traineddata", "lang", o.Lang, "available", have)
		}),
		// Tuỳ chọn của một lượt trích, map từ config worker — bật/tắt chỉ
		// cần sửa JSON rồi restart worker. Để TẮT thì trả slice rỗng chứ
		// không trả option `false`: mặc định của extractor đã là không đo,
		// tránh hai nguồn sự thật cho cùng một quyết định.
		fx.Provide(func(cfg config.IConfig) extractor.ExtractOptions {
			if !cfg.GetWorker().LayoutDetector {
				return nil
			}
			return extractor.ExtractOptions{extractor.WithLayoutDetection(true)}
		}),
		// Vai repo hẹp của worker — khai báo ở hàm riêng để LoadWorkerInfra không
		// phình thêm (ngân sách hàm, src/quality/function_budget_test.go).
		fx.Options(workerRoles()...),
		// ISummarizer cho compile phase 2 (P2.4 phase 1b) — dựng chat model từ
		// provider mặc định (llms.default_provider) + 2 system prompt load từ
		// prompts/compile_section.md (1 mục) và prompts/compile_sections.md (lô
		// N mục) lúc startup (fail-fast nếu thiếu file). Tool-calling model thỏa
		// BaseChatModel nên đưa thẳng vào summarizer.
		fx.Provide(func(cfg config.IConfig, db *gorm.DB) (domain.ISummarizer, error) {
			found, pcfg, err := compileProviderConfig(cfg, repository.NewLLMProviderSettingRepository(db))
			if err != nil {
				return nil, err
			}
			model, err := eino.NewConfigProvider(found, pcfg).NewModel()
			if err != nil {
				return nil, fmt.Errorf("worker compile: dựng model %q lỗi: %w", found, err)
			}
			system, err := prompts.Load(cfg.GetAgent().PromptDir, "compile_section.md")
			if err != nil {
				return nil, err
			}
			batchSystem, err := prompts.Load(cfg.GetAgent().PromptDir, "compile_sections.md")
			if err != nil {
				return nil, err
			}
			return eino.NewSectionSummarizer(model, pcfg.Model, found, system, batchSystem), nil
		}),
		// VLM đọc ảnh — worker dùng vai `Transcribe` khi extract gặp trang bảng/
		// scan; prompt thiếu thì fail-fast ngay lúc boot.
		fx.Options(vlmOptions()...),
	}
}

// workerRoles bind mỗi vai repo hẹp mà service của worker cần — đúng 1 dòng
// mỗi vai, cùng 1 instance ArticleRepository vì fx match theo type tường minh.
//
// ArticleAdminQueue là vai của bộ reconcile mất vector: ListByIndexStatus để
// duyệt bài đã done, ResetQueue để đưa bài ma về hàng đợi. ID và owner đều lấy
// từ row vừa đọc nên bộ lọc user_id của ResetQueue vẫn khớp đúng chủ — không có
// đường nào cho client đặt tên bài của người khác ở đường này (nó không đi qua
// HTTP).
func workerRoles() []fx.Option {
	return []fx.Option{
		fx.Provide(func(r domain.IArticleRepository) domain.ArticleExtractQueue { return r }),
		fx.Provide(func(r domain.IArticleRepository) domain.ArticleIndexQueue { return r }),
		fx.Provide(func(r domain.IArticleRepository) domain.ArticleJanitor { return r }),
		fx.Provide(func(r domain.IArticleRepository) domain.ArticleCompileQueue { return r }),
		fx.Provide(func(r domain.IArticleRepository) domain.ArticleAdminQueue { return r }),
	}
}

// compileProviderConfig chọn provider cho compile phase 2 theo `llms.default_provider`
// trong tập provider HIỆU LỰC do usecase.ProviderCatalog hợp nhất (config ∪ bảng
// cài đặt, đã phủ key/model admin lưu trong DB) — cùng một nguồn với chat nên key
// nhập trong popup có hiệu lực cho cả tóm tắt mục, và provider admin tạo từ UI vẫn
// compile được nếu file config trỏ default_provider vào nó. Tách khỏi
// LoadWorkerInfra (hàm đó đang đúng ngân sách).
func compileProviderConfig(cfg config.IConfig, repo domain.ILLMProviderSettingRepository) (string, config.LLMProviderConfig, error) {
	catalog := usecase.NewProviderCatalog(cfg, repo)
	slug := eino.NormalizeSlug(catalog.ConfiguredDefault())
	for _, item := range catalog.Resolved(context.Background()) {
		if item.Slug == slug {
			return item.Slug, item.Config, nil
		}
	}
	return "", config.LLMProviderConfig{}, fmt.Errorf(
		"worker compile: không thấy provider %q cho default_provider (khai trong config hoặc tạo trong popup)", slug)
}
