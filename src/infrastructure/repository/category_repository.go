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
	err := t.db.WithContext(ctx).Create(topic).Error
	if err != nil {
		return err
	}
	return nil
}

func (t *CategoryRepository) Update(ctx context.Context, topic *domain.Category) error {
	if err := t.db.WithContext(ctx).Model(topic).Updates(topic).Error; err != nil {
		return err
	}
	return nil
}

func (t *CategoryRepository) Delete(ctx context.Context, topic *domain.Category) error {
	if err := t.db.WithContext(ctx).Delete(topic).Error; err != nil {
		return err
	}
	return nil
}

func (t *CategoryRepository) GetById(ctx context.Context, id int64, userId int64) (*domain.Category, error) {
	var topic domain.Category
	err := t.db.WithContext(ctx).First(&topic, "id = ? AND user_id = ?", id, userId).Error
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
	err := t.db.WithContext(ctx).First(&topic, "name = ? AND user_id = ?", name, userId).Error
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
	err := t.db.WithContext(ctx).Where("user_id = ?", userId).Order("id ASC").Find(&topics).Error
	if err != nil {
		return nil, err
	}
	return topics, nil
}
