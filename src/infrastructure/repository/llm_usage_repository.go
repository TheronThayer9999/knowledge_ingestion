package repository

import (
	"context"

	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LLMUsageRepository bảng llm_usage_daily — tổng hợp chi phí LLM theo tài
// khoản. Ghi cộng dồn theo khoá (user_id, day, site, provider, model) nên
// ghi nhiều lượt trong ngày vẫn chỉ 1 dòng.
type LLMUsageRepository struct {
	db *gorm.DB
}

func NewLLMUsageRepository(db *gorm.DB) domain.ILLMUsageRepository {
	return &LLMUsageRepository{db: db}
}

// Record cộng dồn các dòng tổng hợp — ON CONFLICT DO UPDATE cộng vào dòng
// cũ (EXCLUDED.* là giá trị của dòng sắp insert). Cùng khoá thì cộng, không
// tạo dòng mới.
//
// Dùng EXCLUDED thay vì truyền số cộng thẳng vì 1 lệnh có nhiều dòng: mỗi
// dòng có số cộng riêng, EXCLUDED làm đúng điều đó.
func (r *LLMUsageRepository) Record(ctx context.Context, recs []domain.LLMUsageRecord) error {
	rows := usageRows(recs)
	if len(rows) == 0 {
		return nil
	}
	return dbConn(ctx, r.db).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"}, {Name: "day"}, {Name: "site"},
			{Name: "provider"}, {Name: "model"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"calls":             gorm.Expr("llm_usage_daily.calls + EXCLUDED.calls"),
			"prompt_tokens":     gorm.Expr("llm_usage_daily.prompt_tokens + EXCLUDED.prompt_tokens"),
			"completion_tokens": gorm.Expr("llm_usage_daily.completion_tokens + EXCLUDED.completion_tokens"),
			"total_tokens":      gorm.Expr("llm_usage_daily.total_tokens + EXCLUDED.total_tokens"),
			"errors":            gorm.Expr("llm_usage_daily.errors + EXCLUDED.errors"),
			"updated_at":        gorm.Expr("EXCLUDED.updated_at"),
		}),
	}).Create(rows).Error
}

// List trả các dòng tổng hợp theo bộ lọc — ngày mới nhất trước.
//
// JOIN bảng users để lấy luôn tên/email tài khoản: báo cáo audit mà chỉ có
// user_id thì admin phải tra tay, không đọc được "acc nào tốn".
func (r *LLMUsageRepository) List(ctx context.Context, f domain.LLMUsageFilter) ([]*domain.LLMUsageRow, error) {
	q := dbConn(ctx, r.db).Table("llm_usage_daily").
		Select("llm_usage_daily.user_id, users.user_name, users.email, llm_usage_daily.day, " +
			"llm_usage_daily.site, llm_usage_daily.provider, llm_usage_daily.model, " +
			"llm_usage_daily.calls, llm_usage_daily.prompt_tokens, llm_usage_daily.completion_tokens, " +
			"llm_usage_daily.total_tokens, llm_usage_daily.errors").
		Joins("LEFT JOIN users ON users.id = llm_usage_daily.user_id")
	if f.From != "" {
		q = q.Where("llm_usage_daily.day >= ?", f.From)
	}
	if f.To != "" {
		q = q.Where("llm_usage_daily.day <= ?", f.To)
	}
	if f.UserID > 0 {
		q = q.Where("llm_usage_daily.user_id = ?", f.UserID)
	}
	if f.Site != "" {
		q = q.Where("llm_usage_daily.site = ?", f.Site)
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []*domain.LLMUsageRow
	err := q.Order("llm_usage_daily.day DESC, llm_usage_daily.user_id ASC").
		Limit(limit).Offset(f.Offset).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// usageRows đổi record thành row gorm — gộp trùng khoá trong cùng 1 lệnh
// (Postgres không cho ON CONFLICT DO UPDATE chạm 1 dòng 2 lần).
func usageRows(recs []domain.LLMUsageRecord) []*domain.LLMUsageDaily {
	rows := make([]*domain.LLMUsageDaily, 0, len(recs))
	seen := map[string]bool{}
	for _, rec := range recs {
		key := rec.Day + "|" + rec.Site + "|" + rec.Provider + "|" + rec.Model
		if seen[key] {
			continue
		}
		seen[key] = true
		rows = append(rows, &domain.LLMUsageDaily{
			UserID:           rec.UserID,
			Day:              rec.Day,
			Site:             rec.Site,
			Provider:         rec.Provider,
			Model:            rec.Model,
			Calls:            rec.Calls,
			PromptTokens:     rec.PromptTokens,
			CompletionTokens: rec.CompletionTokens,
			TotalTokens:      rec.TotalTokens,
			Errors:           rec.Errors,
		})
	}
	return rows
}
