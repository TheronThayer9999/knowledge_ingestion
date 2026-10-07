package domain

import "context"

type Article struct {
	BaseModel

	URL         string `json:"url" gorm:"not null"`
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

	// GetByID chỉ thấy bài của đúng owner — id чужой thì 404 như không tồn tại.
	GetByID(ctx context.Context, id int64, userID int64) (*Article, error)

	// ExistsByURL dedup URL trong phạm vi từng user.
	ExistsByURL(ctx context.Context, url string, userID int64) (bool, error)

	ListByCategoryID(ctx context.Context, categoryID int64, userID int64, limit, offset int) ([]*Article, error)

	ListByCategoryName(ctx context.Context, categoryName string, userID int64, limit, offset int) ([]*Article, error)
}
