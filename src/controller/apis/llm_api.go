package apis

import (
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/controller/middlewares"

	"github.com/gin-gonic/gin"
)

// LLMAPI phản chiếu cấu hình provider/model ra UI (Đợt 28) — chỉ đọc, không
// bao giờ trả api_key. Role lấy từ token để service quyết định field
// admin-only, client không tự khai role.
type LLMAPI struct {
	*baseController
	svc port.ILLMOptions
}

func NewLLMAPI(base *baseController, svc port.ILLMOptions) *LLMAPI {
	return &LLMAPI{baseController: base, svc: svc}
}

// Options godoc
// @Summary Danh sách provider/model cho UI
// @Description Trả danh sách provider đã cấu hình (sắp theo slug) kèm model gợi ý, trạng thái khả dụng và nhãn suy luận sâu. Không bao giờ trả api_key. `base_url`/`has_key` chỉ có với admin (role ≥ 1).
// @Tags llm
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Failure 401 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/llm/options [get]
func (a *LLMAPI) Options(c *gin.Context) {
	role, _ := middlewares.GetUserRole(c)
	Render(c, a, a.svc.Options(c, role))
}
