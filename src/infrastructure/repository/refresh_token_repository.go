package repository

import (
	"context"
	"errors"
	"knowledge_ingestion/src/domain"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RefreshTokenRepository struct {
	db *gorm.DB
}

func NewRefreshTokenRepository(db *gorm.DB) domain.IRefreshTokenRepository {
	return &RefreshTokenRepository{db: db}
}

func (r *RefreshTokenRepository) Create(ctx context.Context, token *domain.RefreshToken) error {
	return dbConn(ctx, r.db).Create(token).Error
}

// Rotate là điểm nhạy cảm nhất của luồng refresh:
//  1. SELECT ... FOR UPDATE để 2 request cùng token không cùng đọc thấy
//     "chưa xoay" rồi sinh 2 nhánh (1 nhánh sẽ bị coi là reuse oan lần sau).
//  2. Kiểm tra revoked/expired/family-expired/reuse theo thứ tự.
//  3. Đánh dấu rotated + chèn dòng mới trong CÙNG transaction — không có
//     khoảng trống mà token cũ vừa chết vừa chưa có token mới.
//
// Khi phát hiện reuse, repo KHÔNG tự thu hồi family trong transaction này vì
// trả lỗi sẽ rollback; service thu hồi ngay sau đó bằng RevokeFamilyByHash.
func (r *RefreshTokenRepository) Rotate(ctx context.Context, hash string, next *domain.RefreshToken) (*domain.RefreshToken, error) {
	var old domain.RefreshToken
	now := time.Now()

	err := dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token_hash = ?", hash).
			First(&old).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrRefreshNotFound
			}
			return err
		}
		switch {
		case old.RevokedAt != nil:
			return domain.ErrRefreshRevoked
		case !now.Before(old.ExpiresAt), !now.Before(old.FamilyExpiresAt):
			return domain.ErrRefreshExpired
		case old.RotatedAt != nil:
			return domain.ErrRefreshReuse
		}

		if err := tx.Model(&domain.RefreshToken{}).
			Where("id = ?", old.ID).
			Update("rotated_at", now).Error; err != nil {
			return err
		}

		next.UserID = old.UserID
		next.FamilyID = old.FamilyID
		next.FamilyExpiresAt = old.FamilyExpiresAt
		if next.ExpiresAt.After(old.FamilyExpiresAt) {
			next.ExpiresAt = old.FamilyExpiresAt
		}
		return tx.Create(next).Error
	})
	if err != nil {
		// old vẫn có dữ liệu nếu đã tìm thấy — service dùng để log family.
		return &old, err
	}
	return &old, nil
}

func (r *RefreshTokenRepository) RevokeFamilyByHash(ctx context.Context, hash, reason string) (int64, error) {
	var token domain.RefreshToken
	err := dbConn(ctx, r.db).Select("family_id").Where("token_hash = ?", hash).First(&token).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil // idempotent, không lộ token có tồn tại hay không
		}
		return 0, err
	}
	return r.revoke(ctx, "family_id = ?", []any{token.FamilyID}, reason)
}

func (r *RefreshTokenRepository) RevokeAllByUser(ctx context.Context, userID int64, reason string) (int64, error) {
	return r.revoke(ctx, "user_id = ?", []any{userID}, reason)
}

func (r *RefreshTokenRepository) revoke(ctx context.Context, where string, args []any, reason string) (int64, error) {
	res := dbConn(ctx, r.db).Model(&domain.RefreshToken{}).
		Where(where+" AND revoked_at IS NULL", args...).
		Updates(map[string]any{
			"revoked_at":    time.Now(),
			"revoke_reason": reason,
		})
	return res.RowsAffected, res.Error
}

// DeleteExpired xoá CỨNG (Unscoped) thay vì soft delete: token chết không có
// giá trị khôi phục, giữ lại chỉ làm bảng phình và index loãng.
func (r *RefreshTokenRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	res := dbConn(ctx, r.db).Unscoped().
		Where("expires_at < ? OR (revoked_at IS NOT NULL AND revoked_at < ?)", before, before).
		Delete(&domain.RefreshToken{})
	return res.RowsAffected, res.Error
}
