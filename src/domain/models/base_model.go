package models

import (
	"time"

	"gorm.io/gorm"
)

type BaseModel struct {
	ID        int64          `json:"id"`
	DeletedAt gorm.DeletedAt `json:"-"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}
