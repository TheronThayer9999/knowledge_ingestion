package domain

import (
	"context"
	"errors"
	"time"
)

// Lý do thu hồi — ghi vào refresh_tokens.revoke_reason để đối soát sau này
// (ai/ cái gì đã đá phiên). Giá trị cố định để log và filter không lệch nhau.
const (
	RefreshRevokeLogout   = "logout"
	RefreshRevokeReuse    = "reuse_detected"
	RefreshRevokePassword = "password_changed"
	RefreshRevokeAdmin    = "admin"
)

// Lỗi nghiệp vụ của luồng refresh — repository dịch từ trạng thái row sang
// sentinel để service chỉ cần errors.Is, không phải biết SQL. Mọi lỗi này
// đều map về CÙNG một 401 ở API (không phân biệt cho client biết token sai
// loại gì — tránh dò token).
var (
	ErrRefreshNotFound = errors.New("refresh token không tồn tại")
	ErrRefreshExpired  = errors.New("refresh token đã hết hạn")
	ErrRefreshRevoked  = errors.New("refresh token đã bị thu hồi")
	ErrRefreshReuse    = errors.New("refresh token đã dùng rồi lại bị dùng lại")
)

// RefreshToken là 1 dòng trong chuỗi xoay vòng của một phiên đăng nhập.
//
// VÌ SAO CÓ BẢNG NÀY: JWT access token không thể thu hồi trước hạn (server
// không giữ state). Refresh token thì có state, nên logout/đổi pass/khóa acc
// mới đá được phiên. Token gốc KHÔNG lưu — chỉ lưu SHA-256, DB bị đọc cũng
// không dùng lại được.
//
// Một lần login = một FamilyID. Mỗi lần refresh sinh dòng mới cùng FamilyID
// và đánh dấu dòng cũ đã xoay (RotatedAt) — chuỗi này gọi là "family". Dùng
// lại một dòng đã xoay là dấu hiệu token bị copy → thu hồi cả family.
type RefreshToken struct {
	BaseModel
	UserID          int64      `gorm:"not null;index" json:"user_id"`
	FamilyID        string     `gorm:"type:uuid;not null;index" json:"family_id"`
	TokenHash       string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	ExpiresAt       time.Time  `gorm:"not null;index" json:"expires_at"`
	FamilyExpiresAt time.Time  `gorm:"not null" json:"family_expires_at"`
	RotatedAt       *time.Time `json:"rotated_at,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	RevokeReason    string     `gorm:"size:32" json:"revoke_reason,omitempty"`
	UserAgent       string     `gorm:"size:255" json:"user_agent,omitempty"`
	IP              string     `gorm:"size:64" json:"ip,omitempty"`
}

func (RefreshToken) TableName() string {
	return "refresh_tokens"
}

// IRefreshTokenRepository là contract của kho refresh token. Rotate phải
// NGUYÊN TỬ (khóa row + xoay + chèn dòng mới trong 1 transaction), nếu không
// 2 tab cùng refresh một token sẽ sinh 2 nhánh và lần sau bị coi là reuse oan.
type IRefreshTokenRepository interface {
	Create(ctx context.Context, token *RefreshToken) error
	// Rotate khóa dòng theo hash, kiểm tra trạng thái rồi đánh dấu rotated và
	// chèn next. Trả dòng CŨ (kể cả khi lỗi, nếu tìm thấy) để service log
	// user_id/family_id. Lỗi: ErrRefreshNotFound/Expired/Revoked/Reuse.
	Rotate(ctx context.Context, hash string, next *RefreshToken) (*RefreshToken, error)
	// RevokeFamilyByHash thu hồi mọi token còn sống của family chứa hash —
	// idempotent: token không tồn tại trả (0, nil), không lộ thông tin.
	RevokeFamilyByHash(ctx context.Context, hash, reason string) (int64, error)
	// RevokeAllByUser thu hồi toàn bộ phiên của user (đổi pass, khóa acc).
	RevokeAllByUser(ctx context.Context, userID int64, reason string) (int64, error)
	// DeleteExpired xoá CỨNG token hết hạn hoặc đã thu hồi trước mốc before —
	// janitor chạy định kỳ, không để bảng phình mãi.
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}
