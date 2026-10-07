package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// CategoryAPI là controller taxonomy danh mục theo từng user — owner lấy từ
// token (route đã qua RequiredAuth), controller chỉ validate (Bind + parse
// id) rồi Render, mọi nghiệp vụ nằm ở service.
type CategoryAPI struct {
	*baseController
	svc services.ICategoryService
}

func NewCategoryAPI(base *baseController, svc services.ICategoryService) *CategoryAPI {
	return &CategoryAPI{baseController: base, svc: svc}
}

// Create godoc
// @Summary Create a category
// @Description Creates a new taxonomy category, optionally under a parent
// @Tags categories
// @Accept json
// @Produce json
// @Param request body dtos.CreateCategoryRequest true "category payload"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/categories [post]
func (a *CategoryAPI) Create(c *gin.Context) {
	// Route đã qua RequiredAuth nên user_id chắc chắn có trong context.
	userID, _ := middlewares.GetUserID(c)
	req := &dtos.CreateCategoryRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Create(c, userID, req))
}

// GetAll godoc
// @Summary List my categories
// @Description Returns my taxonomy tree with total count
// @Tags categories
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/categories [get]
func (a *CategoryAPI) GetAll(c *gin.Context) {
	userID, _ := middlewares.GetUserID(c)
	Render(c, a, a.svc.GetAll(c, userID))
}

// GetById godoc
// @Summary Get a category by id
// @Description Returns a single category or 404
// @Tags categories
// @Produce json
// @Param id path int true "category id"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/categories/{id} [get]
func (a *CategoryAPI) GetById(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id danh mục không hợp lệ") {
		return
	}
	userID, _ := middlewares.GetUserID(c)
	Render(c, a, a.svc.GetById(c, p.ID, userID))
}

// Update godoc
// @Summary Update a category
// @Description Partial update — only sent fields change
// @Tags categories
// @Accept json
// @Produce json
// @Param id path int true "category id"
// @Param request body dtos.UpdateCategoryRequest true "fields to update"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/categories/{id} [put]
func (a *CategoryAPI) Update(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id danh mục không hợp lệ") {
		return
	}
	req := &dtos.UpdateCategoryRequest{}
	if !a.Bind(c, req) {
		return
	}
	userID, _ := middlewares.GetUserID(c)
	Render(c, a, a.svc.Update(c, p.ID, userID, req))
}

// Delete godoc
// @Summary Delete a category
// @Description Deletes a category that has no children
// @Tags categories
// @Produce json
// @Param id path int true "category id"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/categories/{id} [delete]
func (a *CategoryAPI) Delete(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id danh mục không hợp lệ") {
		return
	}
	userID, _ := middlewares.GetUserID(c)
	Render(c, a, a.svc.Delete(c, p.ID, userID))
}
