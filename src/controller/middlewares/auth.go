package middlewares

import (
	"context"
	"net/http"
	"strings"
	"time"

	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Context key lưu thông tin user đã xác thực — handler lấy qua GetUserID.
const CtxUserID = "user_id"

// Claims là payload nhét vào JWT: ai (user_id/username) + hết hạn khi nào +
// PwdChangedAt là mốc đổi pass lúc ký — Handler so với DB để đá token cũ.
type Claims struct {
	UserID       int64  `json:"user_id"`
	Username     string `json:"username"`
	PwdChangedAt int64  `json:"pwd_changed_at"`
	jwt.RegisteredClaims
}

// IAuthMiddleware là contract xác thực JWT — service Login gọi IssueToken để
// phát token, router gắn Handler vào chain Protected. Constructor
// NewAuthMiddleware giữ secret + repo user để verify token với DB.
type IAuthMiddleware interface {
	// Handler trả middleware chặn request không JWT hợp lệ.
	Handler() gin.HandlerFunc
	// IssueToken ký JWT HS256 cho user vừa login thành công.
	IssueToken(userID int64, username string, pwdChangedAt time.Time) (token string, expiresAt time.Time, err error)
	// ParseToken verify chữ ký + hạn của token, trả claims khi hợp lệ.
	ParseToken(tokenString string) (*Claims, error)
	// VerifyToken verify trọn vẹn 1 token (chữ ký + hạn + đối chiếu DB: acc
	// bị khóa hoặc đổi pass sau khi ký đều rớt) — gin Handler và gRPC
	// interceptor dùng chung để 2 transport thu hồi tức thì như nhau.
	VerifyToken(ctx context.Context, tokenString string) (int64, error)
}

type authMiddleware struct {
	jwtCfg config.JWTConfig
	users  domain.IUserRepositoryImpl
}

// NewAuthMiddleware dựng auth từ config + repo user — fx gọi một lần lúc
// startup. Repo dùng để đối chiếu token với DB mỗi request (đá token cũ
// khi đổi pass/khóa acc).
func NewAuthMiddleware(cfg config.IConfig, users domain.IUserRepositoryImpl) IAuthMiddleware {
	return &authMiddleware{jwtCfg: cfg.GetJWT(), users: users}
}

// IssueToken ký JWT HS256 cho user vừa login thành công. pwdChangedAt phải
// là User.PasswordChangedAt hiện tại — đổi pass sau này làm token này rớt
// ngay ở Handler. Trả token + thời điểm hết hạn để client biết khi nào
// cần login lại.
func (m *authMiddleware) IssueToken(userID int64, username string, pwdChangedAt time.Time) (token string, expiresAt time.Time, err error) {
	expiresAt = time.Now().Add(time.Duration(m.jwtCfg.ExpiryMinutes) * time.Minute)
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID:       userID,
		Username:     username,
		PwdChangedAt: pwdChangedAt.Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	})
	token, err = t.SignedString([]byte(m.jwtCfg.Secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// ParseToken verify chữ ký + hạn của token, trả claims khi hợp lệ.
func (m *authMiddleware) ParseToken(tokenString string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return []byte(m.jwtCfg.Secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := t.Claims.(*Claims)
	if !ok || !t.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}
	return claims, nil
}

// VerifyToken verify trọn vẹn 1 token: chữ ký + hạn + đối chiếu DB (tài
// khoản bị khóa hoặc đổi pass sau khi ký đều rớt) — trả user_id khi hợp lệ.
func (m *authMiddleware) VerifyToken(ctx context.Context, tokenString string) (int64, error) {
	claims, err := m.ParseToken(tokenString)
	if err != nil {
		return 0, err
	}
	user, err := m.users.GetUserById(ctx, claims.UserID)
	if err != nil || user == nil {
		return 0, jwt.ErrTokenInvalidClaims
	}
	if !user.Active {
		return 0, jwt.ErrTokenInvalidClaims
	}
	if user.PasswordChangedAt.Unix() != claims.PwdChangedAt {
		return 0, jwt.ErrTokenInvalidClaims
	}
	return claims.UserID, nil
}

// Handler trả middleware chặn request không có/không đúng JWT — client gửi
// `Authorization: Bearer <token>` (token lấy từ POST /auth/login).
// Verify trọn vẹn qua VerifyToken (chữ ký + hạn + đối chiếu DB mỗi request:
// tài khoản bị khóa hoặc đã đổi pass sau khi token được ký đều 401 ngay).
// Đánh đổi 1 query/request để thu hồi tức thì — sau này cache redis nếu nóng.
func (m *authMiddleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			abortUnauthorized(c, "thiếu token xác thực")
			return
		}
		userID, err := m.VerifyToken(c.Request.Context(), strings.TrimSpace(token))
		if err != nil {
			abortUnauthorized(c, "token không hợp lệ hoặc đã hết hạn")
			return
		}
		c.Set(CtxUserID, userID)
		// Nhét tiếp vào request context chuẩn để tầng service đọc qua
		// ICurrentUser mà không cần biết gin.
		c.Request = c.Request.WithContext(WithUserID(c.Request.Context(), userID))
		c.Next()
	}
}

// GetUserID lấy user_id mà Handler đã nhét vào context.
func GetUserID(c *gin.Context) (int64, bool) {
	id, ok := c.Get(CtxUserID)
	if !ok {
		return 0, false
	}
	uid, ok := id.(int64)
	return uid, ok
}

// abortUnauthorized trả envelope lỗi chuẩn của project với status 401.
func abortUnauthorized(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, dtos.ResponseResource{
		Status:  false,
		Code:    http.StatusUnauthorized,
		Message: message,
	})
}
