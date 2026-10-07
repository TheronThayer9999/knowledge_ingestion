package repository

import (
	"context"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
)

type ArticleRepository struct {
	db *gorm.DB
}

func NewArticleRepository(db *gorm.DB) domain.IArticleRepository {
	return &ArticleRepository{db: db}
}

func (a *ArticleRepository) Create(ctx context.Context, article *domain.Article) error {
	if err := a.db.WithContext(ctx).Create(article).Error; err != nil {
		return err
	}
	return nil
}

func (a *ArticleRepository) Update(ctx context.Context, article *domain.Article) error {
	if err := a.db.WithContext(ctx).Save(article).Error; err != nil {
		return err
	}
	return nil
}

func (a *ArticleRepository) Delete(ctx context.Context, article *domain.Article) error {
	if err := a.db.WithContext(ctx).Delete(article).Error; err != nil {
		return err
	}
	return nil
}

func (a *ArticleRepository) GetByID(ctx context.Context, id int64, userID int64) (*domain.Article, error) {
	var article domain.Article
	if err := a.db.WithContext(ctx).First(&article, "id = ? AND user_id = ?", id, userID).Error; err != nil {
		return nil, err
	}
	return &article, nil
}

func (a *ArticleRepository) ExistsByURL(ctx context.Context, url string, userID int64) (bool, error) {
	var count int64
	if err := a.db.WithContext(ctx).Model(&domain.Article{}).
		Where("url = ? AND user_id = ?", url, userID).
		Count(&count).Error; err != nil {
		logs.Warn("error occurred while checking if url exists: ", url)
		return false, err
	}
	return count > 0, nil
}

func (a *ArticleRepository) ExistsByStorageKey(ctx context.Context, storageKey string, userID int64) (bool, error) {
	var count int64
	if err := a.db.WithContext(ctx).Model(&domain.Article{}).
		Where("storage_key = ? AND user_id = ?", storageKey, userID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *ArticleRepository) ListByCategoryID(ctx context.Context, categoryID int64, userID int64, limit, offset int) ([]*domain.Article, error) {
	var articles = make([]*domain.Article, 0)
	err := a.db.WithContext(ctx).
		Where("category_id = ? AND user_id = ?", categoryID, userID).
		Order("articles.created_at DESC").
		Limit(limit).Offset(offset).
		Find(&articles).Error
	if err != nil {
		return nil, err
	}
	return articles, nil
}

func (a *ArticleRepository) ListByCategoryName(ctx context.Context, categoryName string, userID int64, limit, offset int) ([]*domain.Article, error) {
	var articles = make([]*domain.Article, 0)
	err := a.db.WithContext(ctx).
		Joins("JOIN categories ON categories.id = articles.category_id").
		Where("categories.name = ? AND articles.user_id = ? AND categories.user_id = ?", categoryName, userID, userID).
		Order("articles.created_at DESC").
		Limit(limit).Offset(offset).
		Find(&articles).Error
	if err != nil {
		return nil, err
	}
	return articles, nil
}
