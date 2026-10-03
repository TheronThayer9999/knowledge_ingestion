package apis

import (
	"knowledge_ingestion/src/controller/services"
	"knowledge_ingestion/src/domain/dtos"
	"net/http"

	"github.com/gin-gonic/gin"
)

type PingPongAPI struct {
	svc services.IPingPongService
}

func NewPingPongAPI(svc services.IPingPongService) *PingPongAPI {
	return &PingPongAPI{svc: svc}
}

// Ping godoc
// @Summary Ping the server
// @Description Returns pong with server timestamp
// @Tags health
// @Produce json
// @Success 200 {object} dtos.PingPongResponse
// @Router /api/v1/ping [get]
func (a *PingPongAPI) Ping(c *gin.Context) {
	var resp dtos.PingPongResponse = a.svc.Ping()
	c.JSON(http.StatusOK, resp)
}

// Pong godoc
// @Summary Pong the server
// @Description Returns ping with server timestamp
// @Tags health
// @Produce json
// @Success 200 {object} dtos.PingPongResponse
// @Router /api/v1/pong [get]
func (a *PingPongAPI) Pong(c *gin.Context) {
	var resp dtos.PingPongResponse = a.svc.Pong()
	c.JSON(http.StatusOK, resp)
}
