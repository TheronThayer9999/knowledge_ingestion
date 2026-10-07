package domain

import (
	"context"
	"time"
)

type OutboxEvent struct {
	BaseModel

	AggregateType string    `json:"aggregate_type" gorm:"not null"`
	AggregateID   int64     `json:"aggregate_id" gorm:"not null"`
	EventType     string    `json:"event_type" gorm:"not null"`
	Payload       string    `json:"payload" gorm:"type:jsonb;not null"`
	Status        string    `json:"status" gorm:"not null;index"`
	RetryCount    int       `json:"retry_count" gorm:"not null;default:0"`
	NextRetryAt   time.Time `json:"next_retry_at" gorm:"not null;index"`
}

func (OutboxEvent) TableName() string { return "outbox_events" }

const (
	OutboxPending    = "pending"
	OutboxProcessing = "processing"
	OutboxDone       = "done"
	OutboxDead       = "dead"
)

// Event type cho relay xóa blob — worker switch theo type này để route.
const OutboxEventArticleBlobDelete = "article.blob_delete_requested"

type IOutboxRepository interface {
	// Create ghi event — gọi trong InTx thì chung tx với các op khác,
	// gọi ngoài thì chạy autocommit thường.
	Create(ctx context.Context, e *OutboxEvent) error
	// ClaimPending hốt batch event tới hạn cho worker (FOR UPDATE SKIP
	// LOCKED) — nhiều worker không giẫm nhau.
	ClaimPending(ctx context.Context, limit int) ([]*OutboxEvent, error)
	MarkDone(ctx context.Context, id int64) error
	ScheduleRetry(ctx context.Context, id int64, retryCount int, nextRetry time.Time) error
	MarkDead(ctx context.Context, id int64) error
}
