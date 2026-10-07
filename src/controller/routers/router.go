package routers

import (
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/apis"
	"knowledge_ingestion/src/controller/middlewares"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type Router struct {
	Engine *gin.Engine
}

func NewRouter(cfg config.IConfig, api *apis.PingPongAPI, userAPI *apis.UserAPI, fileAPI *apis.FileAPI, categoryAPI *apis.CategoryAPI, articleAPI *apis.ArticleAPI, cors middlewares.ICORSMiddleware, auth middlewares.IAuthMiddleware) *Router {
	if cfg.GetApp().Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	// Bật fallback *gin.Context -> Request.Context() cho Deadline/Done/Err/
	// Value: middleware nhét user_id vào request context chuẩn, controller
	// truyền thẳng `c` xuống service nên service mới đọc được qua ctx.Value.
	// Mặc định gin tắt cờ này (Value trả nil với key không phải string).
	engine.ContextWithFallback = true
	// Middleware global (recovery + CORS) chạy cho mọi request, kể cả
	// preflight OPTIONS không mang token — xem middlewares.Global.
	engine.Use(middlewares.Global(cors)...)

	engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := engine.Group("/api/v1")
	{
		v1.GET("/ping", api.Ping)
		v1.GET("/pong", api.Pong)
		v1.POST("/users", userAPI.Create)
		v1.POST("/auth/login", userAPI.Login)
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
	}

	return &Router{Engine: engine}
}
