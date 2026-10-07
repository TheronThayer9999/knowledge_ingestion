package repository

import (
	"context"
	"knowledge_ingestion/src/domain"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OutboxRepository struct {
	db *gorm.DB
}

func NewOutboxRepository(db *gorm.DB) domain.IOutboxRepository {
	return &OutboxRepository{db: db}
}

func (o *OutboxRepository) Create(ctx context.Context, e *domain.OutboxEvent) error {
	return dbConn(ctx, o.db).Create(e).Error
}

// ClaimPending hốt batch event tới hạn cho worker trong đúng 1 transaction:
// SELECT ... FOR UPDATE SKIP LOCKED rồi đánh dấu processing + hẹn timeout
// ngay — nhiều worker cùng poll không giẫm nhau; worker nào crash giữa
// chừng thì row processing quá hạn tự đủ điều kiện cho lần hốt sau.
func (o *OutboxRepository) ClaimPending(ctx context.Context, limit int) ([]*domain.OutboxEvent, error) {
	events := make([]*domain.OutboxEvent, 0, limit)
	err := dbConn(ctx, o.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status IN ? AND next_retry_at <= ?", []string{domain.OutboxPending, domain.OutboxProcessing}, time.Now()).
			Order("id ASC").
			Limit(limit).
			Find(&events).Error; err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(events))
		for _, e := range events {
			ids = append(ids, e.ID)
		}
		return tx.Model(&domain.OutboxEvent{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"status":        domain.OutboxProcessing,
				"updated_at":    time.Now(),
				"next_retry_at": time.Now().Add(5 * time.Minute),
			}).Error
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (o *OutboxRepository) MarkDone(ctx context.Context, id int64) error {
	return dbConn(ctx, o.db).Model(&domain.OutboxEvent{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": domain.OutboxDone, "updated_at": time.Now()}).Error
}

// ScheduleRetry hẹn lại event sau lỗi — status giữ pending, worker hốt lại
// khi tới next_retry_at.
func (o *OutboxRepository) ScheduleRetry(ctx context.Context, id int64, retryCount int, nextRetry time.Time) error {
	return dbConn(ctx, o.db).Model(&domain.OutboxEvent{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        domain.OutboxPending,
			"retry_count":   retryCount,
			"next_retry_at": nextRetry,
			"updated_at":    time.Now(),
		}).Error
}

func (o *OutboxRepository) MarkDead(ctx context.Context, id int64) error {
	return dbConn(ctx, o.db).Model(&domain.OutboxEvent{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": domain.OutboxDead, "updated_at": time.Now()}).Error
}
