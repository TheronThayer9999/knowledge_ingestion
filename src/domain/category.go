package domain

import "context"

type Category struct {
	BaseModel
	// UserID là owner của danh mục — mọi query đều scope theo owner,
	// user này không bao giờ thấy/sửa/xóa category của user khác.
	UserID      int64  `json:"user_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ParentID    *int64 `json:"parent_id,omitempty"`
}

func (Category) TableName() string {
	return "categories"
}

type ICategoryRepositoryImpl interface {
	Create(ctx context.Context, topic *Category) error
	Update(ctx context.Context, topic *Category) error
	Delete(ctx context.Context, topic *Category) error
	GetById(ctx context.Context, id int64, userId int64) (*Category, error)
	GetByName(ctx context.Context, name string, userId int64) (*Category, error)
	// GetAll chỉ trả danh mục của đúng owner.
	GetAll(ctx context.Context, userId int64) ([]*Category, error)
}
