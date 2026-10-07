package domain

import (
	"context"
	"time"
)

type Article struct {
	BaseModel

	// Article có đúng 1 trong 2 nguồn — service bắt đúng 1, không tin client:
	// URL là link web bên ngoài (worker fetch sau), StorageKey là object đã
	// upload lên kho qua presign (key dạng "uploads/<uuid>.<ext>").
	URL         string `json:"url" gorm:"not null"`
	StorageKey  string `json:"storage_key" gorm:"not null;index"`
	Name        string `json:"name" gorm:"not null"`
	ContentType string `json:"content_type" gorm:"not null"`
	Description string `json:"description,omitempty"`

	UserID     int64 `json:"user_id" gorm:"not null;index"`
	CategoryID int64 `json:"category_id" gorm:"not null;index"`
}

func (Article) TableName() string {
	return "articles"
}

type IArticleRepository interface {
	Create(ctx context.Context, article *Article) error
	Update(ctx context.Context, article *Article) error
	Delete(ctx context.Context, article *Article) error

	// GetByID chỉ thấy bài của đúng owner — id của người khác thì 404 như không tồn tại.
	GetByID(ctx context.Context, id int64, userID int64) (*Article, error)

	// ExistsByURL dedup URL trong phạm vi từng user.
	ExistsByURL(ctx context.Context, url string, userID int64) (bool, error)

	// ExistsByStorageKey dedup object kho trong phạm vi từng user — 1 blob
	// chỉ map tới 1 article.
	ExistsByStorageKey(ctx context.Context, storageKey string, userID int64) (bool, error)

	ListByCategoryID(ctx context.Context, categoryID int64, userID int64, limit, offset int) ([]*Article, error)

	ListByCategoryName(ctx context.Context, categoryName string, userID int64, limit, offset int) ([]*Article, error)

	// ListSoftDeleted trả các bài đã xóa mềm trước mốc before (cũ nhất
	// trước) để worker dọn blob + xóa hẳn theo đợt.
	ListSoftDeleted(ctx context.Context, before time.Time, limit int) ([]*Article, error)

	// HardDelete xóa hẳn 1 row đã xóa mềm — chỉ worker janitor gọi.
	HardDelete(ctx context.Context, id int64) error
}
