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

// Download godoc
// @Summary Get a download URL for an article's original file
// @Description Signs a short-lived GET URL (5 minutes) so the browser pulls the file straight from object storage instead of streaming it through this API. The signature carries Content-Disposition attachment with the article name (RFC 5987, so Vietnamese diacritics survive). 400 for URL-source articles, 404 when the article is not mine
// @Tags articles
// @Produce json
// @Param id path int true "article id"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/articles/{id}/download [get]
func (a *ArticleAPI) Download(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id bài viết không hợp lệ") {
		return
	}
	Render(c, a, a.svc.DownloadURL(c, p.ID))
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

// ListStatuses godoc
// @Summary Pipeline status of my articles
// @Description Lightweight poll endpoint for the documents UI: only id + extract/index/compile status, no metadata
// @Tags articles
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/pipeline/articles [get]
func (a *ArticleAPI) ListStatuses(c *gin.Context) {
	Render(c, a, a.svc.ListStatuses(c))
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

// ListSections godoc
// @Summary List article sections (PageIndex)
// @Description Returns the PageIndex of one of my articles: sections (Dieu/chapter/phan) in reading order with the chunk index each starts at. Empty sections with index_status != done means the article is not indexed yet
// @Tags articles
// @Produce json
// @Param id path int true "article id"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/articles/{id}/sections [get]
func (a *ArticleAPI) ListSections(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id bài viết không hợp lệ") {
		return
	}
	Render(c, a, a.svc.ListSections(c, p.ID))
}

// GetTree godoc
// @Summary Article section tree (knowledge tree)
// @Description Returns the sections of one of my articles nested by heading hierarchy (Chuong/Phan > Muc > Dieu/Bai) with compiled summaries; flat roots when the document has no chapter headings or is not compiled yet
// @Tags articles
// @Produce json
// @Param id path int true "article id"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/articles/{id}/tree [get]
func (a *ArticleAPI) GetTree(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id bài viết không hợp lệ") {
		return
	}
	Render(c, a, a.svc.GetTree(c, p.ID))
}
