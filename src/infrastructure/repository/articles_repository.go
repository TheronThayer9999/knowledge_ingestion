package repository

import (
	"context"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/domain"
	"time"

	"gorm.io/gorm"
)

type ArticleRepository struct {
	db *gorm.DB
}

func NewArticleRepository(db *gorm.DB) domain.IArticleRepository {
	return &ArticleRepository{db: db}
}

func (a *ArticleRepository) Create(ctx context.Context, article *domain.Article) error {
	if err := dbConn(ctx, a.db).Create(article).Error; err != nil {
		return err
	}
	return nil
}

func (a *ArticleRepository) Update(ctx context.Context, article *domain.Article) error {
	if err := dbConn(ctx, a.db).Save(article).Error; err != nil {
		return err
	}
	return nil
}

func (a *ArticleRepository) Delete(ctx context.Context, article *domain.Article) error {
	if err := dbConn(ctx, a.db).Delete(article).Error; err != nil {
		return err
	}
	return nil
}

func (a *ArticleRepository) GetByID(ctx context.Context, id int64, userID int64) (*domain.Article, error) {
	var article domain.Article
	if err := dbConn(ctx, a.db).First(&article, "id = ? AND user_id = ?", id, userID).Error; err != nil {
		return nil, err
	}
	return &article, nil
}

func (a *ArticleRepository) ExistsByURL(ctx context.Context, url string, userID int64) (bool, error) {
	var count int64
	if err := dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("url = ? AND user_id = ?", url, userID).
		Count(&count).Error; err != nil {
		logs.Warn("error occurred while checking if url exists: ", url)
		return false, err
	}
	return count > 0, nil
}

func (a *ArticleRepository) ExistsByStorageKey(ctx context.Context, storageKey string, userID int64) (bool, error) {
	var count int64
	if err := dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("storage_key = ? AND user_id = ?", storageKey, userID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *ArticleRepository) ListByCategoryID(ctx context.Context, categoryID int64, userID int64, limit, offset int) ([]*domain.Article, error) {
	var articles = make([]*domain.Article, 0)
	err := dbConn(ctx, a.db).
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
	err := dbConn(ctx, a.db).
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

// ListSoftDeleted quét bài đã xóa mềm trước mốc before — Unscoped để bypass
// filter soft-delete mặc định của gorm, lấy cũ nhất trước để dọn dứt điểm.
func (a *ArticleRepository) ListSoftDeleted(ctx context.Context, before time.Time, limit int) ([]*domain.Article, error) {
	var articles = make([]*domain.Article, 0)
	err := dbConn(ctx, a.db).Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", before).
		Order("deleted_at ASC").
		Limit(limit).
		Find(&articles).Error
	if err != nil {
		return nil, err
	}
	return articles, nil
}

// HardDelete xóa hẳn row — gorm Delete thường chỉ set DeletedAt nên phải
// Unscoped. Xóa row không tồn tại cũng không báo lỗi nên 2 worker cùng quét
// trúng 1 row vẫn an toàn.
func (a *ArticleRepository) HardDelete(ctx context.Context, id int64) error {
	return dbConn(ctx, a.db).Unscoped().Where("id = ?", id).Delete(&domain.Article{}).Error
}
