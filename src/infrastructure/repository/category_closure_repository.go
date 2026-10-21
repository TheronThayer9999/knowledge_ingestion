package repository

import (
	"context"
	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
)

type CategoryClosureRepository struct {
	db *gorm.DB
}

func NewCategoryClosureRepository(db *gorm.DB) domain.ICategoryClosureRepositoryImpl {
	return &CategoryClosureRepository{db: db}
}

// AddToTree ghi self row rồi copy toàn bộ tổ tiên của cha với depth + 1.
// Mọi method ở đây dùng dbConn (không phải c.db.WithContext) để chung
// transaction với categoryRepo khi service gọi trong unitOfWork.InTx.
func (c *CategoryClosureRepository) AddToTree(ctx context.Context, categoryID int64, parentID *int64) error {
	// Self row dựng từ domain type, không viết SQL trần — depth lấy từ
	// domain.SelfDepth để không có con số 0 nằm giữa câu lệnh.
	self := &domain.CategoryClosure{
		AncestorID:   categoryID,
		DescendantID: categoryID,
		Depth:        domain.SelfDepth,
	}
	if err := dbConn(ctx, c.db).Create(self).Error; err != nil {
		return err
	}
	if parentID == nil {
		return nil
	}
	return dbConn(ctx, c.db).Exec(
		`INSERT INTO category_closure (ancestor_id, descendant_id, depth)
		 SELECT ancestor_id, ?, depth + 1
		 FROM category_closure
		 WHERE descendant_id = ?
		 ON CONFLICT DO NOTHING`,
		categoryID, *parentID).Error
}

// Move cắt subtree của categoryID khỏi tổ tiên cũ rồi nối vào tổ tiên của
// newParentID. Cycle phải bị service chặn trước — ở đây không tự kiểm.
func (c *CategoryClosureRepository) Move(ctx context.Context, categoryID, newParentID int64) error {
	// Bước 1: xoá các dòng bắc cầu (ancestor ngoài subtree → descendant trong
	// subtree). Dòng nội bộ subtree (ancestor cũng trong subtree) được giữ.
	if err := dbConn(ctx, c.db).Exec(
		`DELETE FROM category_closure
		 WHERE descendant_id IN (SELECT descendant_id FROM category_closure WHERE ancestor_id = ?)
		   AND ancestor_id NOT IN (SELECT descendant_id FROM category_closure WHERE ancestor_id = ?)`,
		categoryID, categoryID).Error; err != nil {
		return err
	}
	// Bước 2: nhân chéo tổ tiên của cha mới với subtree, +1 cho cạnh mới.
	return dbConn(ctx, c.db).Exec(
		`INSERT INTO category_closure (ancestor_id, descendant_id, depth)
		 SELECT sup.ancestor_id, sub.descendant_id, sup.depth + sub.depth + 1
		 FROM category_closure sup
		 JOIN category_closure sub ON sub.ancestor_id = ?
		 WHERE sup.descendant_id = ?
		 ON CONFLICT DO NOTHING`,
		categoryID, newParentID).Error
}

// Remove xoá mọi dòng chạm categoryID — cả hai chiều, không để dòng mồ côi.
func (c *CategoryClosureRepository) Remove(ctx context.Context, categoryID int64) error {
	return dbConn(ctx, c.db).Exec(
		`DELETE FROM category_closure WHERE descendant_id = ? OR ancestor_id = ?`,
		categoryID, categoryID).Error
}

// CountChildren đếm con trực tiếp (depth = domain.DirectChildDepth).
func (c *CategoryClosureRepository) CountChildren(ctx context.Context, categoryID int64) (int64, error) {
	var n int64
	err := dbConn(ctx, c.db).Model(&domain.CategoryClosure{}).
		Where("ancestor_id = ? AND depth = ?", categoryID, domain.DirectChildDepth).
		Count(&n).Error
	return n, err
}

// IsAncestor báo ancestorID có phải tổ tiên của descendantID không. Hai id
// trùng nhau trả true (self row luôn tồn tại) — service phải loại trường hợp
// "tự làm cha của mình" trước khi dùng kết quả này để chặn cycle.
func (c *CategoryClosureRepository) IsAncestor(ctx context.Context, ancestorID, descendantID int64) (bool, error) {
	var n int64
	err := dbConn(ctx, c.db).Model(&domain.CategoryClosure{}).
		Where("ancestor_id = ? AND descendant_id = ?", ancestorID, descendantID).
		Count(&n).Error
	return n > 0, err
}

// GetByOwner trả mọi dòng closure của category thuộc userID.
func (c *CategoryClosureRepository) GetByOwner(ctx context.Context, userID int64) ([]*domain.CategoryClosure, error) {
	owned := dbConn(ctx, c.db).Model(&domain.Category{}).Select("id").Where("user_id = ?", userID)
	var rows = make([]*domain.CategoryClosure, 0)
	err := dbConn(ctx, c.db).Where("ancestor_id IN (?) OR descendant_id IN (?)", owned, owned).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// GetDescendants trả subtree của categoryID, gồm chính nó (depth = 0).
func (c *CategoryClosureRepository) GetDescendants(ctx context.Context, categoryID int64) ([]*domain.CategoryWithDepth, error) {
	var rows = make([]*domain.CategoryWithDepth, 0)
	err := dbConn(ctx, c.db).
		Table("categories").
		Select("categories.*, category_closure.depth").
		Joins("JOIN category_closure ON category_closure.descendant_id = categories.id").
		Where("category_closure.ancestor_id = ? AND categories.deleted_at IS NULL", categoryID).
		Order("category_closure.depth ASC, categories.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// GetAncestors trả đường đi từ categoryID lên gốc, gồm chính nó (depth = 0).
func (c *CategoryClosureRepository) GetAncestors(ctx context.Context, categoryID int64) ([]*domain.CategoryWithDepth, error) {
	var rows = make([]*domain.CategoryWithDepth, 0)
	err := dbConn(ctx, c.db).
		Table("categories").
		Select("categories.*, category_closure.depth").
		Joins("JOIN category_closure ON category_closure.ancestor_id = categories.id").
		Where("category_closure.descendant_id = ? AND categories.deleted_at IS NULL", categoryID).
		Order("category_closure.depth ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
