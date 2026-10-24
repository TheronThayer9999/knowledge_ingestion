package repository

import (
	"context"
	stderrors "errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/domain"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// UpdateMetadata ghi đúng 3 cột metadata bài — không dùng Save, vì Save
// ghi mọi cột kể cả cặp queue extract_*/index_* mà worker extract không có
// ý định đụng tới.
func (a *ArticleRepository) UpdateMetadata(ctx context.Context, id int64, pageCount int, version, effectiveDate string) error {
	return dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"page_count":     pageCount,
			"version":        version,
			"effective_date": effectiveDate,
			"updated_at":     time.Now(),
		}).Error
}

func (a *ArticleRepository) Delete(ctx context.Context, article *domain.Article) error {
	if err := dbConn(ctx, a.db).Delete(article).Error; err != nil {
		return err
	}
	return nil
}

// DeleteByCategoryIDs xoá mềm mọi bài còn sống trong các danh mục — cascade
// khi xoá folder cha. 1 câu UPDATE cho cả cây folder; gorm tự thêm điều kiện
// deleted_at IS NULL nên bài đã xoá mềm giữ nguyên mốc purge, không bị dời
// hạn dọn blob/vector.
func (a *ArticleRepository) DeleteByCategoryIDs(ctx context.Context, categoryIDs []int64) (int64, error) {
	if len(categoryIDs) == 0 {
		return 0, nil
	}
	res := dbConn(ctx, a.db).Where("category_id IN ?", categoryIDs).Delete(&domain.Article{})
	return res.RowsAffected, res.Error
}

func (a *ArticleRepository) GetByID(ctx context.Context, id int64, userID int64) (*domain.Article, error) {
	var article domain.Article
	if err := dbConn(ctx, a.db).First(&article, "id = ? AND user_id = ?", id, userID).Error; err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &article, nil
}

// IsAlive check sống bằng Unscoped (thấy cả bài đã xóa mềm) rồi xét DeletedAt
// — không tìm thấy (đã xóa hẳn) hoặc đã xóa mềm đều là false.
func (a *ArticleRepository) IsAlive(ctx context.Context, id int64) (bool, error) {
	var article domain.Article
	if err := dbConn(ctx, a.db).Unscoped().First(&article, "id = ?", id).Error; err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return !article.DeletedAt.Valid, nil
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

// ListSoftDeletedSince bài đã xóa mềm TỪ mốc after trở đi — Unscoped như
// ListSoftDeleted, đảo chiều lọc: cái này lấy phần CÒN trong cửa sổ undo
// (xóa mới xong), ListSoftDeleted lấy phần ĐÃ quá grace. Mới nhất trước vì
// người vận hành hỏi "vừa xóa cái gì" chứ không phải "cái gì cũ nhất đang
// chờ dọn".
func (a *ArticleRepository) ListSoftDeletedSince(ctx context.Context, after time.Time, limit int) ([]*domain.Article, error) {
	articles := make([]*domain.Article, 0)
	err := dbConn(ctx, a.db).Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at >= ?", after).
		Order("deleted_at DESC").
		Limit(limit).
		Find(&articles).Error
	if err != nil {
		return nil, err
	}
	return articles, nil
}

// queueCountRow là 1 nhóm bài theo cặp trạng thái 2 phase + số lượng.
type queueCountRow struct {
	ExtractStatus string
	IndexStatus   string
	N             int64
}

// QueueStats gom về 8 ô đếm (2 phase × 4 trạng thái). Một GROUP BY 2 cột rồi
// cộng vào ô của TỪNG phase độc lập — không cần bảng chéo 16 ô, vì các phase
// luôn chạy tuần tự nên người vận hành chỉ cần "đang tắc ở đâu", không cần
// biết mọi tổ hợp trạng thái.
func (a *ArticleRepository) QueueStats(ctx context.Context) (*domain.QueueStats, error) {
	var rows []queueCountRow
	err := dbConn(ctx, a.db).Model(&domain.Article{}).
		Select("extract_status, index_status, count(*) AS n").
		Group("extract_status, index_status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	stats := &domain.QueueStats{}
	for _, r := range rows {
		addQueueCount(&stats.ExtractPending, &stats.ExtractProcessing, &stats.ExtractDone, &stats.ExtractFailed, r.ExtractStatus, r.N)
		addQueueCount(&stats.IndexPending, &stats.IndexProcessing, &stats.IndexDone, &stats.IndexFailed, r.IndexStatus, r.N)
	}
	return stats, nil
}

// addQueueCount cộng n vào ô đúng trạng thái — 4 con trỏ theo thứ tự
// pending/processing/done/failed khớp QueueStats.
func addQueueCount(pending, processing, done, failed *int64, status string, n int64) {
	switch status {
	case domain.QueuePending:
		*pending += n
	case domain.QueueProcessing:
		*processing += n
	case domain.QueueDone:
		*done += n
	case domain.QueueFailed:
		*failed += n
	}
}

// ListByExtractStatus lấy bài theo trạng thái trích text, mới nhất trước —
// endpoint vận hành liệt kê "đang xử lý file nào" và "bài nào fail" để canh.
// Order created_at DESC khớp thứ tự user thấy trong danh sách bài.
func (a *ArticleRepository) ListByExtractStatus(ctx context.Context, status string, limit, offset int) ([]*domain.Article, error) {
	articles := make([]*domain.Article, 0)
	err := dbConn(ctx, a.db).Where("extract_status = ?", status).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&articles).Error
	if err != nil {
		return nil, err
	}
	return articles, nil
}

// ListByIndexStatus như ListByExtractStatus cho phase index.
func (a *ArticleRepository) ListByIndexStatus(ctx context.Context, status string, limit, offset int) ([]*domain.Article, error) {
	articles := make([]*domain.Article, 0)
	err := dbConn(ctx, a.db).Where("index_status = ?", status).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&articles).Error
	if err != nil {
		return nil, err
	}
	return articles, nil
}

// ListStatuses chỉ SELECT 4 cột (id + 3 trạng thái) của bài còn sống thuộc
// owner. Client gọi câu này vài chục giây/lần để vẽ badge tiến trình — trả
// cả row (name, storage_key, description) là phí băng thông lẫn thời gian
// quét. Không phân trang: mỗi row chỉ vài chục byte.
func (a *ArticleRepository) ListStatuses(ctx context.Context, userID int64) ([]*domain.Article, error) {
	articles := make([]*domain.Article, 0)
	err := dbConn(ctx, a.db).
		Select("id", "extract_status", "index_status", "compile_status").
		Where("user_id = ?", userID).
		Order("id DESC").
		Find(&articles).Error
	if err != nil {
		return nil, err
	}
	return articles, nil
}

// HardDeleteMany xóa hẳn nhiều row 1 câu — janitor gom id các bài đã dọn xong
// blob + vector trong kỳ rồi xóa loạt, thay vì 1 DELETE mỗi bài (100 bài/đợt
// là 100 round trip chỉ để xóa row). Rỗng thì no-op nên caller không cần
// guard. Không lọc owner — id do chính ListSoftDeleted quyết định.
func (a *ArticleRepository) HardDeleteMany(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return dbConn(ctx, a.db).Unscoped().Where("id IN ?", ids).Delete(&domain.Article{}).Error
}

// ForeignIDs trả các id trong input KHÔNG thuộc userID — gồm bài của user
// khác, bài đã xóa hẳn, và id không tồn tại. Unscoped để bài đã xóa mềm vẫn
// được quy về đúng chủ (lọc soft-delete sẽ báo nhầm id của chính mình đã xoá
// là của người khác và chặn oang một rebuild hợp lệ).
//
// 1 câu lấy id thuộc owner rồi trừ khỏi input — trả luôn cả id sai để client
// biết là scope có vấn đề chứ không phải lỗi hệ thống. ids rỗng thì trả
// slice rỗng (không nil) cho caller so len an toàn.
func (a *ArticleRepository) ForeignIDs(ctx context.Context, ids []int64, userID int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	var owned []int64
	if err := dbConn(ctx, a.db).Unscoped().Model(&domain.Article{}).
		Where("id IN ? AND user_id = ?", ids, userID).
		Pluck("id", &owned).Error; err != nil {
		return nil, err
	}
	isMine := make(map[int64]bool, len(owned))
	for _, id := range owned {
		isMine[id] = true
	}
	foreign := make([]int64, 0, len(ids)-len(owned))
	for _, id := range ids {
		if !isMine[id] {
			foreign = append(foreign, id)
		}
	}
	return foreign, nil
}

// IDsByCategory trả id các bài còn sống trong 1 danh mục của đúng owner —
// gorm tự loại soft-deleted nên bài đã xóa mềm không lọt vào rebuild.
func (a *ArticleRepository) IDsByCategory(ctx context.Context, categoryID int64, userID int64) ([]int64, error) {
	var ids []int64
	if err := dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("category_id = ? AND user_id = ?", categoryID, userID).
		Order("id ASC").Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// ListDoneIDs lọc trong ids chỉ giữ bài đã embed done của đúng owner —
// gorm tự loại soft-deleted nên bài đã xóa không lọt vào search.
func (a *ArticleRepository) ListDoneIDs(ctx context.Context, userID int64, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var done []int64
	if err := dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id IN ? AND user_id = ? AND index_status = ?", ids, userID, domain.QueueDone).
		Pluck("id", &done).Error; err != nil {
		return nil, err
	}
	return done, nil
}

// ResetQueue đưa các bài về pending ở phase CHUNK và EMBED trong 1 câu
// UPDATE — id không thuộc owner thì khớp 0 row nên không reset nhầm bài người
// khác.
//
// KHÔNG đụng phase extract: nội dung đã trích nằm ở article_pages nên chạy
// lại không tốn OCR lần nào — đó là lý do tách phase. Muốn trích lại thì gọi
// ResetExtract.
func (a *ArticleRepository) ResetQueue(ctx context.Context, ids []int64, userID int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	now := time.Now()
	res := dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id IN ? AND user_id = ?", ids, userID).
		Updates(map[string]any{
			"index_status":          domain.QueuePending,
			"index_attempts":        0,
			"index_next_retry_at":   now,
			"index_done_chunks":     0,
			"indexed_at":            nil,
			"compile_status":        domain.QueuePending,
			"compile_attempts":      0,
			"compile_next_retry_at": now,
			"compiled_at":           nil,
			"updated_at":            now,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// ResetExtract đưa phase extract về pending để worker trích lại từ blob —
// dùng khi muốn cải thiện chất lượng trích (đổi OCR, đổi cách khử chữ rải)
// mà không đụng vector đã có.
//
// CHỈ đổi trạng thái, KHÔNG xoá article_pages ở đây: xoá phải đi cùng cập
// nhật trạng thái để không sinh cửa sổ "trang còn, extract đã pending" — kỳ
// sau sẽ nghĩ đã trích tới trang cuối và bài chỉ còn nửa nội dung. Service gọi
// IArticlePageRepository.DeleteByArticles TRƯỚC ResetExtract (xem
// admin.ResetExtract).
func (a *ArticleRepository) ResetExtract(ctx context.Context, ids []int64, userID int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	now := time.Now()
	res := dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id IN ? AND user_id = ?", ids, userID).
		Updates(map[string]any{
			"extract_status":        domain.QueuePending,
			"extract_attempts":      0,
			"extract_next_retry_at": now,
			"extracted_at":          nil,
			"updated_at":            now,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// ListUnprocessed đã thay bằng ClaimChunkPending (queue có lease, nhiều
// worker không giẫm nhau) — giữ comment để ai tìm cũng thấy đường:
//
//	ClaimChunkPending hốt batch bài tới hạn trong đúng 1 transaction: SELECT
//	... FOR UPDATE SKIP LOCKED rồi đánh processing + hẹn lease ngay (cùng
//	pattern outbox ClaimPending).
//
// ClaimExtractPending hốt batch bài tới hạn trích text, đúng 1 transaction
// (FOR UPDATE SKIP LOCKED) — cùng pattern chunk/embed, xem ClaimChunkPending.
// Chỉ lấy bài loại StorageKey (URL chưa xử lý phase này).
func (a *ArticleRepository) ClaimExtractPending(ctx context.Context, limit int, lease time.Duration) ([]*domain.Article, error) {
	// Guard limit để hằng số claim nào bị set 0 nhầm cũng không hốt toàn bảng
	// (gorm Limit(0) nghĩa là bỏ limit).
	if limit <= 0 {
		limit = 10
	}
	articles := make([]*domain.Article, 0, limit)
	now := time.Now()
	err := dbConn(ctx, a.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		q := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("extract_status IN ? AND extract_next_retry_at <= ?",
				[]string{domain.QueuePending, domain.QueueProcessing}, now).
			Where("storage_key <> ''").
			Order("id ASC").
			Limit(limit)
		if err := q.Find(&articles).Error; err != nil {
			return err
		}
		if len(articles) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(articles))
		for _, ar := range articles {
			ids = append(ids, ar.ID)
		}
		// Claim chỉ đổi status + hẹn lease, KHÔNG tăng attempts ở đây —
		// lý do xem ClaimChunkPending.
		updates := map[string]any{"updated_at": now, "extract_next_retry_at": now.Add(lease), "extract_status": domain.QueueProcessing}
		return tx.Model(&domain.Article{}).Where("id IN ?", ids).Updates(updates).Error
	})
	if err != nil {
		return nil, err
	}
	return articles, nil
}

// MarkExtractDone đánh dấu trích xong 1 bài (kèm mốc extracted_at). Nội
// dung đã nằm ở article_pages từ lúc trích nên không có text nào phải gửi
// kèm — cột "done nhưng text rỗng" không thể xảy ra.
func (a *ArticleRepository) MarkExtractDone(ctx context.Context, id int64) error {
	now := time.Now()
	return dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"extract_status": domain.QueueDone,
			"extracted_at":   now,
			"updated_at":     now,
		}).Error
}

// MarkExtractError ghi lỗi trích 1 bài.
func (a *ArticleRepository) MarkExtractError(ctx context.Context, id int64, attempts int) error {
	return a.markError(ctx, id, attempts, phaseExtract)
}

// ReleaseExtractClaims trả NHIỀU bài về pending trong 1 câu UPDATE khi job bị
// hủy do shutdown — không tăng attempts (bài không có lỗi), không đụng
// article_pages (kỳ sau resume). WHERE ... processing bảo vệ bài đã kịp done.
func (a *ArticleRepository) ReleaseExtractClaims(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	return dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id IN ? AND extract_status = ?", ids, domain.QueueProcessing).
		Updates(map[string]any{
			"extract_status":        domain.QueuePending,
			"extract_next_retry_at": now,
			"updated_at":            now,
		}).Error
}

// phải do service tự nhớ.
func (a *ArticleRepository) ClaimIndexPending(ctx context.Context, limit int, lease time.Duration) ([]*domain.Article, error) {
	// Guard limit để hằng số claim nào bị set 0 nhầm cũng không hốt toàn bảng
	// (gorm Limit(0) nghĩa là bỏ limit).
	if limit <= 0 {
		limit = 10
	}
	articles := make([]*domain.Article, 0, limit)
	now := time.Now()
	err := dbConn(ctx, a.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		q := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("index_status IN ? AND index_next_retry_at <= ?",
				[]string{domain.QueuePending, domain.QueueProcessing}, now).
			Where("storage_key <> ''").
			Where("extract_status = ?", domain.QueueDone).
			Order("id ASC").
			Limit(limit)
		if err := q.Find(&articles).Error; err != nil {
			return err
		}
		if len(articles) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(articles))
		for _, ar := range articles {
			ids = append(ids, ar.ID)
		}
		// Claim chỉ đổi status + hẹn lease, KHÔNG tăng attempts ở đây —
		// attempts là số lần LỖI, tăng trong markError. Tăng ở claim thì job
		// chậm bị reclaim lease vài lần (chưa lỗi lần nào) cũng đủ failed oan.
		updates := map[string]any{"updated_at": now, "index_next_retry_at": now.Add(lease), "index_status": domain.QueueProcessing}
		return tx.Model(&domain.Article{}).Where("id IN ?", ids).Updates(updates).Error
	})
	if err != nil {
		return nil, err
	}
	return articles, nil
}

// MarkIndexProgress ghi checkpoint sau mỗi đợt upsert — chỗ TIẾN TRÌNH ĐƯỢC
// GHI XUỐNG của phase index, nên job chết giữa file lớn chỉ mất tối đa 1 đợt.
// Update cột lẻ, không Save cả struct.
func (a *ArticleRepository) MarkIndexProgress(ctx context.Context, id int64, done int) error {
	return dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id = ?", id).
		Updates(map[string]any{"index_done_chunks": done, "updated_at": time.Now()}).Error
}

// MarkIndexDone đánh dấu index xong 1 bài (kèm mốc indexed_at để audit).
func (a *ArticleRepository) MarkIndexDone(ctx context.Context, id int64) error {
	now := time.Now()
	return dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id = ?", id).
		Updates(map[string]any{"index_status": domain.QueueDone, "indexed_at": now, "updated_at": now}).Error
}

// MarkIndexError ghi lỗi index 1 bài.
func (a *ArticleRepository) MarkIndexError(ctx context.Context, id int64, attempts int) error {
	return a.markError(ctx, id, attempts, phaseIndex)
}

// ReleaseIndexClaims cùng ý nghĩa ReleaseExtractClaims cho phase index: trả
// cả lô bài processing về pending ngay khi job bị hủy do shutdown, giữ
// index_done_chunks để kỳ sau chỉ làm nốt phần còn lại.
func (a *ArticleRepository) ReleaseIndexClaims(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	return dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id IN ? AND index_status = ?", ids, domain.QueueProcessing).
		Updates(map[string]any{
			"index_status":        domain.QueuePending,
			"index_next_retry_at": now,
			"updated_at":          now,
		}).Error
}

// ClaimCompilePending hốt batch bài tới hạn compile, đúng 1 transaction
// (FOR UPDATE SKIP LOCKED) — cùng pattern extract/index, xem
// ClaimChunkPending vì sao không tăng attempts ở claim. Chỉ lấy bài đã index
// done (mục + text chunk trên Qdrant đã đủ để tóm tắt).
func (a *ArticleRepository) ClaimCompilePending(ctx context.Context, limit int, lease time.Duration) ([]*domain.Article, error) {
	// Guard limit để hằng số claim nào bị set 0 nhầm cũng không hốt toàn bảng
	// (gorm Limit(0) nghĩa là bỏ limit).
	if limit <= 0 {
		limit = 10
	}
	articles := make([]*domain.Article, 0, limit)
	now := time.Now()
	err := dbConn(ctx, a.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		q := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("compile_status IN ? AND compile_next_retry_at <= ?",
				[]string{domain.QueuePending, domain.QueueProcessing}, now).
			Where("index_status = ?", domain.QueueDone).
			Order("id ASC").
			Limit(limit)
		if err := q.Find(&articles).Error; err != nil {
			return err
		}
		if len(articles) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(articles))
		for _, ar := range articles {
			ids = append(ids, ar.ID)
		}
		updates := map[string]any{"updated_at": now, "compile_next_retry_at": now.Add(lease), "compile_status": domain.QueueProcessing}
		return tx.Model(&domain.Article{}).Where("id IN ?", ids).Updates(updates).Error
	})
	if err != nil {
		return nil, err
	}
	return articles, nil
}

// MarkCompileDone đánh dấu compile xong 1 bài (kèm mốc compiled_at để audit).
func (a *ArticleRepository) MarkCompileDone(ctx context.Context, id int64) error {
	now := time.Now()
	return dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"compile_status": domain.QueueDone,
			"compiled_at":    now,
			"updated_at":     now,
		}).Error
}

// MarkCompileError ghi lỗi compile 1 bài.
func (a *ArticleRepository) MarkCompileError(ctx context.Context, id int64, attempts int) error {
	return a.markError(ctx, id, attempts, phaseCompile)
}

// ReleaseCompileClaims cùng ý nghĩa ReleaseIndexClaims cho phase compile.
func (a *ArticleRepository) ReleaseCompileClaims(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	return dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id IN ? AND compile_status = ?", ids, domain.QueueProcessing).
		Updates(map[string]any{
			"compile_status":        domain.QueuePending,
			"compile_next_retry_at": now,
			"updated_at":            now,
		}).Error
}

// phase là cột queue nào của articles — markError dùng để ghi đúng cột thay
// vì bool mơ hồ (đọc code không biết true là phase nào).
type phase int

const (
	phaseExtract phase = iota
	phaseIndex
	phaseCompile
)

// markError dùng chung cho extract/index/compile: quá MaxQueueAttempts thì
// failed để người xử lý, còn lại pending + lùi giờ thử theo utils.QueueBackoff.
// attempts là số lần lỗi tính tới hiện tại — ghi luôn vào cột để lần sau
// cộng tiếp (claim không tăng, chỉ lỗi mới tăng).
func (a *ArticleRepository) markError(ctx context.Context, id int64, attempts int, p phase) error {
	status := domain.QueuePending
	nextRetry := utils.QueueBackoff(attempts)
	if attempts >= utils.MaxQueueAttempts {
		status = domain.QueueFailed
	}
	prefix := "extract_"
	switch p {
	case phaseIndex:
		prefix = "index_"
	case phaseCompile:
		prefix = "compile_"
	}
	return dbConn(ctx, a.db).Model(&domain.Article{}).Where("id = ?", id).
		Updates(map[string]any{
			"updated_at":             time.Now(),
			prefix + "status":        status,
			prefix + "attempts":      attempts,
			prefix + "next_retry_at": nextRetry,
		}).Error
}
