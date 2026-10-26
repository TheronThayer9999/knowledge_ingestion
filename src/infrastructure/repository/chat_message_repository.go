package repository

import (
	"context"
	"slices"

	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
)

type ChatMessageRepository struct {
	db *gorm.DB
}

func NewChatMessageRepository(db *gorm.DB) domain.IChatMessageRepository {
	return &ChatMessageRepository{db: db}
}

// Append ghi cả lượt (user + assistant) bằng 1 lệnh INSERT. Nằm trong InTx thì
// dbConn đọc tx từ ctx nên hai row + touch phiên là cùng một transaction —
// không có trạng thái "có câu hỏi mà không có câu trả lời" trong lịch sử.
func (r *ChatMessageRepository) Append(ctx context.Context, msgs []*domain.ChatMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	return dbConn(ctx, r.db).Create(&msgs).Error
}

// ListRecent cắt `limit` message MỚI NHẤT rồi đảo lại thành CŨ → MỚI.
//
// Không Order("id ASC").Limit(n) thẳng: làm vậy là lấy N message ĐẦU tiên của
// hội thoại, tức ngữ cảnh cũ nhất — trong khi history cần phần mới nhất sát câu
// hỏi hiện tại. Tầng trong dùng index (conversation_id, id) + LIMIT, tầng ngoài
// chỉ đảo mảng trong bộ nhớ (n = 12, không đáng kể).
func (r *ChatMessageRepository) ListRecent(ctx context.Context, conversationID int64, limit int) ([]*domain.ChatMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []*domain.ChatMessage
	if err := dbConn(ctx, r.db).
		Where("conversation_id = ?", conversationID).
		Order("id DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	slices.Reverse(out)
	return out, nil
}

// ListPage phân trang ngược cho UI (mở phiên cũ cuộn lên trên), newest-first
// theo id — id là mốc tách được dù 2 row của cùng một lượt ghi chung tx có
// created_at trùng khớp từng nano.
func (r *ChatMessageRepository) ListPage(ctx context.Context, conversationID int64, beforeID int64, limit int) ([]*domain.ChatMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	q := dbConn(ctx, r.db).Where("conversation_id = ?", conversationID).
		Order("id DESC").Limit(limit)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var out []*domain.ChatMessage
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// CountByConversationIDs đếm cho đúng danh sách phiên đang hiển thị — 1 câu
// GROUP BY, không N+1 và không cột đếm phản dư.
func (r *ChatMessageRepository) CountByConversationIDs(ctx context.Context, ids []int64) (map[int64]int64, error) {
	counts := make(map[int64]int64, len(ids))
	if len(ids) == 0 {
		return counts, nil
	}
	var rows []struct {
		ConversationID int64
		Total          int64
	}
	err := dbConn(ctx, r.db).Model(&domain.ChatMessage{}).
		Select("conversation_id, COUNT(*) AS total").
		Where("conversation_id IN ?", ids).
		Group("conversation_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.ConversationID] = row.Total
	}
	return counts, nil
}
