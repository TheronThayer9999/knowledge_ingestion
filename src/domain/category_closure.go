package domain

import "context"

// CategoryClosure là một dòng quan hệ tổ tiên → con cháu giữa hai folder.
// Khoá là cặp (ancestor_id, descendant_id); depth là thuộc tính của cặp nên
// KHÔNG nằm trong khoá — đưa depth vào khoá sẽ cho phép ghi trùng cặp với
// depth khác nhau và làm ON CONFLICT mất tác dụng.
type CategoryClosure struct {
	AncestorID   int64 `json:"ancestor_id" gorm:"primaryKey"`         // ID của category tổ tiên
	DescendantID int64 `json:"descendant_id" gorm:"primaryKey;index"` // ID của category con cháu
	Depth        int   `json:"depth" gorm:"not null"`                 // 0 = chính nó, 1 = con, 2 = cháu…
}

func (CategoryClosure) TableName() string {
	return "category_closure"
}

// SelfDepth là depth của dòng (X, X, …) — khoảng cách từ một node tới chính
// nó luôn bằng 0 theo định nghĩa closure table, không phụ thuộc loại cây,
// owner hay node đó có phải root không. Mọi depth khác đều do SQL suy ra
// (depth + 1 khi gắn con, sup.depth + sub.depth + 1 khi move).
const SelfDepth = 0

// DirectChildDepth là depth của con trực tiếp so với cha — đúng một cạnh,
// nên luôn bằng 1. Repo dùng hằng số này thay vì viết số trần trong câu
// "depth = 1" khi đếm con.
const DirectChildDepth = 1

// CategoryWithDepth là category kèm depth tương đối so với node gốc của
// truy vấn (0 = chính nó, 1 = con, …) — dùng để render cây.
type CategoryWithDepth struct {
	Category
	Depth int `json:"depth"`
}

// ICategoryClosureRepositoryImpl là cửa duy nhất để ghi/đọc bảng trung gian
// folder ↔ folder. Bảng này không có user_id: quyền được kiểm ở categories
// trước khi gọi, nên mọi categoryID truyền vào đây phải là ID đã được service
// xác thực owner (categoryRepo.GetById(id, userID)).
type ICategoryClosureRepositoryImpl interface {
	// AddToTree gắn category vào cây: ghi self row (categoryID, categoryID,
	// SelfDepth) rồi copy toàn bộ tổ tiên của cha với depth + 1.
	// parentID nil nghĩa là root — chỉ ghi self row.
	// categoryID phải là ID thật do DB sinh (gọi SAU categoryRepo.Create).
	AddToTree(ctx context.Context, categoryID int64, parentID *int64) error

	// Move chuyển categoryID và toàn bộ subtree của nó sang làm con của
	// newParentID; newParentID nil nghĩa là đưa về root (chỉ cắt khỏi tổ
	// tiên cũ, không nối thêm). Service phải chặn cycle, khác owner và khác
	// loại cây trước khi gọi.
	Move(ctx context.Context, categoryID int64, newParentID *int64) error

	// RemoveSubtree xoá mọi dòng closure chạm bất kỳ category nào trong
	// categoryIDs (cả hai chiều) — gọi trong cùng transaction với xoá
	// category để không để lại dòng trỏ vào row đã xoá.
	RemoveSubtree(ctx context.Context, categoryIDs []int64) error

	// IsAncestor báo ancestorID có phải tổ tiên của descendantID không
	// (hai id trùng nhau trả true). Dùng để chặn cycle khi Move.
	IsAncestor(ctx context.Context, ancestorID, descendantID int64) (bool, error)

	// GetByOwner trả mọi dòng closure của các category thuộc userID — dựng
	// cây phía client bằng 1 query, không N+1.
	GetByOwner(ctx context.Context, userID int64) ([]*CategoryClosure, error)

	// GetDescendants trả mọi con cháu của categoryID (gồm chính nó) kèm
	// depth tương đối: 0 = chính nó, 1 = con, 2 = cháu… Đây là danh sách
	// folder bị xoá khi xoá cả cây.
	GetDescendants(ctx context.Context, categoryID int64) ([]*CategoryWithDepth, error)

	// GetAncestors trả mọi tổ tiên của categoryID (gồm chính nó) kèm depth
	// tương đối: 0 = chính nó, 1 = cha, 2 = ông…
	GetAncestors(ctx context.Context, categoryID int64) ([]*CategoryWithDepth, error)
}
