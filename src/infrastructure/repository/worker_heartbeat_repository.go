package repository

import (
	"context"
	stderrors "errors"
	"knowledge_ingestion/src/domain"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WorkerHeartbeatRepository struct {
	db *gorm.DB
}

func NewWorkerHeartbeatRepository(db *gorm.DB) domain.IWorkerHeartbeatRepository {
	return &WorkerHeartbeatRepository{db: db}
}

// Beat upsert nhịp sống: worker khởi động lại (pid mới) thì có row mới, còn
// cùng tiến trình thì chỉ đẩy last_seen_at — started_at giữ mốc lần đầu để
// biết worker đã chạy bao lâu.
func (r *WorkerHeartbeatRepository) Beat(ctx context.Context, workerID string, startedAt time.Time) error {
	return dbConn(ctx, r.db).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "worker_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"last_seen_at": gorm.Expr("now()"),
		}),
	}).Create(&domain.WorkerHeartbeat{
		WorkerID:   workerID,
		StartedAt:  startedAt,
		LastSeenAt: time.Now(),
	}).Error
}

func (r *WorkerHeartbeatRepository) Forget(ctx context.Context, workerID string) error {
	return dbConn(ctx, r.db).Where("worker_id = ?", workerID).Delete(&domain.WorkerHeartbeat{}).Error
}

// LatestSeen đọc row mới nhất — worker chết không kịp Forget (kill -9, sập
// máy) thì row cũ vẫn còn, service tự so mốc với ngưỡng offline.
func (r *WorkerHeartbeatRepository) LatestSeen(ctx context.Context) (time.Time, bool, error) {
	var row domain.WorkerHeartbeat
	err := dbConn(ctx, r.db).Order("last_seen_at DESC").Limit(1).Take(&row).Error
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	return row.LastSeenAt, true, nil
}
