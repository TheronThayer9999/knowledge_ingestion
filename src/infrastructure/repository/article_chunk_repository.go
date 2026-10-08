package repository

import (
	"context"
	"knowledge_ingestion/src/domain"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// staleTrimBatch trần row dọn mỗi kỳ — janitor làm theo đợt bounded như
// purgeBatchSize để 1 kỳ không ôm transaction quá to.
const staleTrimBatch = 100

type ArticleChunkRepository struct {
	db *gorm.DB
}

func NewArticleChunkRepository(db *gorm.DB) domain.IArticleChunkRepository {
	return &ArticleChunkRepository{db: db}
}

const (
	// chunkInsertBatch số row mỗi câu INSERT — 500 row × ~8 cột ≈ 4000 param,
	// còn xa trần 65535 của Postgres. File 500 trang (~1500 chunk) chia 3 câu,
	// 1 câu lỗi cũng không bay cả bài.
	chunkInsertBatch = 500
)

// CreateBatch lưu toàn bộ chunk của 1 article trong ĐÚNG 1 transaction, chia
// nhiều câu INSERT (500 row/câu, xa trần param Postgres) — câu nào lỗi thì
// rollback toàn bộ, kỳ sau làm lại từ đầu thay vì kẹt nửa vời. ON CONFLICT DO
// NOTHING nhờ unique (article_id, chunk_index): 2 worker claim trùng (lease
// race) cùng insert thì worker sau bỏ qua, không duplicate — worker nào cũng
// đánh done được vì full bộ chunk đã có.
func (r *ArticleChunkRepository) CreateBatch(ctx context.Context, chunks []*domain.ArticleChunk) error {
	return dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		for i := 0; i < len(chunks); i += chunkInsertBatch {
			end := i + chunkInsertBatch
			if end > len(chunks) {
				end = len(chunks)
			}
			batch := chunks[i:end]
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&batch).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ExistsByArticleID đếm row theo article — gorm tự loại soft-deleted nên
// bài chunk xong rồi xóa mềm vẫn tính là "đã xong", worker không chunk lại.
func (r *ArticleChunkRepository) ExistsByArticleID(ctx context.Context, articleID int64) (bool, error) {
	var count int64
	if err := dbConn(ctx, r.db).Model(&domain.ArticleChunk{}).
		Where("article_id = ?", articleID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteByArticleID xóa HẲN toàn bộ chunk của 1 bài (Unscoped) — worker
// re-chunk, embed chốt done, hoặc purge dọn bài gọi. Chunk không có giá trị
// audit sau khi vector đã lên Qdrant nên không giữ rác mềm; xóa bài không có
// chunk cũng không báo lỗi.
func (r *ArticleChunkRepository) DeleteByArticleID(ctx context.Context, articleID int64) error {
	return dbConn(ctx, r.db).Unscoped().Where("article_id = ?", articleID).Delete(&domain.ArticleChunk{}).Error
}

// unembeddedRow hứng kết quả join chunk + owner — gorm embedded map toàn bộ
// cột article_chunks, 2 cột articles.* map tay.
type unembeddedRow struct {
	domain.ArticleChunk `gorm:"embedded"`
	UserID              int64 `gorm:"column:user_id"`
	CategoryID          int64 `gorm:"column:category_id"`
}

// ListUnembeddedByArticle trả toàn bộ chunk chưa lên Qdrant của 1 bài kèm
// owner — join articles có điều kiện deleted_at viết tay vì filter soft-delete
// tự động của gorm chỉ áp cho model chính (article_chunks).
func (r *ArticleChunkRepository) ListUnembeddedByArticle(ctx context.Context, articleID int64) ([]*domain.ChunkWithOwner, error) {
	var rows []unembeddedRow
	err := dbConn(ctx, r.db).Model(&domain.ArticleChunk{}).
		Select("article_chunks.*, articles.user_id, articles.category_id").
		Joins("JOIN articles ON articles.id = article_chunks.article_id AND articles.deleted_at IS NULL").
		Where("article_chunks.article_id = ? AND article_chunks.embedded_at IS NULL", articleID).
		Order("article_chunks.chunk_index ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*domain.ChunkWithOwner, 0, len(rows))
	for i := range rows {
		out = append(out, &domain.ChunkWithOwner{
			Chunk:      &rows[i].ArticleChunk,
			UserID:     rows[i].UserID,
			CategoryID: rows[i].CategoryID,
		})
	}
	return out, nil
}

// DeleteStaleChunks dọn chunk già trong 1 transaction: row đã embedded quá
// hạn + row mồ côi (không còn bài cha, kể cả bài đã xóa mềm) quá hạn, mỗi
// loại tối đa staleTrimBatch row/kỳ. Trả số row đã xóa. Row đang chờ
// (embedded_at NULL còn bài) không bao giờ bị đụng.
func (r *ArticleChunkRepository) DeleteStaleChunks(ctx context.Context, before time.Time) (int64, error) {
	var total int64
	err := dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		// 2 bước select-id rồi delete theo id (thay vì DELETE LIMIT trực
		// tiếp) để chạy chắc trên mọi Postgres — GORM không phải bản nào
		// cũng dịch Limit trong Delete đúng.
		var ids []int64
		// Unscoped ở cả 2 SELECT để nhìn thấy row đã xóa mềm (đường done và
		// purge soft-delete trước khi hard-delete) — không thì rác mềm tích
		// mãi vì scope mặc định loại chúng ra.
		if err := tx.Unscoped().Model(&domain.ArticleChunk{}).
			Where("embedded_at IS NOT NULL AND embedded_at < ?", before).
			Order("id ASC").Limit(staleTrimBatch).Pluck("id", &ids).Error; err != nil {
			return err
		}
		var orphans []int64
		if err := tx.Unscoped().Model(&domain.ArticleChunk{}).
			Where("embedded_at IS NULL AND created_at < ? AND article_id NOT IN (SELECT id FROM articles)", before).
			Order("id ASC").Limit(staleTrimBatch).Pluck("id", &orphans).Error; err != nil {
			return err
		}
		ids = append(ids, orphans...)
		if len(ids) == 0 {
			return nil
		}
		res := tx.Unscoped().Where("id IN ?", ids).Delete(&domain.ArticleChunk{})
		if res.Error != nil {
			return res.Error
		}
		total = res.RowsAffected
		return nil
	})
	return total, err
}

// MarkEmbedded đánh dấu chunk đã lên Qdrant — chỉ gọi sau Upsert thành công.
func (r *ArticleChunkRepository) MarkEmbedded(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return dbConn(ctx, r.db).Model(&domain.ArticleChunk{}).
		Where("id IN ?", ids).
		Update("embedded_at", time.Now()).Error
}
