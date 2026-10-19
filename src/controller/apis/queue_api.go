package apis

import (
	"knowledge_ingestion/src/controller/dtos"

	"github.com/gin-gonic/gin"
)

// Queue godoc
// @Summary Inspect extract/index queue and pending purges
// @Description Read-only toàn cảnh hàng đợi: số bài theo trạng thái extract (phase 0, bóc text) và index (phase 1, cắt chunk + embed), bài đang processing kèm lease (lease_expired=true là worker rớt, không phải đang chạy), bài failed, bài pending, và bài đã xóa mềm chia theo cửa sổ undo. Không ghi gì — gọi lại thoải mái. Toàn hệ thống, không giới hạn theo user
// @Tags admin
// @Produce json
// @Param limit query int false "số bài mỗi nhóm (default 20, max 100)"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/admin/queue [get]
func (a *ArticleAdminAPI) Queue(c *gin.Context) {
	var q dtos.ListQueueQuery
	if !a.BindQuery(c, &q) {
		return
	}
	Render(c, a, a.svc.QueueStatus(c, q.Limit))
}
