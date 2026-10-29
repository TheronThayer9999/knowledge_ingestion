package repository

import (
	"context"
	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ArticleSectionRepository bảng article_sections (P2.4 phase 1b) — compile
// ghi, API PageIndex đọc merge summary, purge dọn theo bài.
type ArticleSectionRepository struct {
	db *gorm.DB
}

func NewArticleSectionRepository(db *gorm.DB) domain.IArticleSectionRepository {
	return &ArticleSectionRepository{db: db}
}

// Replace xóa dòng cũ của bài rồi ghi lô mới trong ĐÚNG 1 transaction —
// lỗi giữa chừng rollback sạch, không để bài ở trạng thái nửa cũ nửa mới.
// Xóa bằng Unscoped vì compile ghi lại sau khi bài đổi mục: dòng cũ còn
// DeletedAt thì uniqueIndex (article_id, name) sẽ chặn insert tên trùng.
func (r *ArticleSectionRepository) Replace(ctx context.Context, articleID int64, sections []*domain.ArticleSection) error {
	return dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("article_id = ?", articleID).Delete(&domain.ArticleSection{}).Error; err != nil {
			return err
		}
		if len(sections) == 0 {
			return nil
		}
		return tx.Create(sections).Error
	})
}

// Upsert ghi/cập nhật lô mục theo khoá (article_id, name) mà KHÔNG xoá dòng
// khác của bài — checkpoint compile: mỗi lô vừa tóm tắt xong ghi ngay, job
// chết giữa chừng thì kỳ sau chỉ làm nốt.
//
// Dòng cũ bị xoá mềm (deleted_at khác NULL) cũng được hồi sinh: uniqueIndex
// (article_id, name) không partial nên dòng đó vẫn giữ chỗ khoá, nếu không
// set deleted_at = NULL thì insert đụng khoá cũ và bị chặn.
func (r *ArticleSectionRepository) Upsert(ctx context.Context, sections []*domain.ArticleSection) error {
	if len(sections) == 0 {
		return nil
	}
	return dbConn(ctx, r.db).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "article_id"}, {Name: "name"}},
		DoUpdates: clause.Assignments(map[string]any{
			"start_chunk": gorm.Expr("excluded.start_chunk"),
			"summary":     gorm.Expr("excluded.summary"),
			"updated_at":  gorm.Expr("excluded.updated_at"),
			"deleted_at":  nil,
		}),
	}).Create(sections).Error
}

// ListByArticle trả mục của bài theo start_chunk tăng dần — thứ tự đọc
// khớp PageIndex bên Qdrant nên caller map theo tên là merge xong.
func (r *ArticleSectionRepository) ListByArticle(ctx context.Context, articleID int64) ([]*domain.ArticleSection, error) {
	var sections []*domain.ArticleSection
	err := dbConn(ctx, r.db).
		Where("article_id = ?", articleID).
		Order("start_chunk ASC").
		Find(&sections).Error
	if err != nil {
		return nil, err
	}
	return sections, nil
}

// DeleteByArticles xóa dòng của nhiều bài 1 câu — purge gọi cùng nhịp
// HardDeleteMany; id rỗng thì no-op để caller không cần guard.
func (r *ArticleSectionRepository) DeleteByArticles(ctx context.Context, articleIDs []int64) error {
	if len(articleIDs) == 0 {
		return nil
	}
	return dbConn(ctx, r.db).Unscoped().
		Where("article_id IN ?", articleIDs).
		Delete(&domain.ArticleSection{}).Error
}
