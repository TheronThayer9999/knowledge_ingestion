package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

type UserAPI struct {
	*baseController
	svc services.IUserService
}

func NewUserAPI(base *baseController, svc services.IUserService) *UserAPI {
	return &UserAPI{baseController: base, svc: svc}
}

// Create godoc
// @Summary Create a new user
// @Description Hashes the password and creates a new user
// @Tags users
// @Accept json
// @Produce json
// @Param request body dtos.CreateUserRequest true "user payload"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Router /api/v1/users [post]
func (a *UserAPI) Create(c *gin.Context) {
	req := &dtos.CreateUserRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Create(c, req))
}

// GetAllUser godoc
// @Summary List all users
// @Description Returns all users with total count
// @Tags users
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Router /api/v1/users [get]
func (a *UserAPI) GetAllUser(c *gin.Context) {
	Render(c, a, a.svc.GetAllUser(c))
}
