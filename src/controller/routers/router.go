package routers

import (
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/apis"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type Router struct {
	Engine *gin.Engine
}

func NewRouter(cfg config.IConfig, api *apis.PingPongAPI, userAPI *apis.UserAPI) *Router {
	if cfg.GetApp().Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.Use(gin.Recovery())

	engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := engine.Group("/api/v1")
	{
		v1.GET("/ping", api.Ping)
		v1.GET("/pong", api.Pong)
		v1.POST("/users", userAPI.Create)
	}

	return &Router{Engine: engine}
}
