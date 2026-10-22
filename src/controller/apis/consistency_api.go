package apis

import (
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// ConsistencyAPI kiểm tra tính toàn vẹn dữ liệu — admin endpoint read-only.
type ConsistencyAPI struct {
	*baseController
	svc services.IConsistencyService
}

func NewConsistencyAPI(base *baseController, svc services.IConsistencyService) *ConsistencyAPI {
	return &ConsistencyAPI{baseController: base, svc: svc}
}

// Check godoc
// @Summary Data consistency check
// @Description Kiểm tra orphan/cycle/missing self-row trong category closure, articles, article_pages. Tất cả 0 = lành mạnh.
// @Tags admin
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/admin/consistency [get]
func (a *ConsistencyAPI) Check(c *gin.Context) {
	Render(c, a, a.svc.Check(c))
}