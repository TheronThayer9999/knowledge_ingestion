package domain

import (
	"context"
	"time"
)

// WorkerHeartbeat là nhịp sống của 1 tiến trình worker, khoá theo worker_id
// (host:pid). Chỉ để API/UI trả lời câu "worker còn chạy không" — thiếu nó
// thì bài đang chờ trông y hệt bài đang xử lý, user ngồi chờ vô vọng.
//
// Không nhúng BaseModel: row bị xoá HẲN khi worker tắt sạch (Forget), không
// có chuyện soft delete rồi purge.
type WorkerHeartbeat struct {
	WorkerID   string    `gorm:"primaryKey;type:varchar(150)"`
	StartedAt  time.Time `gorm:"not null"`
	LastSeenAt time.Time `gorm:"not null;index"`
}

func (WorkerHeartbeat) TableName() string {
	return "worker_heartbeats"
}

// IWorkerHeartbeatRepository ghi/đọc nhịp sống worker.
type IWorkerHeartbeatRepository interface {
	// Beat upsert nhịp sống của 1 worker — last_seen_at lấy giờ DB (now())
	// để nhiều máy lệch đồng hồ vẫn so được với nhau.
	Beat(ctx context.Context, workerID string, startedAt time.Time) error
	// Forget xoá row khi worker tắt sạch — UI thấy offline ngay thay vì đợi
	// hết ngưỡng WORKER_OFFLINE_AFTER.
	Forget(ctx context.Context, workerID string) error
	// LatestSeen mốc heartbeat mới nhất; ok=false khi chưa worker nào ghi.
	LatestSeen(ctx context.Context) (seen time.Time, ok bool, err error)
}
