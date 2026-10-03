package routers

import (
	"context"
	"fmt"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/apis"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

type Router struct {
	engine *gin.Engine
	server *http.Server
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

	return &Router{
		engine: engine,
		server: &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.GetApp().Port),
			Handler:           engine,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
}

func (r *Router) Run(lc fx.Lifecycle) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				if err := r.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					panic(fmt.Errorf("http server failed: %w", err))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return r.server.Shutdown(ctx)
		},
	})
}

var Module = fx.Options(
	fx.Provide(NewRouter),
	fx.Invoke((*Router).Run),
)
