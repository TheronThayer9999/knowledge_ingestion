package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// EmbedCacheAdminAPI vận hành cache embed — controller chỉ validate (Bind)
// rồi Render, nghiệp vụ nằm ở service.
type EmbedCacheAdminAPI struct {
	*baseController
	svc services.IEmbedCacheAdminService
}

func NewEmbedCacheAdminAPI(base *baseController, svc services.IEmbedCacheAdminService) *EmbedCacheAdminAPI {
	return &EmbedCacheAdminAPI{baseController: base, svc: svc}
}

// Stats godoc
// @Summary Embed cache stats
// @Description Báo số entry + tổng dung lượng cache embed để monitor hit rate.
// @Tags admin
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/admin/embed-cache/stats [get]
func (a *EmbedCacheAdminAPI) Stats(c *gin.Context) {
	Render(c, a, a.svc.Stats(c))
}

// Purge godoc
// @Summary Purge unused embed cache entries
// @Description Xóa các entry ít dùng (chunk_count <= 1) cũ hơn older_than_days (mặc định 30) để giữ bảng không phình.
// @Tags admin
// @Accept json
// @Produce json
// @Param request body dtos.EmbedCachePurgeRequest false "purge options"
// @Success 200 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/admin/embed-cache/purge [post]
func (a *EmbedCacheAdminAPI) Purge(c *gin.Context) {
	req := &dtos.EmbedCachePurgeRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Purge(c, req.OlderThanDays))
}