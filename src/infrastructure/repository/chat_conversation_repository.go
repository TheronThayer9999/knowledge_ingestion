package repository

import (
	"context"
	stderrors "errors"
	"time"

	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
)

type ConversationRepository struct {
	db *gorm.DB
}

func NewConversationRepository(db *gorm.DB) domain.IConversationRepository {
	return &ConversationRepository{db: db}
}

func (r *ConversationRepository) Create(ctx context.Context, conv *domain.Conversation) error {
	return dbConn(ctx, r.db).Create(conv).Error
}

// GetByID luôn gắn user_id vào WHERE — dịch ErrRecordNotFound thành
// domain.ErrNotFound để service trả 404 như mọi tài nguyên owner-scope khác.
func (r *ConversationRepository) GetByID(ctx context.Context, id int64, userID int64) (*domain.Conversation, error) {
	var conv domain.Conversation
	if err := dbConn(ctx, r.db).First(&conv, "id = ? AND user_id = ?", id, userID).Error; err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &conv, nil
}

// List newest-first theo (last_message_at, id). beforeID != 0 là keyset sau
// phiên con trỏ, KHÔNG phải OFFSET: sidebar mở thêm trang trong khi lượt chat
// mới vừa ghi xuống thì offset bỏ sót/lặp dòng, còn keyset trên tổng thứ tự
// (last_message_at, id) thì không.
//
// So sánh row-value `(a, b) < (SELECT ...)` là Postgres thuần; subquery đọc
// thẳng bảng nên không dính soft-delete scope của gorm — con trỏ trỏ vào phiên
// đã xoá thì vẫn ra mốc hợp lệ, chỉ là không còn ai bấm vào đó.
func (r *ConversationRepository) List(ctx context.Context, userID int64, beforeID int64, limit int) ([]*domain.Conversation, error) {
	if limit <= 0 {
		return nil, nil
	}
	q := dbConn(ctx, r.db).Where("user_id = ?", userID).
		Order("last_message_at DESC, id DESC").Limit(limit)
	if beforeID > 0 {
		q = q.Where("(last_message_at, id) < (SELECT last_message_at, id FROM conversations WHERE id = ?)", beforeID)
	}
	var out []*domain.Conversation
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *ConversationRepository) UpdateTitle(ctx context.Context, id int64, userID int64, title string) error {
	return dbConn(ctx, r.db).Model(&domain.Conversation{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{"title": title, "updated_at": time.Now()}).Error
}

// SetTitleIfEmpty chỉ ghi khi title đang rỗng — lượt thứ hai của một phiên đã
// có tên không được phép đổi tên nó (đó là việc của PATCH /rename).
func (r *ConversationRepository) SetTitleIfEmpty(ctx context.Context, id int64, title string) error {
	return dbConn(ctx, r.db).Model(&domain.Conversation{}).
		Where("id = ? AND title = ''", id).
		Updates(map[string]any{"title": title, "updated_at": time.Now()}).Error
}

// TouchMessageTime dời last_message_at để phiên vừa có lượt mới nhảy lên đầu
// sidebar. Không đụng title (đường này chạy trong cùng tx với SetTitleIfEmpty).
func (r *ConversationRepository) TouchMessageTime(ctx context.Context, id int64, at time.Time) error {
	return dbConn(ctx, r.db).Model(&domain.Conversation{}).
		Where("id = ?", id).
		Updates(map[string]any{"last_message_at": at, "updated_at": time.Now()}).Error
}

// TouchScope ghi lại phạm vi phiên đang tra (S4) — chỉ là hint, quyền thật vẫn
// do tool intersect theo ctx lúc thực thi.
func (r *ConversationRepository) TouchScope(ctx context.Context, id int64, scopeCategoryIDs string) error {
	return dbConn(ctx, r.db).Model(&domain.Conversation{}).
		Where("id = ?", id).
		Updates(map[string]any{"scope_category_ids": scopeCategoryIDs, "updated_at": time.Now()}).Error
}

func (r *ConversationRepository) Delete(ctx context.Context, id int64, userID int64) (int64, error) {
	res := dbConn(ctx, r.db).Where("id = ? AND user_id = ?", id, userID).Delete(&domain.Conversation{})
	return res.RowsAffected, res.Error
}
