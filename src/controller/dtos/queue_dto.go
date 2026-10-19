package dtos

import (
	"time"

	"knowledge_ingestion/src/domain"
)

// Bảng trạng thái hàng đợi chunk/embed + bài đã xóa mềm chờ janitor dọn.
// Read-only toàn hệ thống (không scope owner): người vận hành cần nhìn
// được cả bài của user khác, vì bài kẹt ở phase nào không phải chuyện
// riêng của chủ bài.
//
// Cột queue trên domain.Article đánh dấu json:"-" nên phải khai báo lại
// tường minh ở đây — đây là chỗ duy nhất được phép lộ chúng.

// ListQueueQuery kẹp độ dài mỗi nhóm bài trả về — 1 endpoint gom hết nên
// chỉ có limit, không offset (nhóm nào cũng lấy N bài mới nhất).
type ListQueueQuery struct {
	Limit int `form:"limit" binding:"omitempty,gte=0"`
}

// QueueArticleItem là 1 bài trong hàng đợi kèm trạng thái 2 phase.
// RunningFor là số phút đã claim (chỉ có nghĩa với processing) và
// LeaseExpiresAt là mốc worker phải ghi done trước — quá mốc này mà
// vẫn processing nghĩa là worker crash giữa chừng, bài sẽ được claim
// lại ở kỳ sau chứ không phải đang chạy.
type QueueArticleItem struct {
	ID            int64  `json:"id" example:"8"`
	Name          string `json:"name" example:"Nghị định 123/2024"`
	StorageKey    string `json:"storage_key" example:"uploads/550e8400-e29b-41d4-a716-446655440000.pdf"`
	ExtractStatus string `json:"extract_status" example:"done"`
	IndexStatus   string `json:"index_status" example:"processing"`
	// Attempts số lần đã lỗi (không tăng lúc claim, chỉ tăng khi lỗi) —
	// chạm utils.MaxQueueAttempts là failed, không retry nữa.
	Attempts       int       `json:"attempts" example:"2"`
	NextRetryAt    time.Time `json:"next_retry_at" example:"2026-10-19T15:00:00+07:00"`
	LeaseExpiresAt time.Time `json:"lease_expires_at" example:"2026-10-19T14:45:00+07:00"`
	RunningForMin  int       `json:"running_for_min" example:"3"`
	LeaseExpired   bool      `json:"lease_expired" example:"false"`
	CreatedAt      time.Time `json:"created_at" example:"2026-10-19T14:00:00+07:00"`
	UpdatedAt      time.Time `json:"updated_at" example:"2026-10-19T14:03:00+07:00"`
}

// QueuePhaseCounts số bài theo trạng thái của 1 phase.
type QueuePhaseCounts struct {
	Pending    int64 `json:"pending" example:"3"`
	Processing int64 `json:"processing" example:"1"`
	Done       int64 `json:"done" example:"12"`
	Failed     int64 `json:"failed" example:"2"`
}

// QueuePhaseView trạng thái 1 phase kèm các nhóm bài đáng để nhìn: đang
// xử lý (để biết worker còn sống không), fail (để biết cần xử lý gì), và
// chờ (để biết tắc ở đâu).
type QueuePhaseView struct {
	Counts     QueuePhaseCounts    `json:"counts"`
	Processing []*QueueArticleItem `json:"processing"`
	Failed     []*QueueArticleItem `json:"failed"`
	Pending    []*QueueArticleItem `json:"pending"`
}

// PurgeItem là 1 bài đã xóa mềm — trạng thái purge chỉ suy ra từ mốc
// deleted_at so với grace period, không có cột riêng trong DB. Undo được
// khi còn trong cửa sổ (CanUndo=true), quá cửa sổ thì janitor sẽ dọn
// blob → vector → chunk → row.
type PurgeItem struct {
	ID            int64     `json:"id" example:"9"`
	Name          string    `json:"name" example:"Bài cũ đã xóa"`
	StorageKey    string    `json:"storage_key" example:"uploads/550e8400-e29b-41d4-a716-446655440001.pdf"`
	DeletedAt     time.Time `json:"deleted_at" example:"2026-10-19T14:00:00+07:00"`
	WaitingMin    int       `json:"waiting_min" example:"12"`
	UndoUntil     time.Time `json:"undo_until" example:"2026-10-19T14:05:00+07:00"`
	CanUndo       bool      `json:"can_undo" example:"false"`
	ExtractStatus string    `json:"extract_status" example:"done"`
	IndexStatus   string    `json:"index_status" example:"done"`
}

// PurgeView bài đã xóa mềm chia làm 2 nhóm theo cửa sổ undo — nhóm đầu
// còn cứu được, nhóm sau sắp bị dọn hẳn.
type PurgeView struct {
	GracePeriodMin int          `json:"grace_period_min" example:"5"`
	BatchSize      int          `json:"batch_size" example:"100"`
	Undoing        []*PurgeItem `json:"undoing"`
	ReadyToPurge   []*PurgeItem `json:"ready_to_purge"`
}

// QueueStatusResponse toàn cảnh hàng đợi trong 1 lần gọi. Now là mốc
// server dùng để tính lease_expired / can_undo — client so với đồng hồ
// máy mình sẽ lệch, còn so với Now thì khớp.
type QueueStatusResponse struct {
	Now     time.Time       `json:"now" example:"2026-10-19T14:30:00+07:00"`
	Extract *QueuePhaseView `json:"extract"`
	Index   *QueuePhaseView `json:"index"`
	Purge   *PurgeView      `json:"purge"`
}

// ToQueueArticleItems map danh sách bài trong hàng đợi — lease_expired so
// sánh với now (mốc server) chứ không so với đồng hồ client. Attempts và
// RunningFor chỉ có nghĩa với processing nên để 0 cho các trạng thái khác.
// ToQueueArticleItems map danh sách bài trong hàng đợi — lease_expired so
// sánh với now (mốc server) chứ không so với đồng hồ client. Attempts và
// RunningFor chỉ có ý nghĩa với processing nên để 0 cho các trạng thái khác.
//
// index=true lấy cột lease của phase index, false lấy của phase extract —
// mỗi phase có cặp next_retry_at riêng, lấy chung phase nào sẽ ra số lease
// của phase kia.
func ToQueueArticleItems(articles []*domain.Article, now time.Time, index bool) []*QueueArticleItem {
	out := make([]*QueueArticleItem, 0, len(articles))
	for _, a := range articles {
		if a == nil {
			continue
		}
		item := &QueueArticleItem{
			ID:            a.ID,
			Name:          a.Name,
			StorageKey:    a.StorageKey,
			ExtractStatus: a.ExtractStatus,
			IndexStatus:   a.IndexStatus,
			CreatedAt:     a.CreatedAt,
			UpdatedAt:     a.UpdatedAt,
		}
		status, attempts, nextRetry := a.ExtractStatus, a.ExtractAttempts, a.ExtractNextRetryAt
		if index {
			status, attempts, nextRetry = a.IndexStatus, a.IndexAttempts, a.IndexNextRetryAt
		}
		item.NextRetryAt = nextRetry
		item.LeaseExpiresAt = nextRetry
		if status == domain.QueueProcessing {
			item.Attempts = attempts
			item.RunningForMin = int(now.Sub(a.UpdatedAt).Minutes())
			item.LeaseExpired = now.After(nextRetry)
		}
		out = append(out, item)
	}
	return out
}

// ToPurgeItems map bài đã xóa mềm — can_undo = còn trong cửa sổ grace.
// DeletedAt là gorm.DeletedAt (struct bọc sql.NullTime) chứ không phải
// *time.Time nên phải hỏi Valid chứ không so sánh nil.
func ToPurgeItems(articles []*domain.Article, now time.Time, grace time.Duration) []*PurgeItem {
	out := make([]*PurgeItem, 0, len(articles))
	for _, a := range articles {
		if a == nil || !a.DeletedAt.Valid {
			continue
		}
		deleted := a.DeletedAt.Time
		undoUntil := deleted.Add(grace)
		out = append(out, &PurgeItem{
			ID:            a.ID,
			Name:          a.Name,
			StorageKey:    a.StorageKey,
			DeletedAt:     deleted,
			WaitingMin:    int(now.Sub(deleted).Minutes()),
			UndoUntil:     undoUntil,
			CanUndo:       now.Before(undoUntil),
			ExtractStatus: a.ExtractStatus,
			IndexStatus:   a.IndexStatus,
		})
	}
	return out
}
