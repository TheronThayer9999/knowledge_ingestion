package middlewares

import (
	"net/http"
	"strings"
	"time"

	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Context key lưu thông tin user đã xác thực — handler lấy qua GetUserID/GetUsername.
const (
	CtxUserID   = "user_id"
	CtxUsername = "username"
)

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

// Handler trả middleware chặn request không có/không đúng JWT — client gửi
// `Authorization: Bearer <token>` (token lấy từ POST /auth/login).
// Ngoài chữ ký + hạn còn đối chiếu với DB mỗi request: tài khoản bị khóa
// hoặc đã đổi pass sau khi token được ký đều 401 ngay. Đánh đổi 1
// query/request để thu hồi tức thì — sau này cache redis nếu nóng.
func (m *authMiddleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			abortUnauthorized(c, "thiếu token xác thực")
			return
		}
		claims, err := m.ParseToken(strings.TrimSpace(token))
		if err != nil {
			abortUnauthorized(c, "token không hợp lệ hoặc đã hết hạn")
			return
		}
		user, err := m.users.GetUserById(c.Request.Context(), claims.UserID)
		if err != nil || user == nil {
			abortUnauthorized(c, "token không hợp lệ hoặc đã hết hạn")
			return
		}
		if !user.Active {
			abortUnauthorized(c, "tài khoản đã bị khóa")
			return
		}
		if user.PasswordChangedAt.Unix() != claims.PwdChangedAt {
			abortUnauthorized(c, "mật khẩu đã đổi, vui lòng đăng nhập lại")
			return
		}
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxUsername, claims.Username)
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

// GetUsername lấy username mà Handler đã nhét vào context.
func GetUsername(c *gin.Context) (string, bool) {
	name, ok := c.Get(CtxUsername)
	if !ok {
		return "", false
	}
	username, ok := name.(string)
	return username, ok
}

// abortUnauthorized trả envelope lỗi chuẩn của project với status 401.
func abortUnauthorized(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, dtos.ResponseResource{
		Status:  false,
		Code:    http.StatusUnauthorized,
		Message: message,
	})
}
