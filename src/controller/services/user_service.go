package services

import (
	"context"
	stderrors "errors"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type IUserService interface {
	Create(ctx context.Context, dto *dtos.CreateUserRequest) dtos.Result[*dtos.UserResponse]
	GetAllUser(ctx context.Context) dtos.Result[dtos.UserListResponse]
	Login(ctx context.Context, dto *dtos.LoginUserRequest, meta dtos.SessionMeta) dtos.Result[*dtos.LoginUserResponse]
	Refresh(ctx context.Context, dto *dtos.RefreshTokenRequest, meta dtos.SessionMeta) dtos.Result[*dtos.RefreshTokenResponse]
	Logout(ctx context.Context, dto *dtos.LogoutRequest) dtos.Result[*dtos.LogoutResponse]
	LogoutAll(ctx context.Context, userID int64) dtos.Result[*dtos.LogoutResponse]
	ChangePassword(ctx context.Context, userID int64, dto *dtos.ChangePasswordRequest) dtos.Result[*dtos.UserResponse]
	Profile(ctx context.Context, userID int64) dtos.Result[*dtos.UserResponse]
}

type userService struct {
	auth        middlewares.IAuthMiddleware
	userRepo    domain.IUserRepositoryImpl
	refreshRepo domain.IRefreshTokenRepository
	jwtCfg      config.JWTConfig
}

func NewUserService(cfg config.IConfig, auth middlewares.IAuthMiddleware, userRepo domain.IUserRepositoryImpl, refreshRepo domain.IRefreshTokenRepository) IUserService {
	return &userService{
		auth:        auth,
		userRepo:    userRepo,
		refreshRepo: refreshRepo,
		jwtCfg:      cfg.GetJWT(),
	}
}

// errUsernameOrEmailTaken — 409 cho đăng ký trùng danh tính. Message tiếng
// Việt chỉ để debug/API; FE không hiển thị thẳng mà map status 409 sang
// key i18n riêng.
var errUsernameOrEmailTaken = errors.NewCustomHttpError(http.StatusConflict, http.StatusConflict, "tên đăng nhập hoặc email đã tồn tại")

func (u *userService) Create(ctx context.Context, dto *dtos.CreateUserRequest) dtos.Result[*dtos.UserResponse] {
	// Pre-check username/email cho message rõ ràng; unique index partial
	// (ux_users_username/ux_users_email) mới là chốt chặn cuối khi 2 request
	// đua nhau lọt qua pre-check.
	if _, err := u.userRepo.GetUserByUsername(ctx, dto.Username); err == nil {
		return dtos.Fail[*dtos.UserResponse](errUsernameOrEmailTaken)
	} else if !stderrors.Is(err, domain.ErrNotFound) {
		return dtos.Fail[*dtos.UserResponse](err)
	}
	if _, err := u.userRepo.GetUserByEmail(ctx, dto.Email); err == nil {
		return dtos.Fail[*dtos.UserResponse](errUsernameOrEmailTaken)
	} else if !stderrors.Is(err, domain.ErrNotFound) {
		return dtos.Fail[*dtos.UserResponse](err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(dto.Password), bcrypt.DefaultCost)
	if err != nil {
		return dtos.Fail[*dtos.UserResponse](err)
	}
	user := dto.ToModel()
	user.Password = string(hash)
	user.Active = true

	if err := u.userRepo.CreateUser(ctx, user); err != nil {
		if stderrors.Is(err, gorm.ErrDuplicatedKey) {
			return dtos.Fail[*dtos.UserResponse](errUsernameOrEmailTaken)
		}
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
func (u *userService) Login(ctx context.Context, dto *dtos.LoginUserRequest, meta dtos.SessionMeta) dtos.Result[*dtos.LoginUserResponse] {
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
	refreshPlain, refresh, err := u.newRefreshToken(ctx, user.ID, now, meta)
	if err != nil {
		return dtos.Fail[*dtos.LoginUserResponse](err)
	}
	if err := u.userRepo.TouchLastLogin(ctx, user.ID, now); err != nil {
		logs.Error(err, "touch last login failed", "trace_id", utils.TraceIDFromCtx(ctx), "user_id", user.ID)
	}
	user.LastLogin = &now
	return dtos.Ok(&dtos.LoginUserResponse{
		Token:            token,
		ExpiresAt:        expiresAt,
		RefreshToken:     refreshPlain,
		RefreshExpiresAt: refresh.ExpiresAt,
		User:             dtos.ToUserResponse(user),
	})
}

// newRefreshToken sinh token + chèn dòng mới. Log in là family mới (1 thiết
// bị/1 phiên); refresh thì kế thừa family cũ nên đi qua Rotate, không dùng
// helper này.
func (u *userService) newRefreshToken(ctx context.Context, userID int64, now time.Time, meta dtos.SessionMeta) (string, *domain.RefreshToken, error) {
	familyID, err := utils.NewUUIDv7()
	if err != nil {
		return "", nil, err
	}
	plain, hash, err := utils.NewOpaqueToken()
	if err != nil {
		return "", nil, err
	}
	rt := &domain.RefreshToken{
		UserID:          userID,
		FamilyID:        familyID,
		TokenHash:       hash,
		ExpiresAt:       now.Add(u.refreshTTL()),
		FamilyExpiresAt: now.Add(u.refreshAbsoluteTTL()),
		UserAgent:       meta.UserAgent,
		IP:              meta.IP,
	}
	if err := u.refreshRepo.Create(ctx, rt); err != nil {
		return "", nil, err
	}
	return plain, rt, nil
}

// Refresh đổi refresh token lấy cặp token mới. MỌI lỗi xác thực trả cùng một
// 401 — không phân biệt token không tồn tại / hết hạn / bị thu hồi / bị dùng
// lại, để người dò token không biết mình đang ở bước nào.
//
// Reuse detection: token đã xoay mà còn bị dùng nghĩa là có 2 bản sao (kẻ
// tấn công đã copy). Thu hồi CẢ family buộc kẻ tấn công và user thật đều
// phải đăng nhập lại — đánh đổi an toàn chủ động thay vì để phiên bị chiếm.
func (u *userService) Refresh(ctx context.Context, dto *dtos.RefreshTokenRequest, meta dtos.SessionMeta) dtos.Result[*dtos.RefreshTokenResponse] {
	unauthorized := dtos.Fail[*dtos.RefreshTokenResponse](errors.NewCustomHttpError(http.StatusUnauthorized, http.StatusUnauthorized, "phiên đăng nhập không hợp lệ hoặc đã hết hạn"))

	nextPlain, nextHash, err := utils.NewOpaqueToken()
	if err != nil {
		return dtos.Fail[*dtos.RefreshTokenResponse](err)
	}
	now := time.Now()
	next := &domain.RefreshToken{
		TokenHash: nextHash,
		ExpiresAt: now.Add(u.refreshTTL()),
		UserAgent: meta.UserAgent,
		IP:        meta.IP,
	}
	old, err := u.refreshRepo.Rotate(ctx, utils.HashToken(dto.RefreshToken), next)
	switch {
	case err == nil:
		// đi tiếp
	case stderrors.Is(err, domain.ErrRefreshReuse):
		var userID int64
		var familyID string
		if old != nil {
			userID, familyID = old.UserID, old.FamilyID
		}
		if familyID != "" {
			if _, revokeErr := u.refreshRepo.RevokeFamilyByHash(ctx, utils.HashToken(dto.RefreshToken), domain.RefreshRevokeReuse); revokeErr != nil {
				logs.Error(revokeErr, "auth: thu hồi family sau reuse thất bại", "user_id", userID, "family_id", familyID)
			}
		}
		logs.Warnw("auth: refresh token bị dùng lại, đã thu hồi cả phiên",
			"user_id", userID, "family_id", familyID, "ip", meta.IP)
		return unauthorized
	case stderrors.Is(err, domain.ErrRefreshNotFound),
		stderrors.Is(err, domain.ErrRefreshExpired),
		stderrors.Is(err, domain.ErrRefreshRevoked):
		return unauthorized
	default:
		return dtos.Fail[*dtos.RefreshTokenResponse](err)
	}

	user, err := u.userRepo.GetUserById(ctx, old.UserID)
	if err != nil {
		if stderrors.Is(err, domain.ErrNotFound) {
			if _, revokeErr := u.refreshRepo.RevokeAllByUser(ctx, old.UserID, domain.RefreshRevokeAdmin); revokeErr != nil {
				logs.Error(revokeErr, "auth: thu hồi token của user đã xoá thất bại", "user_id", old.UserID)
			}
			return unauthorized
		}
		return dtos.Fail[*dtos.RefreshTokenResponse](err)
	}
	if !user.Active {
		if _, revokeErr := u.refreshRepo.RevokeAllByUser(ctx, user.ID, domain.RefreshRevokeAdmin); revokeErr != nil {
			logs.Error(revokeErr, "auth: thu hồi token của user bị khóa thất bại", "user_id", user.ID)
		}
		return dtos.Fail[*dtos.RefreshTokenResponse](errors.NewCustomHttpError(http.StatusForbidden, http.StatusForbidden, "tài khoản đã bị khóa"))
	}
	token, expiresAt, err := u.auth.IssueToken(user.ID, user.UserName, user.PasswordChangedAt)
	if err != nil {
		return dtos.Fail[*dtos.RefreshTokenResponse](err)
	}
	return dtos.Ok(&dtos.RefreshTokenResponse{
		Token:            token,
		ExpiresAt:        expiresAt,
		RefreshToken:     nextPlain,
		RefreshExpiresAt: next.ExpiresAt,
	})
}

// Logout thu hồi cả family của refresh token gửi lên (idempotent). Access
// token hiện tại vẫn sống tới hết hạn — muốn chết ngay thì phải kiểm tra
// session mỗi request, xem technical_debt/AUTH_REFRESH_TOKEN.md.
func (u *userService) Logout(ctx context.Context, dto *dtos.LogoutRequest) dtos.Result[*dtos.LogoutResponse] {
	n, err := u.refreshRepo.RevokeFamilyByHash(ctx, utils.HashToken(dto.RefreshToken), domain.RefreshRevokeLogout)
	if err != nil {
		return dtos.Fail[*dtos.LogoutResponse](err)
	}
	return dtos.Ok(&dtos.LogoutResponse{Revoked: n})
}

// LogoutAll đá mọi thiết bị của user — dùng khi nghi ngờ lộ token. Route này
// qua RequiredAuth nên userID lấy từ JWT, không nhận từ client.
func (u *userService) LogoutAll(ctx context.Context, userID int64) dtos.Result[*dtos.LogoutResponse] {
	n, err := u.refreshRepo.RevokeAllByUser(ctx, userID, domain.RefreshRevokeLogout)
	if err != nil {
		return dtos.Fail[*dtos.LogoutResponse](err)
	}
	return dtos.Ok(&dtos.LogoutResponse{Revoked: n})
}

func (u *userService) refreshTTL() time.Duration {
	days := u.jwtCfg.RefreshExpiryDays
	if days <= 0 {
		days = 30
	}
	return time.Duration(days) * 24 * time.Hour
}

func (u *userService) refreshAbsoluteTTL() time.Duration {
	days := u.jwtCfg.RefreshAbsoluteDays
	if days <= 0 {
		days = 90
	}
	return time.Duration(days) * 24 * time.Hour
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
	// Đổi pass phải đá cả refresh token: PasswordChangedAt chỉ vô hiệu hoá
	// access token, còn refresh token thì có state riêng trong DB.
	if _, revokeErr := u.refreshRepo.RevokeAllByUser(ctx, user.ID, domain.RefreshRevokePassword); revokeErr != nil {
		logs.Error(revokeErr, "auth: thu hồi refresh token sau đổi mật khẩu thất bại", "trace_id", utils.TraceIDFromCtx(ctx), "user_id", user.ID)
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
