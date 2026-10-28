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
	consistencyAPI *apis.ConsistencyAPI, healthAPI *apis.HealthAPI,
	searchAPI *apis.SearchAPI, chatAPI *apis.ChatAPI,
	conversationAPI *apis.ConversationAPI, llmAPI *apis.LLMAPI,
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
	engine.GET("/health", healthAPI.Check)

	v1 := engine.Group("/api/v1")
	registerPublicRoutes(v1, api, userAPI, rl)

	// Route cần đăng nhập — Handler verify token với DB mỗi request
	// nên đổi pass/khóa acc có hiệu lực ngay. Thêm middleware mới
	// (rate limit...) vào middlewares.Protected là đủ.
	authed := v1.Group("")
	authed.Use(middlewares.Protected(auth)...)
	{
		authed.GET("/users", userAPI.GetAllUser)
		authed.GET("/auth/profile", userAPI.Profile)
		authed.POST("/auth/change-password", userAPI.ChangePassword)
		authed.POST("/auth/logout-all", userAPI.LogoutAll)
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
		authed.GET("/articles/:id/sections", articleAPI.ListSections)
		authed.GET("/articles/:id/tree", articleAPI.GetTree)
		// Ký URL tải file gốc — đi thẳng xuống kho, không qua API.
		authed.GET("/articles/:id/download", articleAPI.Download)
		authed.PUT("/articles/:id", articleAPI.Update)
		authed.DELETE("/articles/:id", articleAPI.Delete)
		// Trạng thái pipeline gọn (1 query 4 cột) cho UI poll tiến trình —
		// không đặt dưới /articles vì đụng wildcard :id của gin.
		authed.GET("/pipeline/articles", articleAPI.ListStatuses)
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
			// Kiểm tra tính toàn vẹn dữ liệu (orphan/cycle/missing self-row).
			admin.GET("/admin/consistency", consistencyAPI.Check)
		}
		authed.POST("/search", searchAPI.Query)
		authed.POST("/chat", chatAPI.Chat)
		registerConversationRoutes(authed, chatAPI, conversationAPI, llmAPI)
	}

	return &Router{Engine: engine}
}

// registerPublicRoutes là nhóm không cần đăng nhập: ping/pong, đăng ký, đăng
// nhập (có rate limit) và refresh/logout — hai đường sau tự xác thực bằng
// chính refresh token nên access hết hạn vẫn dùng được.
//
// Tách khỏi NewRouter vì hàm đó đã chạm ngân sách (guardrail
// src/quality/function_budget_test.go).
func registerPublicRoutes(v1 *gin.RouterGroup, api *apis.PingPongAPI, userAPI *apis.UserAPI, rl middlewares.IRateLimiter) {
	v1.GET("/ping", api.Ping)
	v1.GET("/pong", api.Pong)
	v1.POST("/users", userAPI.Create)
	v1.POST("/auth/login", rl.Handler(), userAPI.Login)
	v1.POST("/auth/refresh", userAPI.Refresh)
	v1.POST("/auth/logout", userAPI.Logout)
}

// registerConversationRoutes ghi nhóm route phiên chat của chính user (S1),
// tệp đính kèm chat, lựa chọn provider/model cho UI (Đợt 28) và nhóm quản trị
// kết nối LLM (nấc 2). Tách khỏi NewRouter vì hàm đó đã chạm ngân sách
// (guardrail src/quality/function_budget_test.go).
//
// /chat/attachments trích text ĐỒNG BỘ cho một lượt hỏi (không nạp kho) nên
// mọi user đã đăng nhập đều dùng được; /llm/options KHÔNG gate admin: ai cũng
// được chọn model, service tự giới hạn field admin-only theo role trong token.
// Ngược lại /llm/providers/* là ĐƯỜNG GHI (key/model) nên gắn RequireRole ngay
// trên từng route — sub-group ở đây thay vì nhét thêm dòng vào block admin của
// NewRouter (đang đúng ngân sách).
func registerConversationRoutes(authed *gin.RouterGroup, chatAPI *apis.ChatAPI,
	api *apis.ConversationAPI, llmAPI *apis.LLMAPI) {
	authed.POST("/conversations", api.Create)
	authed.GET("/conversations", api.List)
	authed.GET("/conversations/:id", api.Get)
	authed.PATCH("/conversations/:id", api.Rename)
	authed.DELETE("/conversations/:id", api.Delete)
	authed.GET("/conversations/:id/messages", api.Messages)
	// Tệp đính kèm của một lượt: trả text đã trích, FE gửi lại trong ChatRequest.
	authed.POST("/chat/attachments", chatAPI.Attachment)
	// Lựa chọn provider/model đọc từ config — menu chat + Settings.
	authed.GET("/llm/options", llmAPI.Options)

	llmAdmin := authed.Group("")
	llmAdmin.Use(middlewares.RequireRole(domain.RoleAdmin))
	{
		// Định nghĩa provider (nấc 3) — PUT/DELETE trên đúng /llm/providers/:slug,
		// gin phân biệt với /llm/providers/:slug/key theo độ sâu path nên không
		// cần route phụ.
		llmAdmin.PUT("/llm/providers/:slug", llmAPI.SaveProvider)
		llmAdmin.DELETE("/llm/providers/:slug", llmAPI.DeleteProvider)
		llmAdmin.PUT("/llm/providers/:slug/key", llmAPI.SaveKey)
		llmAdmin.DELETE("/llm/providers/:slug/key", llmAPI.ClearKey)
		llmAdmin.POST("/llm/providers/:slug/models", llmAPI.LoadModels)
		llmAdmin.PUT("/llm/providers/:slug/model", llmAPI.SelectModel)
	}
}
