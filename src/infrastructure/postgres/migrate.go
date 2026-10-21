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
	if err := db.AutoMigrate(domain.User{}, domain.Category{}, domain.Article{}, domain.ArticlePage{},
		domain.EmbedCacheEntry{}, domain.OutboxEvent{}, domain.CategoryClosure{}); err != nil {
		return err
	}
	// Bảng article_chunks đã bị bỏ hoàn toàn (phase chunk + embed gộp làm 1,
	// Qdrant giữ text + vector trong payload). DROP nếu còn — có thể dùng
	// transaction + rollback vì đây là dữ liệu trung gian, mất là mất.
	if err := dropTableIfExists(db, "article_chunks"); err != nil {
		return err
	}
	if err := migrateQueueColumns(db); err != nil {
		return err
	}
	// Closure table thay cho categories.parent_id: PK phải là cặp
	// (ancestor_id, descendant_id), có FK về categories và parent_id bị xoá.
	if err := migrateCategoryClosure(db); err != nil {
		return err
	}
	// Worker cũ (pdf_process) ghi trạng thái 'waiting' vào cùng bảng, nhưng
	// state machine của service này chỉ có pending/processing/done/failed —
	// claim sẽ bỏ qua các dòng đó vĩnh viễn. Chuẩn hoá 1 lần, idempotent nên
	// boot lại vô hại; chỉ quét đúng các dòng còn 'waiting'.
	for _, col := range []string{"extract_status", "index_status"} {
		if err := db.Exec(fmt.Sprintf(`UPDATE articles SET %s = ? WHERE %s = ?`, col, col),
			domain.QueuePending, "waiting").Error; err != nil {
			return fmt.Errorf("chuẩn hoá %s='waiting' sang pending: %w", col, err)
		}
	}
	// Index composite partial cho luồng list article (user_id + category_id
	// + order created_at): WHERE deleted_at IS NULL vì mọi query gorm đều
	// kèm điều kiện đó nên index gọn và Sort biến mất khi bảng lớn.
	// AutoMigrate không dựng được partial index nên exec tay, IF NOT EXISTS
	// để boot lại idempotent.
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_articles_owner_list ON articles (user_id, category_id, created_at DESC) WHERE deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("failed to create articles owner index: %w", err)
	}
	// Seq: thứ tự đọc của mảnh, tách khỏi page_num (số trang thật để trích
	// dẫn). Cần vì file txt bị chia thành nhiều mảnh cùng page_num = 0 —
	// unique theo page_num buộc cả file txt phải là 1 row, tức là hoặc cắt
	// cụt mất dữ liệu, hoặc kéo cả file lên RAM 1 lần.
	if !hasColumn(db, "article_pages", "seq") {
		if err := db.Exec(`ALTER TABLE article_pages ADD COLUMN seq integer`).Error; err != nil {
			return fmt.Errorf("thêm cột article_pages.seq: %w", err)
		}
	}
	// Backfill CHẠY ĐỘC LẬP với điều kiện trên, vì AutoMigrate đã tạo cột
	// (kèm DEFAULT 0) TRƯỚC hàm này — nếu gộp vào if thì hasColumn thấy cột
	// đã có và bỏ qua luôn, mọi dòng cũ giữ seq = 0 và tạo unique index sau
	// đó sẽ fail vì trùng. Idempotent: chỉ sửa dòng seq <= 0.
	//
	// Dữ liệu cũ chỉ có trang thật (PDF) nên seq = page_num là đúng thứ tự
	// đọc. Dòng page_num = 0 (loại file không có trang) đánh số lại theo id,
	// CỘNG thêm seq lớn nhất đã có của cùng bài để không đụng dòng kia.
	if err := db.Exec(`UPDATE article_pages SET seq = page_num WHERE page_num > 0 AND seq <= 0`).Error; err != nil {
		return fmt.Errorf("backfill seq từ page_num: %w", err)
	}
	if err := db.Exec(`UPDATE article_pages p SET seq = COALESCE(m.maxseq, 0) + s.n
		FROM (
			SELECT id, article_id,
			       ROW_NUMBER() OVER (PARTITION BY article_id ORDER BY id) AS n
			FROM article_pages WHERE page_num <= 0
		) s
		LEFT JOIN (
			SELECT article_id, MAX(seq) AS maxseq
			FROM article_pages WHERE page_num > 0 GROUP BY article_id
		) m ON m.article_id = s.article_id
		WHERE p.id = s.id AND p.seq <= 0`).Error; err != nil {
		return fmt.Errorf("backfill seq cho dòng không có trang: %w", err)
	}
	// Unique (article_id, seq) — CreateBatch ON CONFLICT DO NOTHING dựa vào đây
	// để 2 worker claim trùng (lease race) không insert trùng mảnh. Partial để
	// bài từng bị xóa mềm rồi trích lại không vướng.
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_pages_article_seq ON article_pages (article_id, seq) WHERE deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("failed to create article pages seq index: %w", err)
	}
	// Index cũ theo page_num không còn là khoá nữa nhưng vẫn hữu ích khi tra
	// theo số trang. Giữ lại; tự xoá nếu muốn gọn.
	_ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_pages_article_page ON article_pages (article_id, page_num) WHERE deleted_at IS NULL`).Error

	// embed_cache: cache vector theo sha256(text) + model. PK (text_hash, model,
	// dim) vừa là khoá vừa là index lookup — Get() đánh theo đúng nó nên
	// không cần index riêng. chunk_count phục vụ dọn LRU sau này, chưa dùng
	// nên không index.
	_ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_embed_cache_model ON embed_cache (model, dim)`).Error

	// Index cho worker poll queue 2 phase. Index poll của phase index thêm
	// điều kiện extract_status done để chỉ hốt bài đã qua phase trước.
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_articles_extract_poll ON articles (extract_status, extract_next_retry_at, id) WHERE deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("failed to create articles extract poll index: %w", err)
	}
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_articles_index_poll ON articles (index_status, index_next_retry_at, id) WHERE deleted_at IS NULL AND extract_status = 'done'`).Error; err != nil {
		return fmt.Errorf("failed to create articles index poll index: %w", err)
	}
	return nil
}

// dropTableIfExists xoá bảng nếu có — dùng cho bảng đã bỏ khỏi model.
func dropTableIfExists(db *gorm.DB, table string) error {
	var n int64
	err := db.Raw(`SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?`, table).Scan(&n).Error
	if err != nil {
		return fmt.Errorf("kiểm tra bảng %s: %w", table, err)
	}
	if n == 0 {
		return nil
	}
	if err := db.Exec(`DROP TABLE IF EXISTS ` + table).Error; err != nil {
		return fmt.Errorf("xoá bảng %s: %w", table, err)
	}
	return nil
}

// hasColumn báo cột có tồn tại trong bảng không.
func hasColumn(db *gorm.DB, table, column string) bool {
	var n int64
	err := db.Raw(`SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`, table, column).Scan(&n).Error
	return err == nil && n > 0
}

// dropColumnIfExists xoá cột nếu có.
func dropColumnIfExists(db *gorm.DB, table, column string) error {
	if !hasColumn(db, table, column) {
		return nil
	}
	if err := db.Exec(fmt.Sprintf(`ALTER TABLE %s DROP COLUMN %s`, table, column)).Error; err != nil {
		return fmt.Errorf("xoá cột %s.%s: %w", table, column, err)
	}
	return nil
}

// migrateCategoryClosure dựng bảng trung gian folder ↔ folder đúng chuẩn
// closure table rồi bỏ categories.parent_id.
//
// Bảng chưa có dữ liệu thật, nên nếu shape sai (bản cũ để depth nằm trong
// PK, hoặc chưa có PK) thì DROP dựng lại — đây là điểm mất dữ liệu duy nhất
// của migration này. Idempotent: lần sau PK đúng thì không đụng vào bảng.
func migrateCategoryClosure(db *gorm.DB) error {
	var wrongShape int64
	err := db.Raw(`SELECT CASE WHEN count(*) = 2
			AND count(*) FILTER (WHERE column_name IN ('ancestor_id', 'descendant_id')) = 2
		THEN 0 ELSE 1 END
		FROM information_schema.key_column_usage
		WHERE table_schema = current_schema()
		  AND table_name = 'category_closure'
		  AND constraint_name = 'category_closure_pkey'`).Scan(&wrongShape).Error
	if err != nil {
		return fmt.Errorf("kiểm tra PK category_closure: %w", err)
	}
	if wrongShape > 0 {
		if err := db.Exec(`DROP TABLE IF EXISTS category_closure`).Error; err != nil {
			return fmt.Errorf("xoá category_closure sai shape: %w", err)
		}
		if err := db.Exec(`CREATE TABLE category_closure (
			ancestor_id   BIGINT NOT NULL,
			descendant_id BIGINT NOT NULL,
			depth         INTEGER NOT NULL,
			CONSTRAINT category_closure_pkey PRIMARY KEY (ancestor_id, descendant_id)
		)`).Error; err != nil {
			return fmt.Errorf("tạo category_closure: %w", err)
		}
	}
	// Index cho chiều tra ngược (cha của X, subtree của X) — AutoMigrate đã
	// tạo theo tag `index` nhưng IF NOT EXISTS để chạy tay vẫn an toàn.
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_category_closure_descendant_id ON category_closure (descendant_id)`).Error; err != nil {
		return fmt.Errorf("tạo index descendant_id: %w", err)
	}
	// FK về categories: purge cứng một category thì closure tự sạch, không
	// để lại dòng mồ côi. Kiểm theo tên constraint vì Postgres không có
	// ADD CONSTRAINT IF NOT EXISTS.
	for _, fk := range []struct{ name, sql string }{
		{"fk_closure_ancestor", `ALTER TABLE category_closure ADD CONSTRAINT fk_closure_ancestor FOREIGN KEY (ancestor_id) REFERENCES categories (id) ON DELETE CASCADE`},
		{"fk_closure_descendant", `ALTER TABLE category_closure ADD CONSTRAINT fk_closure_descendant FOREIGN KEY (descendant_id) REFERENCES categories (id) ON DELETE CASCADE`},
	} {
		if hasConstraint(db, "category_closure", fk.name) {
			continue
		}
		if err := db.Exec(fk.sql).Error; err != nil {
			return fmt.Errorf("thêm %s: %w", fk.name, err)
		}
	}
	// Mọi category còn sống phải có self row, nếu không GetDescendants trả
	// rỗng và Move/Remove tính sai. Dữ liệu tạo trước khi có closure chưa có
	// dòng nào nên bù một lần; ON CONFLICT để boot lại vô hại. Quan hệ cha–con
	// cũ trong parent_id không dựng lại — chưa có ai dùng, mọi category cũ
	// thành root độc lập.
	if err := db.Exec(`INSERT INTO category_closure (ancestor_id, descendant_id, depth)
		SELECT id, id, ? FROM categories WHERE deleted_at IS NULL
		ON CONFLICT DO NOTHING`, domain.SelfDepth).Error; err != nil {
		return fmt.Errorf("bù self row cho categories cũ: %w", err)
	}
	// parent_id bị bỏ hoàn toàn: quan hệ cha–con nay là depth = 1 trong
	// closure. DROP COLUMN tự bỏ theo fk_categories_parent,
	// idx_categories_parent_id và idx_categories_single_root_per_user
	// (index partial có parent_id trong predicate) — nên không còn ràng buộc
	// "mỗi user 1 root", đúng với forest nhiều root.
	return dropColumnIfExists(db, "categories", "parent_id")
}

// hasConstraint báo constraint có tồn tại trong bảng không.
func hasConstraint(db *gorm.DB, table, name string) bool {
	var n int64
	err := db.Raw(`SELECT count(*) FROM information_schema.table_constraints
		WHERE table_schema = current_schema() AND table_name = ? AND constraint_name = ?`,
		table, name).Scan(&n).Error
	return err == nil && n > 0
}

// migrateQueueColumns chuyển cột queue cũ (chunk_*, embed_*) sang cột của
// phase index mới rồi xoá cột cũ.
//
// AutoMigrate không đổi tên/xoá cột nên phải exec tay. Thứ tự BẮT BUỘC: copy
// dữ liệu -> xoá cột cũ. Mỗi bước kiểm tra cột đã có chưa để boot lại không
// lỗi (migrate chạy mỗi lần start).
//
// Hệ quả lần migrate đầu: mọi bài đang có sẽ đi qua phase extract đúng 1
// lần (extract_status mặc định pending) để lấy nội dung vào article_pages —
// đánh đổi OCR một lần lấy dữ liệu mà phase index cần. Bài đã embed xong thì
// index_status kế thừa từ embed_status nên không index lại, vector cũ vẫn
// dùng được.
func migrateQueueColumns(db *gorm.DB) error {
	if hasColumn(db, "articles", "embed_status") {
		if err := db.Exec(`UPDATE articles SET
			index_status = embed_status,
			index_attempts = embed_attempts,
			index_next_retry_at = embed_next_retry_at,
			indexed_at = COALESCE(indexed_at, embedded_at)
			WHERE embed_status <> 'pending'`).Error; err != nil {
			return fmt.Errorf("copy embed sang index: %w", err)
		}
	}
	// extracted_text bị bỏ: nội dung nay nằm ở article_pages theo từng trang
	// để phase index đọc cửa sổ trượt (xem ListPagesRange).
	for _, col := range []string{
		"chunk_status", "chunk_attempts", "chunk_next_retry_at",
		"embed_status", "embed_attempts", "embed_next_retry_at", "embedded_at",
		"extracted_text",
	} {
		if err := dropColumnIfExists(db, "articles", col); err != nil {
			return err
		}
	}
	// Index cu gắn với cột đã xoá thì Postgres tự bỏ, nhưng tên index có thể
	// còn nếu được tạo tay ở nơi khác — xoá cho sạch rồi migrate tạo lại bản
	// đúng cột.
	for _, idx := range []string{"idx_articles_chunk_poll", "idx_articles_embed_poll", "idx_pages_article_unique"} {
		if err := db.Exec(`DROP INDEX IF EXISTS ` + idx).Error; err != nil {
			return fmt.Errorf("xoá index cũ %s: %w", idx, err)
		}
	}
	return nil
}
