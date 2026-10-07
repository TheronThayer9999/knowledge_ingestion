package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ArticleAPI là controller tài liệu tri thức theo từng user — owner lấy từ
// token (route đã qua RequiredAuth), controller chỉ validate (Bind + parse
// id/query) rồi Render, mọi nghiệp vụ nằm ở service.
type ArticleAPI struct {
	*baseController
	svc services.IArticleService
}

func NewArticleAPI(base *baseController, svc services.IArticleService) *ArticleAPI {
	return &ArticleAPI{baseController: base, svc: svc}
}

// parseArticleID đọc :id trên path thành int64 — sai format thì 400 sớm,
// khỏi gọi service.
func (a *ArticleAPI) parseArticleID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		a.BadRequest(c, "id bài viết không hợp lệ")
		return 0, false
	}
	return id, true
}

// Create godoc
// @Summary Create an article
// @Description Creates a knowledge article inside one of my categories
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
	id, ok := a.parseArticleID(c)
	if !ok {
		return
	}
	Render(c, a, a.svc.GetByID(c, id))
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
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "0"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	rawID := c.Query("category_id")
	name := c.Query("category_name")
	if rawID != "" && name != "" {
		a.BadRequest(c, "chỉ dùng một trong category_id hoặc category_name")
		return
	}
	if rawID != "" {
		categoryID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || categoryID <= 0 {
			a.BadRequest(c, "category_id không hợp lệ")
			return
		}
		Render(c, a, a.svc.ListByCategoryID(c, categoryID, limit, offset))
		return
	}
	if name == "" {
		a.BadRequest(c, "cần category_id hoặc category_name")
		return
	}
	Render(c, a, a.svc.ListByCategoryName(c, name, limit, offset))
}

// Update godoc
// @Summary Update an article
// @Description Partial update — only sent fields change (URL is immutable)
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
	id, ok := a.parseArticleID(c)
	if !ok {
		return
	}
	req := &dtos.UpdateArticleRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Update(c, id, req))
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
	id, ok := a.parseArticleID(c)
	if !ok {
		return
	}
	Render(c, a, a.svc.Delete(c, id))
}
