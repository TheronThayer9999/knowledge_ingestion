package domain

import "context"

type Category struct {
	BaseModel
	// UserID là owner của danh mục — mọi query đều scope theo owner,
	// user này không bao giờ thấy/sửa/xóa category của user khác.
	UserID      int64  `json:"user_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	ImageURL string `json:"image_url,omitempty"`
}

func (Category) TableName() string {
	return "categories"
}

type ICategoryRepositoryImpl interface {
	Create(ctx context.Context, topic *Category) error
	Update(ctx context.Context, topic *Category) error
	Delete(ctx context.Context, topic *Category) error
	// DeleteByIDs xoá mềm nhiều danh mục trong 1 câu UPDATE — cascade xoá
	// cả cây con, không phát N lệnh DELETE.
	DeleteByIDs(ctx context.Context, ids []int64) error
	GetById(ctx context.Context, id int64, userId int64) (*Category, error)
	GetByName(ctx context.Context, name string, userId int64) (*Category, error)
	// GetAll chỉ trả danh mục của đúng owner.
	GetAll(ctx context.Context, userId int64) ([]*Category, error)
	// Exists báo category id có tồn tại và thuộc đúng owner không — chỉ kiểm tra
	// sự tồn tại, không tải row, dùng cho validate nhanh (vd. parent khi Create).
	Exists(ctx context.Context, id int64, userId int64) (bool, error)
}
