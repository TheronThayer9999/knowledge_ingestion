package repository

import (
	"context"
	stderrors "errors"

	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type LLMProviderSettingRepository struct {
	db *gorm.DB
}

func NewLLMProviderSettingRepository(db *gorm.DB) domain.ILLMProviderSettingRepository {
	return &LLMProviderSettingRepository{db: db}
}

func (r *LLMProviderSettingRepository) List(ctx context.Context) ([]*domain.LLMProviderSetting, error) {
	var out []*domain.LLMProviderSetting
	if err := dbConn(ctx, r.db).Order("slug ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *LLMProviderSettingRepository) Get(ctx context.Context, slug string) (*domain.LLMProviderSetting, error) {
	var setting domain.LLMProviderSetting
	if err := dbConn(ctx, r.db).Where("slug = ?", slug).First(&setting).Error; err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &setting, nil
}

// Save là upsert theo slug: admin bấm lưu key rồi đổi model là hai lệnh trên
// cùng dòng, và dòng có thể chưa tồn tại. ON CONFLICT (slug) chạy trên
// uniqueIndex của model nên không cần SELECT trước (2 request song song không
// đẻ ra 2 dòng). created_at giữ nguyên, chỉ updated_at nhích.
//
// Gọi ở đây luôn nhận nguyên bản setting đã đọc từ DB nên các cột không đụng
// tới (ví dụ label khi chỉ lưu key) được ghi lại đúng giá trị cũ — không có
// cột nào bị xoá ngầm.
func (r *LLMProviderSettingRepository) Save(ctx context.Context, setting *domain.LLMProviderSetting) error {
	return dbConn(ctx, r.db).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "slug"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"api_key_cipher", "model", "models", "label", "type", "base_url", "disabled", "updated_at",
		}),
	}).Create(setting).Error
}

// Delete xoá HARD (Unscoped) chứ không soft delete: BaseModel có DeletedAt nên
// Delete thường để lại dòng, và uniqueIndex(slug) khi đó chặn mọi lần tạo lại
// cùng slug với lỗi rất khó hiểu. Bảng này không có lịch sử cần giữ — mất dòng
// là provider quay về đúng file config (hoặc biến mất nếu UI tạo nó).
func (r *LLMProviderSettingRepository) Delete(ctx context.Context, slug string) error {
	return dbConn(ctx, r.db).Unscoped().Where("slug = ?", slug).Delete(&domain.LLMProviderSetting{}).Error
}
