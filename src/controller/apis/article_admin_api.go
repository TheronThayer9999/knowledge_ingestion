package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// ArticleAdminAPI vận hành hàng đợi tri thức — controller chỉ validate
// (Bind) rồi Render, nghiệp vụ nằm ở service. Route đã gate Role admin
// (RequireRole) trong router.
type ArticleAdminAPI struct {
	*baseController
	svc services.IArticleAdminService
}

func NewArticleAdminAPI(base *baseController, svc services.IArticleAdminService) *ArticleAdminAPI {
	return &ArticleAdminAPI{baseController: base, svc: svc}
}

// Rebuild godoc
// @Summary Rebuild vectors for articles
// @Description Đưa bài về hàng đợi index từ đầu (Qdrant mất collection, chuyển cụm mới). Scope bắt buộc đúng 1 trong article_ids hoặc category_id, chỉ tác động bài của chính mình. Worker hốt dần theo nhịp claim.
// @Tags admin
// @Accept json
// @Produce json
// @Param request body dtos.RebuildVectorsRequest true "rebuild scope"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/admin/rebuild-vectors [post]
func (a *ArticleAdminAPI) Rebuild(c *gin.Context) {
	req := &dtos.RebuildVectorsRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.RebuildVectors(c, req))
}

// ResetExtract godoc
// @Summary Re-extract text (re-run OCR) for articles
// @Description Xoá sạch trang đã trích rồi đưa bài về hàng đợi extract để worker OCR lại từ blob — dùng khi muốn cải thiện chất lượng trích (đổi renderer OCR, đổi chính sách OCR). KHÔNG đụng vector đã index, nên search vẫn chạy bình thường cho tới khi gọi rebuild-vectors. Scope bắt buộc đúng 1 trong article_ids hoặc category_id, chỉ tác động bài của chính mình. Cảnh báo: tốn OCR lại cho các bài này — file scan 500 trang là ~17 phút mỗi bài.
// @Tags admin
// @Accept json
// @Produce json
// @Param request body dtos.ResetScopeRequest true "reset extract scope"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 403 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/admin/reset-extract [post]
func (a *ArticleAdminAPI) ResetExtract(c *gin.Context) {
	req := &dtos.ResetScopeRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.ResetExtract(c, req))
}
