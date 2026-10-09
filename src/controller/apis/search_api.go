package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// SearchAPI truy hồi tri thức — controller chỉ validate (Bind) rồi Render,
// nghiệp vụ nằm ở service.
type SearchAPI struct {
	*baseController
	svc services.ISearchService
}

func NewSearchAPI(base *baseController, svc services.ISearchService) *SearchAPI {
	return &SearchAPI{baseController: base, svc: svc}
}

// Query godoc
// @Summary Search knowledge chunks
// @Description Embed câu hỏi rồi tìm đoạn gần nhất trong phạm vi bài của chính mình (kèm category_id để khoanh chủ đề). Chỉ trả hit thuộc bài đã embed done.
// @Tags search
// @Accept json
// @Produce json
// @Param request body dtos.SearchRequest true "search query"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/search [post]
func (a *SearchAPI) Query(c *gin.Context) {
	req := &dtos.SearchRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Search(c, req))
}
