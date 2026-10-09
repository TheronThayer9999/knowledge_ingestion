package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// ArticleAdminAPI vận hành hàng đợi tri thức — controller chỉ validate
// (Bind) rồi Render, nghiệp vụ nằm ở service. Gate Role để sau (endpoint
// hiện nằm sau auth thường như mọi route articles).
type ArticleAdminAPI struct {
	*baseController
	svc services.IArticleAdminService
}

func NewArticleAdminAPI(base *baseController, svc services.IArticleAdminService) *ArticleAdminAPI {
	return &ArticleAdminAPI{baseController: base, svc: svc}
}

// Rebuild godoc
// @Summary Rebuild vectors for articles
// @Description Đưa bài về hàng đợi chunk→embed từ đầu (Qdrant mất collection, chuyển cụm mới). Scope bắt buộc đúng 1 trong article_ids hoặc category_id, chỉ tác động bài của chính mình. Worker hốt dần theo nhịp claim.
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
