package routers

import (
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/apis"

	"github.com/gin-gonic/gin"
)

type Router struct {
	Engine *gin.Engine
}

func NewRouter(cfg config.IConfig, api *apis.PingPongAPI) *Router {
	if cfg.GetApp().Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.Use(gin.Recovery())

	v1 := engine.Group("/api/v1")
	{
		v1.GET("/ping", api.Ping)
		v1.GET("/pong", api.Pong)
	}

	return &Router{Engine: engine}
}
