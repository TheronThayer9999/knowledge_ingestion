package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// ArticleAPI là controller tài liệu tri thức theo từng user — owner lấy từ
// token (route đã qua RequiredAuth cho ICurrentUser, service tự đọc),
// controller chỉ validate (Bind/BindUri/BindQuery) rồi Render, mọi nghiệp
// vụ nằm ở service.
type ArticleAPI struct {
	*baseController
	svc services.IArticleService
}

func NewArticleAPI(base *baseController, svc services.IArticleService) *ArticleAPI {
	return &ArticleAPI{baseController: base, svc: svc}
}

// Create godoc
// @Summary Create an article
// @Description Creates a knowledge article inside one of my categories. Source is exactly one of: url (external web link) or storage_key (object already uploaded via presign, e.g. uploads/<uuid>.pdf — server verifies it with HeadObject before saving)
// @Tags articles
// @Accept json
// @Produce json
// @Param request body dtos.CreateArticleRequest true "article payload"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/articles [post]
func (a *ArticleAPI) Create(c *gin.Context) {
	req := &dtos.CreateArticleRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Create(c, req))
}

// GetByID godoc
// @Summary Get an article by id
// @Description Returns a single article or 404
// @Tags articles
// @Produce json
// @Param id path int true "article id"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/articles/{id} [get]
func (a *ArticleAPI) GetByID(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id bài viết không hợp lệ") {
		return
	}
	Render(c, a, a.svc.GetByID(c, p.ID))
}

// List godoc
// @Summary List my articles
// @Description Lists my articles filtered by exactly one of category_id or category_name
// @Tags articles
// @Produce json
// @Param category_id query int false "filter by category id (one of category_id/category_name required)"
// @Param category_name query string false "filter by exact category name (one of category_id/category_name required)"
// @Param limit query int false "page size (default 20, max 100)"
// @Param offset query int false "page offset"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/articles [get]
func (a *ArticleAPI) List(c *gin.Context) {
	var q dtos.ListArticlesQuery
	if !a.BindQuery(c, &q) {
		return
	}
	Render(c, a, a.svc.List(c, &q))
}

// Update godoc
// @Summary Update an article
// @Description Partial update — only sent fields change (source url/storage_key is immutable)
// @Tags articles
// @Accept json
// @Produce json
// @Param id path int true "article id"
// @Param request body dtos.UpdateArticleRequest true "fields to update"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/articles/{id} [put]
func (a *ArticleAPI) Update(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id bài viết không hợp lệ") {
		return
	}
	req := &dtos.UpdateArticleRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Update(c, p.ID, req))
}

// Delete godoc
// @Summary Delete an article
// @Description Deletes one of my articles
// @Tags articles
// @Produce json
// @Param id path int true "article id"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/articles/{id} [delete]
func (a *ArticleAPI) Delete(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id bài viết không hợp lệ") {
		return
	}
	Render(c, a, a.svc.Delete(c, p.ID))
}
