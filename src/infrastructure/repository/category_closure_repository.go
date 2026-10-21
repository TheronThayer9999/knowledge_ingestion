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

func (c CategoryClosureRepository) Create(ctx context.Context, topic *domain.CategoryClosure) error {
	err := c.db.WithContext(ctx).Create(topic).Error
	if err != nil {
		return err
	}
	return nil
}

func (c CategoryClosureRepository) GetByOwner(ctx context.Context, userId int64) ([]*domain.CategoryClosure, error) {
	var categoryClosures = make([]*domain.CategoryClosure, 0)
	err := c.db.WithContext(ctx).Where("ancestor_id IN (?) OR descendant_id IN (?)",
		c.db.Model(&domain.Category{}).Select("id").Where("user_id = ?", userId),
		c.db.Model(&domain.Category{}).Select("id").Where("user_id = ?", userId),
	).Find(&categoryClosures).Error
	if err != nil {
		return nil, err
	}
	return categoryClosures, nil
}

func (c CategoryClosureRepository) GetDescendants(ctx context.Context, ancestorID int64) ([]*domain.CategoryWithDepth, error) {
	//TODO implement me
	panic("implement me")
}

func (c CategoryClosureRepository) GetAncestors(ctx context.Context, descendantID int64) ([]*domain.CategoryWithDepth, error) {
	//TODO implement me
	panic("implement me")
}

func (c CategoryClosureRepository) CheckOrphaned(ctx context.Context, userId int64) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (c CategoryClosureRepository) CheckExist(ctx context.Context, ancestorID int64, descendantID int64) (bool, error) {
	//TODO implement me
	panic("implement me")
}
