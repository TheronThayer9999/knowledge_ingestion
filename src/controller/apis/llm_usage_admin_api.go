package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// LLMUsageAdminAPI báo cáo chi phí LLM theo tài khoản — controller chỉ validate
// (BindQuery) rồi Render, nghiệp vụ nằm ở service. Route đã gate Role admin
// (RequireRole) trong router.
type LLMUsageAdminAPI struct {
	*baseController
	svc services.ILLMUsageAdminService
}

func NewLLMUsageAdminAPI(base *baseController, svc services.ILLMUsageAdminService) *LLMUsageAdminAPI {
	return &LLMUsageAdminAPI{baseController: base, svc: svc}
}

// List godoc
// @Summary LLM usage report per account
// @Description Báo cáo chi phí LLM theo tài khoản (số lượt gọi + token đầu vào/ra theo phase chat/compile/classify, provider và model), tổng hợp theo NGÀY. Dùng để quản lý: biết acc nào đang tốn token. Chỉ báo cáo, KHÔNG chặn dùng. Lọc: from/to (yyyy-mm-dd), user_id (1 tài khoản), site (chat/compile/classify), limit/offset.
// @Tags admin
// @Produce json
// @Param from query string false "ngày bắt đầu (yyyy-mm-dd)"
// @Param to query string false "ngày kết thúc (yyyy-mm-dd)"
// @Param user_id query int false "lọc đúng 1 tài khoản"
// @Param site query string false "lọc theo phase" Enums(chat,compile,classify)
// @Param limit query int false "số dòng (mặc định 200, max 500)"
// @Param offset query int false "bỏ qua N dòng"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/admin/llm-usage [get]
func (a *LLMUsageAdminAPI) List(c *gin.Context) {
	var q dtos.LLMUsageListQuery
	if !a.BindQuery(c, &q) {
		return
	}
	Render(c, a, a.svc.List(c, q))
}
