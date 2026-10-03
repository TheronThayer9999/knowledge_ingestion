package apis

import (
	"knowledge_ingestion/src/controller/services"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

type PingPongAPI struct {
	svc services.IPingPongService
}

func NewPingPongAPI(svc services.IPingPongService) *PingPongAPI {
	return &PingPongAPI{svc: svc}
}

func (a *PingPongAPI) Ping(c *gin.Context) {
	c.JSON(http.StatusOK, a.svc.Ping())
}

func (a *PingPongAPI) Pong(c *gin.Context) {
	c.JSON(http.StatusOK, a.svc.Pong())
}

var Module = fx.Options(
	fx.Provide(NewPingPongAPI),
)
