package domain

import "context"

type CategoryClosure struct {
	AncestorID   int64 `json:"ancestor_id"`
	DescendantID int64 `json:"descendant_id"`

	Depth int `json:"depth"`
}

func (CategoryClosure) TableName() string {
	return "category_closure"
}

// 3. Struct phụ để trả về dữ liệu có kèm độ sâu (rất hữu ích cho UI render cây)
type CategoryWithDepth struct {
	Category
	Depth int `json:"depth"`
}
type ICategoryClosureRepositoryImpl interface {
	Create(ctx context.Context, topic *CategoryClosure) error
	GetByOwner(ctx context.Context, userId int64) ([]*CategoryClosure, error)
	// GetDescendants trả mọi con cháu của ancestorID (bao gồm cả ancestorID) —
	// depth = 0 là ancestorID, depth = 1 là con, depth = 2 là cháu, ...
	GetDescendants(ctx context.Context, ancestorID int64) ([]*CategoryWithDepth, error)
	// GetAncestors trả mọi tổ tiên của descendantID (bao gồm cả descendantID) —
	// depth = 0 là descendantID, depth = 1 là cha, depth = 2 là ông, ...
	GetAncestors(ctx context.Context, descendantID int64) ([]*CategoryWithDepth, error)
	// CheckOrphaned kiểm tra xem user có category hay file nào bất thường không
	CheckOrphaned(ctx context.Context, userId int64) (bool, error)
	//check xem catetory đã tồn tại trong closure chưa
	CheckExist(ctx context.Context, ancestorID int64, descendantID int64) (bool, error)
}
