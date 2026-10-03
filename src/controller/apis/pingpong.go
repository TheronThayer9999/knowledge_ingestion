package apis

import (
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

type PingPongAPI struct {
	*baseController
	svc services.IPingPongService
}

func NewPingPongAPI(base *baseController, svc services.IPingPongService) *PingPongAPI {
	return &PingPongAPI{baseController: base, svc: svc}
}

// Ping godoc
// @Summary Ping the server
// @Description Returns pong with server timestamp
// @Tags health
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Router /api/v1/ping [get]
func (a *PingPongAPI) Ping(c *gin.Context) {
	res := a.svc.Ping()
	Render(c, a.baseController, res)
}

// Pong godoc
// @Summary Pong the server
// @Description Returns ping with server timestamp
// @Tags health
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Router /api/v1/pong [get]
func (a *PingPongAPI) Pong(c *gin.Context) {
	res := a.svc.Pong()
	Render(c, a.baseController, res)
}
