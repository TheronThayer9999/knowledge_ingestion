package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
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
// @Security BearerAuth
// @Router /api/v1/users [get]
func (a *UserAPI) GetAllUser(c *gin.Context) {
	Render(c, a, a.svc.GetAllUser(c))
}

// Login godoc
// @Summary Login with username and password
// @Description Verifies credentials and returns a JWT for calling protected APIs via Authorization: Bearer
// @Tags auth
// @Accept json
// @Produce json
// @Param request body dtos.LoginUserRequest true "credentials"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 401 {object} dtos.ResponseResource
// @Router /api/v1/auth/login [post]
func (a *UserAPI) Login(c *gin.Context) {
	req := &dtos.LoginUserRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Login(c, req))
}

// ChangePassword godoc
// @Summary Change my password
// @Description Verifies the old password then sets a new one; all previously issued tokens are invalidated immediately
// @Tags auth
// @Accept json
// @Produce json
// @Param request body dtos.ChangePasswordRequest true "passwords"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 401 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/auth/change-password [post]
func (a *UserAPI) ChangePassword(c *gin.Context) {
	// Route đã qua RequiredAuth nên user_id chắc chắn có trong context.
	userID, _ := middlewares.GetUserID(c)
	req := &dtos.ChangePasswordRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.ChangePassword(c, userID, req))
}

// Profile godoc
// @Summary Get my profile
// @Description Returns the profile of the user identified by the JWT
// @Tags auth
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Failure 401 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/auth/profile [get]
func (a *UserAPI) Profile(c *gin.Context) {
	// Route đã qua RequiredAuth nên user_id chắc chắn có trong context.
	userID, _ := middlewares.GetUserID(c)
	Render(c, a, a.svc.Profile(c, userID))
}
