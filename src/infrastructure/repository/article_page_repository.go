package repository

import (
	"context"

	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ArticlePageRepository struct {
	db *gorm.DB
}

func NewArticlePageRepository(db *gorm.DB) domain.IArticlePageRepository {
	return &ArticlePageRepository{db: db}
}

// pageInsertBatch số row mỗi câu INSERT — 1 mảnh text tối đa 256KB nên chia
// nhỏ hơn chunk: 20 mảnh × 1 cột text vẫn xa trần param Postgres.
const pageInsertBatch = 20

// CreateBatch lưu 1 lô mảnh trong transaction, ON CONFLICT DO NOTHING theo
// unique (article_id, seq). Cùng lý do với chunk: job chết giữa chừng,
// lease hết hạn rồi worker khác claim lại và ghi lại đúng các mảnh đã lưu —
// phải là no-op chứ không phải insert trùng.
func (r *ArticlePageRepository) CreateBatch(ctx context.Context, pages []*domain.ArticlePage) error {
	if len(pages) == 0 {
		return nil
	}
	return dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		for _, batch := range utils.Batch(pages, pageInsertBatch) {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&batch).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Progress đọc seq lớn nhất đã lưu trong 1 câu. COALESCE vì MAX của tập rỗng
// là NULL, mà NULL scan vào int sẽ lỗi — đổi thành -1.
func (r *ArticlePageRepository) Progress(ctx context.Context, articleID int64) (*domain.PageProgress, error) {
	var doneSeq int
	err := dbConn(ctx, r.db).Model(&domain.ArticlePage{}).
		Select("COALESCE(MAX(seq), -1)").
		Where("article_id = ?", articleID).
		Scan(&doneSeq).Error
	if err != nil {
		return nil, err
	}
	return &domain.PageProgress{DoneSeq: doneSeq}, nil
}

// ListByArticle trả mọi mảnh theo thứ tự đọc (seq) — phase extract ghép với
// phần bóc mới nên thứ tự phải đúng.
func (r *ArticlePageRepository) ListByArticle(ctx context.Context, articleID int64) ([]*domain.ArticlePage, error) {
	pages := make([]*domain.ArticlePage, 0)
	err := dbConn(ctx, r.db).Where("article_id = ?", articleID).
		Order("seq ASC").
		Find(&pages).Error
	if err != nil {
		return nil, err
	}
	return pages, nil
}

// ListPagesRange trả tối đa limit mảnh có seq >= fromSeq — cửa sổ trượt của
// phase index. Câu này chạy vài chục lần cho mỗi bài, nên nó phải RẺ và
// không kéo theo chỗ nào khác: SELECT 3 cột, đúng thứ tự, đúng index
// (article_id, seq).
func (r *ArticlePageRepository) ListPagesRange(ctx context.Context, articleID int64, fromSeq, limit int) ([]*domain.ArticlePage, error) {
	if limit <= 0 {
		limit = 1
	}
	pages := make([]*domain.ArticlePage, 0, limit)
	err := dbConn(ctx, r.db).Where("article_id = ? AND seq >= ?", articleID, fromSeq).
		Order("seq ASC").
		Limit(limit).
		Find(&pages).Error
	if err != nil {
		return nil, err
	}
	return pages, nil
}

// DeleteByArticleID dọn trang (Unscoped) — purge và các nhánh bài bị xoá giữa
// lúc đang trích. Sau khi trích xong thì giữ lại làm nguồn cho phase index.
func (r *ArticlePageRepository) DeleteByArticleID(ctx context.Context, articleID int64) error {
	return dbConn(ctx, r.db).Unscoped().
		Where("article_id = ?", articleID).
		Delete(&domain.ArticlePage{}).Error
}

// DeleteByArticles như DeleteByArticleID nhưng nhiều bài 1 câu — purge gom id
// trước khi xoá hẳn article row.
func (r *ArticlePageRepository) DeleteByArticles(ctx context.Context, articleIDs []int64) error {
	if len(articleIDs) == 0 {
		return nil
	}
	return dbConn(ctx, r.db).Unscoped().
		Where("article_id IN ?", articleIDs).
		Delete(&domain.ArticlePage{}).Error
}

// DeleteOrphan xoá trang của bài không còn tồn tại — dùng NOT EXISTS để 1 câu
// xong thay vì kéo id về rồi lọc. Unscoped ở cả 2 bảng: bài xoá mềm VẪN còn
// row nên trang của nó là hợp lệ, chỉ bài đã xoá hẳn mới là mồ côi.
func (r *ArticlePageRepository) DeleteOrphan(ctx context.Context) (int64, error) {
	res := dbConn(ctx, r.db).Unscoped().
		Where("article_id NOT IN (?)",
			r.db.Session(&gorm.Session{NewDB: true}).Model(&domain.Article{}).Select("id")).
		Delete(&domain.ArticlePage{})
	return res.RowsAffected, res.Error
}
