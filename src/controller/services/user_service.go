package services

import (
	"context"
	stderrors "errors"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type IUserService interface {
	Create(ctx context.Context, dto *dtos.CreateUserRequest) dtos.Result[*dtos.UserResponse]
	GetAllUser(ctx context.Context) dtos.Result[dtos.UserListResponse]
	Login(ctx context.Context, dto *dtos.LoginUserRequest) dtos.Result[*dtos.LoginUserResponse]
	ChangePassword(ctx context.Context, userID int64, dto *dtos.ChangePasswordRequest) dtos.Result[*dtos.UserResponse]
	Profile(ctx context.Context, userID int64) dtos.Result[*dtos.UserResponse]
}

type userService struct {
	auth     middlewares.IAuthMiddleware
	userRepo domain.IUserRepositoryImpl
}

func NewUserService(auth middlewares.IAuthMiddleware, userRepo domain.IUserRepositoryImpl) IUserService {
	return &userService{
		auth:     auth,
		userRepo: userRepo,
	}
}

func (u *userService) Create(ctx context.Context, dto *dtos.CreateUserRequest) dtos.Result[*dtos.UserResponse] {
	hash, err := bcrypt.GenerateFromPassword([]byte(dto.Password), bcrypt.DefaultCost)
	if err != nil {
		return dtos.Fail[*dtos.UserResponse](err)
	}
	user := dto.ToModel()
	user.Password = string(hash)
	user.Active = true

	if err := u.userRepo.CreateUser(ctx, user); err != nil {
		return dtos.Fail[*dtos.UserResponse](err)
	}
	return dtos.Ok(dtos.ToUserResponse(user))
}

func (u *userService) GetAllUser(ctx context.Context) dtos.Result[dtos.UserListResponse] {
	users, err := u.userRepo.GetAllUser(ctx)
	if err != nil {
		return dtos.Fail[dtos.UserListResponse](err)
	}
	return dtos.Ok(dtos.ToUserListResponse(users))
}

// Login tìm user theo username, so hash bcrypt, đúng thì phát JWT gắn
// PasswordChangedAt hiện tại. Sai tài khoản/mật khẩu đều 401 chung một
// message để không lộ username nào tồn tại.
func (u *userService) Login(ctx context.Context, dto *dtos.LoginUserRequest) dtos.Result[*dtos.LoginUserResponse] {
	user, err := u.userRepo.GetUserByUsername(ctx, dto.Username)
	if err != nil {
		if stderrors.Is(err, domain.ErrNotFound) {
			return dtos.Fail[*dtos.LoginUserResponse](errors.NewCustomHttpError(http.StatusUnauthorized, http.StatusUnauthorized, "sai tài khoản hoặc mật khẩu"))
		}
		return dtos.Fail[*dtos.LoginUserResponse](err)
	}
	if !user.Active {
		return dtos.Fail[*dtos.LoginUserResponse](errors.NewCustomHttpError(http.StatusForbidden, http.StatusForbidden, "tài khoản đã bị khóa"))
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(dto.Password)); err != nil {
		return dtos.Fail[*dtos.LoginUserResponse](errors.NewCustomHttpError(http.StatusUnauthorized, http.StatusUnauthorized, "sai tài khoản hoặc mật khẩu"))
	}
	token, expiresAt, err := u.auth.IssueToken(user.ID, user.UserName, user.PasswordChangedAt)
	if err != nil {
		return dtos.Fail[*dtos.LoginUserResponse](err)
	}
	now := time.Now()
	if err := u.userRepo.TouchLastLogin(ctx, user.ID, now); err != nil {
		logs.Error(err, "touch last login failed", "trace_id", utils.TraceIDFromCtx(ctx), "user_id", user.ID)
	}
	user.LastLogin = &now
	return dtos.Ok(&dtos.LoginUserResponse{
		Token:     token,
		ExpiresAt: expiresAt,
		User:      dtos.ToUserResponse(user),
	})
}

// ChangePassword đổi pass của chính mình (userID lấy từ token, không tin
// id client gửi). Verify pass cũ đúng mới cho đổi; chốt mốc
// PasswordChangedAt=now nên mọi JWT ký trước đó rớt ngay ở middleware.
func (u *userService) ChangePassword(ctx context.Context, userID int64, dto *dtos.ChangePasswordRequest) dtos.Result[*dtos.UserResponse] {
	user, err := u.userRepo.GetUserById(ctx, userID)
	if err != nil {
		if stderrors.Is(err, domain.ErrNotFound) {
			return dtos.Fail[*dtos.UserResponse](errors.NewCustomHttpError(http.StatusUnauthorized, http.StatusUnauthorized, "token không hợp lệ hoặc đã hết hạn"))
		}
		return dtos.Fail[*dtos.UserResponse](err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(dto.OldPassword)); err != nil {
		return dtos.Fail[*dtos.UserResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "mật khẩu cũ không đúng"))
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(dto.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return dtos.Fail[*dtos.UserResponse](err)
	}
	now := time.Now()
	if err := u.userRepo.UpdatePassword(ctx, user.ID, string(hash), now); err != nil {
		return dtos.Fail[*dtos.UserResponse](err)
	}
	user.PasswordChangedAt = now
	return dtos.Ok(dtos.ToUserResponse(user))
}

func (u *userService) Profile(ctx context.Context, userID int64) dtos.Result[*dtos.UserResponse] {
	user, err := u.userRepo.GetUserById(ctx, userID)
	if err != nil {
		return dtos.Fail[*dtos.UserResponse](err)
	}

	return dtos.Ok(dtos.ToUserResponse(user))
}
