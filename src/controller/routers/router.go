package routers

import (
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/apis"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type Router struct {
	Engine *gin.Engine
}

func NewRouter(cfg config.IConfig, api *apis.PingPongAPI,
	userAPI *apis.UserAPI, fileAPI *apis.FileAPI,
	categoryAPI *apis.CategoryAPI, articleAPI *apis.ArticleAPI,
	adminAPI *apis.ArticleAdminAPI, embedCacheAPI *apis.EmbedCacheAdminAPI,
	searchAPI *apis.SearchAPI, chatAPI *apis.ChatAPI,
	cors middlewares.ICORSMiddleware, auth middlewares.IAuthMiddleware,
	trace middlewares.ITraceMiddleware, rl middlewares.IRateLimiter) *Router {
	if cfg.GetApp().Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	// Bật fallback *gin.Context -> Request.Context() cho Deadline/Done/Err/
	// Value: middleware nhét user_id vào request context chuẩn, controller
	// truyền thẳng `c` xuống service nên service mới đọc được qua ctx.Value.
	// Mặc định gin tắt cờ này (Value trả nil với key không phải string).
	engine.ContextWithFallback = true
	// Middleware global (recovery + trace + CORS) chạy cho mọi request, kể cả
	// preflight OPTIONS không mang token — xem middlewares.Global.
	engine.Use(middlewares.Global(cors, trace)...)

	engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := engine.Group("/api/v1")
	{
		v1.GET("/ping", api.Ping)
		v1.GET("/pong", api.Pong)
		v1.POST("/users", userAPI.Create)
		v1.POST("/auth/login", rl.Handler(), userAPI.Login)
	}

	// Route cần đăng nhập — Handler verify token với DB mỗi request
	// nên đổi pass/khóa acc có hiệu lực ngay. Thêm middleware mới
	// (rate limit...) vào middlewares.Protected là đủ.
	authed := v1.Group("")
	authed.Use(middlewares.Protected(auth)...)
	{
		authed.GET("/users", userAPI.GetAllUser)
		authed.GET("/auth/profile", userAPI.Profile)
		authed.POST("/auth/change-password", userAPI.ChangePassword)
		authed.POST("/uploads/presign", fileAPI.PresignUpload)
		authed.POST("/uploads/presign/batch", fileAPI.PresignUploads)
		//
		authed.POST("/categories", categoryAPI.Create)
		authed.GET("/categories", categoryAPI.GetAll)
		authed.GET("/categories/:id", categoryAPI.GetById)
		authed.PUT("/categories/:id", categoryAPI.Update)
		authed.DELETE("/categories/:id", categoryAPI.Delete)
		//
		authed.POST("/articles", articleAPI.Create)
		authed.GET("/articles", articleAPI.List)
		authed.GET("/articles/:id", articleAPI.GetByID)
		authed.PUT("/articles/:id", articleAPI.Update)
		authed.DELETE("/articles/:id", articleAPI.Delete)
		// Admin endpoints — yêu cầu role admin (role >= 1). Gate Role đã bật.
		admin := authed.Group("")
		admin.Use(middlewares.RequireRole(domain.RoleAdmin))
		{
			admin.POST("/admin/rebuild-vectors", adminAPI.Rebuild)
			// Xoá trang đã trích → OCR lại (cải thiện chất lượng trích).
			admin.POST("/admin/reset-extract", adminAPI.ResetExtract)
			// Xem hàng đợi extract/index + bài chờ purge — read-only.
			admin.GET("/admin/queue", adminAPI.Queue)
			// Embed cache: monitor + janitor giữ bảng không phình.
			admin.GET("/admin/embed-cache/stats", embedCacheAPI.Stats)
			admin.POST("/admin/embed-cache/purge", embedCacheAPI.Purge)
		}
		authed.POST("/search", searchAPI.Query)
		authed.POST("/chat", chatAPI.Chat)
	}

	return &Router{Engine: engine}
}
