package domain

import (
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
// Bảng + repo outbox hiện chưa có consumer (purge xóa blob trực tiếp) nên
// chỉ giữ model phục vụ AutoMigrate/định danh event, chưa wire repository.
const OutboxEventArticleBlobDelete = "article.blob_delete_requested"
