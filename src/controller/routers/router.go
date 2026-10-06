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

func NewRouter(cfg config.IConfig, api *apis.PingPongAPI, userAPI *apis.UserAPI, fileAPI *apis.FileAPI, cors middlewares.ICORSMiddleware, auth middlewares.IAuthMiddleware) *Router {
	if cfg.GetApp().Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
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
	}

	return &Router{Engine: engine}
}
