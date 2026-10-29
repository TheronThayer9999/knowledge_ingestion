package loader

import (
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/agent/usecase"
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
		fx.Provide(repository.NewRefreshTokenRepository),
		fx.Provide(repository.NewCategoryRepository),
		fx.Provide(repository.NewArticleRepository),
		// Bind vai ArticleAdminQueue → cùng 1 repo. fx chỉ tự bind khi kiểu
		// consumer TRÙNG kiểu provider, nên đổi service sang vai hẹp hơn (ISP)
		// bắt buộc phải có dòng này — thiếu thì app fail lúc boot với
		// "missing type: domain.ArticleAdminQueue", và `go build` vẫn xanh.
		fx.Provide(func(repo domain.IArticleRepository) domain.ArticleAdminQueue { return repo }),
		// Category lẫn xoá bài trong cả subtree — cùng repo, vai hẹp hơn.
		fx.Provide(func(repo domain.IArticleRepository) domain.ArticleCategoryCascade { return repo }),
		// API cần pageRepo cho admin/reset-extract (xoá trang đã trích để
		// OCR lại). Worker dĩng vậy cũng có repo riêng ở loader/worker.go.
		fx.Provide(repository.NewArticlePageRepository),
		// Tool agent hỏi "trang nào không đọc được" — cùng bảng, vai hẹp hơn
		// (constructor riêng vì cái trên trả interface lớn, xem giải thích
		// trong repository).
		fx.Provide(repository.NewArticlePageIssues),
		// Tool agent đọc sheet của Excel — cùng bảng, vai hẹp "đọc text theo
		// cửa sổ" (không mở đường ghi cho agent).
		fx.Provide(repository.NewArticlePageText),
		// API /sections đọc summary đã compile (phase 1b) merge vào PageIndex.
		fx.Provide(repository.NewArticleSectionRepository),
		fx.Provide(repository.NewUnitOfWork),
		// qdrant cho API rebuild-vectors (xoá vector cũ) — NewConnection
		// ensureCollection lúc dựng nên API boot giờ fail-fast khi Qdrant
		// chết, giống worker (đánh đổi để endpoint rebuild luôn sẵn sàng).
		fx.Provide(qdrant.NewConnection),
		//
		fx.Provide(repository.NewCategoryClosureRepository),
		fx.Provide(repository.NewEmbedCacheRepository),
		// Nhịp sống worker để API trả về UI biết "đang xử lý" hay "chờ vì
		// worker đã tắt".
		fx.Provide(repository.NewWorkerHeartbeatRepository),
		// Phiên chat (S1): 2 repo conversations/messages — tách ra hàm riêng vì
		// loadAdapter đã đầy ngân sách hàm (guardrail function_budget_test.go).
		fx.Options(loadChatRepositories()...),
		// VLM đọc ảnh (qwen-vl) — bind 2 vai hẹp cho agent dùng `Ask`; provider
		// lazy nên không thành phụ thuộc bắt buộc lúc boot.
		fx.Options(vlmOptions()...),
	}
}

// loadChatRepositories: repo của phiên chat + cài đặt provider (key/model admin
// nhập trong popup — cùng nhóm "chat cần đọc"), để riêng khỏi loadAdapter cho
// hàm đó nằm trong ngân sách (guardrail src/quality/function_budget_test.go).
func loadChatRepositories() []fx.Option {
	return []fx.Option{
		fx.Provide(repository.NewConversationRepository),
		fx.Provide(repository.NewChatMessageRepository),
		fx.Provide(repository.NewLLMProviderSettingRepository),
	}
}

func loadService() []fx.Option {
	return []fx.Option{
		fx.Provide(services.NewPingPongService),
		fx.Provide(services.NewUserService),
		fx.Provide(services.NewFileService),
		fx.Provide(services.NewChatAttachmentService),
		fx.Provide(services.NewCategoryService),
		fx.Provide(services.NewArticleService),
		fx.Provide(services.NewArticleAdminService),
		fx.Provide(services.NewEmbedCacheAdminService),
		// Bộ reconcile mất vector — API cần nó cho endpoint consistency (phần
		// kiểm phía Qdrant); worker dựng bản riêng trong cmd/worker.
		fx.Provide(services.NewIndexReconcileService),
		fx.Provide(services.NewConsistencyService),
		fx.Provide(services.NewHealthService),
		// agent không import middlewares — loader nối ICurrentUser với IUserResolver.
		fx.Provide(func(u middlewares.ICurrentUser) port.IUserResolver { return u }),
		fx.Provide(newHybridSearcher),
		fx.Provide(newClassifyPrompts),
		fx.Provide(newQuestionClassifier),
		fx.Provide(usecase.NewSearchService),
		fx.Provide(newChatPrompts),
		// Nguồn cấu hình provider (config ∪ bảng cài đặt) dựng MỘT lần, mọi nơi
		// cần biết "provider nào, key nào, model nào" đều nhận cùng instance:
		// registry agent lúc boot, /llm/options, popup Nhà cung cấp AI.
		fx.Provide(usecase.NewProviderCatalog),
		fx.Provide(newProviderAgents),
		// Registry có hai vai — đọc cho UI/chat, ghi cho admin — nên service đọc
		// không cầm được quyền gỡ agent của người đang chat.
		fx.Provide(asProviderRegistry),
		fx.Provide(asProviderRegistryAdmin),
		fx.Provide(usecase.NewChatService),
		// CRUD phiên cho API + cổng lưu hội thoại cho agent — CÙNG nghiệp vụ,
		// hai vai hẹp (khuôn NewArticlePageRepository/NewArticlePageIssues).
		fx.Provide(services.NewConversationService),
		fx.Provide(services.NewChatSessionStore),
		// Bảng chọn provider/model cho UI (Đợt 28) — đọc config + registry
		// agent đã dựng, không gọi LLM. Bản ghi (nấc 2) là phần admin lưu key /
		// tải model / chọn model; cả hai dùng chung catalog. fetcher là cổng HTTP
		// riêng của "Tải model" (timeout 20s chặn provider treo).
		fx.Provide(usecase.NewLLMOptionsService),
		fx.Provide(usecase.NewLLMModelFetcher),
		fx.Provide(usecase.NewLLMSettingsService),
		// Phiên mã hoá payload chat (Đợt 29): handshake X25519 + kho khoá phiên.
		// Middleware không import services (services đã import middlewares — vòng
		// quay tròn), nên nghiệp vụ được expose qua vai hẹp ICryptoSessions mà
		// chính middlewares khai báo bên tiêu dùng.
		fx.Provide(services.NewChatCryptoService),
		fx.Provide(asCryptoSessions),
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
		fx.Provide(middlewares.NewRateLimiter),
		fx.Provide(middlewares.NewChatCryptoMiddleware),
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
		fx.Provide(apis.NewEmbedCacheAdminAPI),
		fx.Provide(apis.NewConsistencyAPI),
		fx.Provide(apis.NewHealthAPI),
		fx.Provide(apis.NewSearchAPI),
		fx.Provide(apis.NewChatAPI),
		fx.Provide(apis.NewConversationAPI),
		fx.Provide(apis.NewLLMAPI),
		fx.Provide(apis.NewCryptoAPI),
		fx.Provide(routers.NewRouter),
	}
}

// asCryptoSessions trói nghiệp vụ phiên mã hoá với vai hẹp mà middleware cần
// (Mode + SessionKey). Cùng một instance: fx dựng service đúng một lần nên
// handshake và middleware nhìn thấy chung một kho phiên.
func asCryptoSessions(svc services.IChatCryptoService) middlewares.ICryptoSessions { return svc }
