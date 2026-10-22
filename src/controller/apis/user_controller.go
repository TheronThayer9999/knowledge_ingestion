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
	Render(c, a, a.svc.Login(c, req, sessionMeta(c)))
}

// Refresh godoc
// @Summary Exchange a refresh token for a new token pair
// @Description Rotates the refresh token. Each token is single-use; presenting an already rotated token revokes the whole session family.
// @Tags auth
// @Accept json
// @Produce json
// @Param request body dtos.RefreshTokenRequest true "refresh token"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 401 {object} dtos.ResponseResource
// @Router /api/v1/auth/refresh [post]
func (a *UserAPI) Refresh(c *gin.Context) {
	req := &dtos.RefreshTokenRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Refresh(c, req, sessionMeta(c)))
}

// Logout godoc
// @Summary Revoke the current session
// @Description Revokes every refresh token in the same session family. Idempotent: unknown tokens still return success.
// @Tags auth
// @Accept json
// @Produce json
// @Param request body dtos.LogoutRequest true "refresh token"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Router /api/v1/auth/logout [post]
func (a *UserAPI) Logout(c *gin.Context) {
	req := &dtos.LogoutRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Logout(c, req))
}

// LogoutAll godoc
// @Summary Revoke every session of the current user
// @Description Revokes all refresh tokens of the user in the JWT — use after suspected token theft.
// @Tags auth
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Failure 401 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/auth/logout-all [post]
func (a *UserAPI) LogoutAll(c *gin.Context) {
	// Route đã qua RequiredAuth nên user_id chắc chắn có trong context.
	userID, _ := middlewares.GetUserID(c)
	Render(c, a, a.svc.LogoutAll(c, userID))
}

// sessionMeta gom thông tin audit của phiên (UA/IP) do server tự lấy từ
// request — không nhận từ body để client không tự khai man.
func sessionMeta(c *gin.Context) dtos.SessionMeta {
	return dtos.SessionMeta{UserAgent: c.Request.UserAgent(), IP: c.ClientIP()}
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
