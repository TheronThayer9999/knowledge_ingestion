package repository

import (
	"context"
	stderrors "errors"

	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LLMUserProviderRepository là kho provider RIÊNG của user (BYOK). Mọi
// method đều lọc theo user_id ở mệnh đề WHERE — không có đường nào đọc
// được dòng của user khác, kể cả khi biết id.
type LLMUserProviderRepository struct {
	db *gorm.DB
}

func NewLLMUserProviderRepository(db *gorm.DB) domain.ILLMUserProviderRepository {
	return &LLMUserProviderRepository{db: db}
}

func (r *LLMUserProviderRepository) List(ctx context.Context, userID int64) ([]*domain.LLMUserProvider, error) {
	var out []*domain.LLMUserProvider
	if err := dbConn(ctx, r.db).Where("user_id = ?", userID).
		Order("id ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// Get lấy đúng dòng của user đó: WHERE có cả id lẫn user_id nên đoán id
// cũng không đọc được dòng của người khác — trả ErrNotFound cho cả hai.
func (r *LLMUserProviderRepository) Get(ctx context.Context, id, userID int64) (*domain.LLMUserProvider, error) {
	var provider domain.LLMUserProvider
	err := dbConn(ctx, r.db).Where("id = ? AND user_id = ?", id, userID).
		First(&provider).Error
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &provider, nil
}

// Save là upsert theo id: tạo mới thì để trống id (DB cấp), sửa thì ghi đè
// đúng các cột đã đổi. ON CONFLICT chạy trên khoá chính nên không cần
// SELECT trước — 2 request song song không đẻ ra 2 dòng.
func (r *LLMUserProviderRepository) Save(ctx context.Context, provider *domain.LLMUserProvider) error {
	return dbConn(ctx, r.db).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"label", "type", "base_url", "api_key_cipher", "model", "models", "updated_at",
		}),
	}).Create(provider).Error
}

// Delete xoá HARD (Unscoped) — bảng này không có lịch sử cần giữ, và xoá
// mềm để lại dòng sẽ chặn việc tạo lại cùng id sau này.
func (r *LLMUserProviderRepository) Delete(ctx context.Context, id, userID int64) error {
	return dbConn(ctx, r.db).Unscoped().
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&domain.LLMUserProvider{}).Error
}

func (r *LLMUserProviderRepository) Count(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := dbConn(ctx, r.db).Model(&domain.LLMUserProvider{}).
		Where("user_id = ?", userID).Count(&n).Error
	return n, err
}
