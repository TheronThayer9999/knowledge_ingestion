package repository

import (
	"context"
	"errors"
	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
)

type CategoryRepository struct {
	db *gorm.DB
}

func NewCategoryRepository(db *gorm.DB) domain.ICategoryRepositoryImpl {
	return &CategoryRepository{db: db}
}

func (t *CategoryRepository) Create(ctx context.Context, topic *domain.Category) error {
	err := dbConn(ctx, t.db).Create(topic).Error
	if err != nil {
		return err
	}
	return nil
}

func (t *CategoryRepository) Update(ctx context.Context, topic *domain.Category) error {
	// Save thay vì Model(topic).Updates(topic): Updates bỏ qua zero value nên
	// không thể xoá description. Service luôn load đủ row rồi mới sửa.
	if err := dbConn(ctx, t.db).Save(topic).Error; err != nil {
		return err
	}
	return nil
}

func (t *CategoryRepository) Delete(ctx context.Context, topic *domain.Category) error {
	if err := dbConn(ctx, t.db).Delete(topic).Error; err != nil {
		return err
	}
	return nil
}

// DeleteByIDs xoá mềm cả danh sách trong 1 câu UPDATE — cascade xoá cây
// folder nên 1 lần xoá có thể hàng trăm row.
func (t *CategoryRepository) DeleteByIDs(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return dbConn(ctx, t.db).Where("id IN ?", ids).Delete(&domain.Category{}).Error
}

func (t *CategoryRepository) GetById(ctx context.Context, id int64, userId int64) (*domain.Category, error) {
	var topic domain.Category
	err := dbConn(ctx, t.db).First(&topic, "id = ? AND user_id = ?", id, userId).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &topic, nil
}

func (t *CategoryRepository) GetByName(ctx context.Context, name string, userId int64) (*domain.Category, error) {
	var topic domain.Category
	err := dbConn(ctx, t.db).First(&topic, "name = ? AND user_id = ?", name, userId).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &topic, nil
}

func (t *CategoryRepository) GetAll(ctx context.Context, userId int64) ([]*domain.Category, error) {
	var topics = make([]*domain.Category, 0)
	err := dbConn(ctx, t.db).Where("user_id = ?", userId).Order("created_at ASC").Find(&topics).Error
	if err != nil {
		return nil, err
	}
	return topics, nil
}

// Exists báo category id có tồn tại và thuộc đúng owner không — chỉ kiểm tra
// sự tồn tại, không tải row.
func (t *CategoryRepository) Exists(ctx context.Context, id int64, userId int64) (bool, error) {
	var count int64
	if err := dbConn(ctx, t.db).Model(&domain.Category{}).Where("id = ? AND user_id = ?", id, userId).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
