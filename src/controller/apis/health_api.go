package apis

import (
	"net/http"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// HealthAPI kiểm tra liveness/readiness — không yêu cầu auth.
type HealthAPI struct {
	*baseController
	svc services.IHealthService
}

func NewHealthAPI(base *baseController, svc services.IHealthService) *HealthAPI {
	return &HealthAPI{baseController: base, svc: svc}
}

// Check godoc
// @Summary Health check
// @Description Kiểm tra postgres, qdrant, seaweedfs, rabbitmq. Trả 200 nếu tất cả OK, 503 nếu có thành phần chết.
// @Tags health
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Failure 503 {object} dtos.ResponseResource
// @Router /health [get]
func (a *HealthAPI) Check(c *gin.Context) {
	res := a.svc.Check(c)
	if res.Err != nil {
		Render(c, a, res)
		return
	}
	code := http.StatusOK
	if res.Data != nil && res.Data.Status != "ok" {
		code = http.StatusServiceUnavailable
	}
	status := code == http.StatusOK
	c.JSON(code, dtos.ResponseResource{Status: status, Code: code, Message: res.Data.Status, Data: res.Data})
}