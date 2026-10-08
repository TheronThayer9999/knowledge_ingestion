package postgres

import (
	"fmt"

	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
)

// migrate dựng schema + backfill dữ liệu — tách khỏi NewConnection để
// constructor chỉ lo mở kết nối, tune pool và ping. Mọi câu idempotent nên
// boot lại an toàn.
func migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(domain.User{}, domain.Category{}, domain.Article{}, domain.ArticleChunk{}, domain.OutboxEvent{}); err != nil {
		return err
	}
	// Index composite partial cho luồng list article (user_id + category_id
	// + order created_at): WHERE deleted_at IS NULL vì mọi query gorm đều
	// kèm điều kiện đó nên index gọn và Sort biến mất khi bảng lớn.
	// AutoMigrate không dựng được partial index nên exec tay, IF NOT EXISTS
	// để boot lại idempotent.
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_articles_owner_list ON articles (user_id, category_id, created_at DESC) WHERE deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("failed to create articles owner index: %w", err)
	}
	// Index cho luồng chunk worker (article_id + chunk_index + order đọc
	// lại) và tra cứu "bài đã chunk chưa": WHERE deleted_at IS NULL vì mọi
	// query gorm đều kèm điều kiện đó. Tag index trong model đã dựng 1
	// index, đây là composite cover thêm thứ tự đọc — IF NOT EXISTS để boot
	// lại idempotent.
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_chunks_article_read ON article_chunks (article_id, chunk_index) WHERE deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("failed to create article chunks index: %w", err)
	}
	// Unique (article_id, chunk_index) để 2 worker claim trùng không insert
	// trùng chunk — CreateBatch ON CONFLICT DO NOTHING dựa vào index này.
	// Partial (bỏ row đã xóa mềm) để re-chunk bài từng bị xóa không vướng.
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_chunks_article_unique ON article_chunks (article_id, chunk_index) WHERE deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("failed to create article chunks unique index: %w", err)
	}
	// Index cho worker poll queue chunk/embed — WHERE deleted_at IS NULL vì
	// mọi query gorm đều kèm điều kiện đó. Embed poll thêm chunk_status done
	// để phase 2 chỉ hốt bài đã chunk xong.
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_articles_chunk_poll ON articles (chunk_status, chunk_next_retry_at, id) WHERE deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("failed to create articles chunk poll index: %w", err)
	}
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_articles_embed_poll ON articles (embed_status, embed_next_retry_at, id) WHERE deleted_at IS NULL AND chunk_status = 'done'`).Error; err != nil {
		return fmt.Errorf("failed to create articles embed poll index: %w", err)
	}
	// Backfill queue: bài đã có chunk từ trước khi có cột status (cột mới
	// default pending) thì đánh chunk done luôn để worker không chunk lại gây
	// trùng. Embed để pending cho 1 lượt catch-up idempotent (upsert đúng
	// PointID đã lưu nên không trùng point).
	if err := db.Exec(`UPDATE articles SET chunk_status = 'done' WHERE chunk_status = 'pending' AND id IN (SELECT DISTINCT article_id FROM article_chunks WHERE deleted_at IS NULL)`).Error; err != nil {
		return fmt.Errorf("failed to backfill chunk status: %w", err)
	}
	// Backfill embedded_at cho bài đã done từ trước khi có cột (lấy updated_at
	// làm mốc gần đúng) — boot lại idempotent.
	if err := db.Exec(`UPDATE articles SET embedded_at = updated_at WHERE embed_status = 'done' AND embedded_at IS NULL`).Error; err != nil {
		return fmt.Errorf("failed to backfill embedded_at: %w", err)
	}
	return nil
}
