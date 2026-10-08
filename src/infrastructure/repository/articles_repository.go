package repository

import (
	"context"
	stderrors "errors"
	"knowledge_ingestion/src/common/logs"
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

// HardDelete xóa hẳn row — gorm Delete thường chỉ set DeletedAt nên phải
// Unscoped. Xóa row không tồn tại cũng không báo lỗi nên 2 worker cùng quét
// trúng 1 row vẫn an toàn.
func (a *ArticleRepository) HardDelete(ctx context.Context, id int64) error {
	return dbConn(ctx, a.db).Unscoped().Where("id = ?", id).Delete(&domain.Article{}).Error
}

// ListUnprocessed đã thay bằng ClaimChunkPending (queue có lease, nhiều
// worker không giẫm nhau) — giữ comment để ai tìm cũng thấy đường:
//
//	ClaimChunkPending hốt batch bài tới hạn trong đúng 1 transaction: SELECT
//	... FOR UPDATE SKIP LOCKED rồi đánh processing + hẹn lease ngay (cùng
//	pattern outbox ClaimPending).
func (a *ArticleRepository) ClaimChunkPending(ctx context.Context, limit int, lease time.Duration) ([]*domain.Article, error) {
	return a.claimPending(ctx, limit, lease, false)
}

// ClaimEmbedPending như ClaimChunkPending nhưng chỉ hốt bài đã chunk done.
func (a *ArticleRepository) ClaimEmbedPending(ctx context.Context, limit int, lease time.Duration) ([]*domain.Article, error) {
	return a.claimPending(ctx, limit, lease, true)
}

func (a *ArticleRepository) claimPending(ctx context.Context, limit int, lease time.Duration, embed bool) ([]*domain.Article, error) {
	// Guard limit để hằng số claim nào bị set 0 nhầm cũng không hốt toàn bảng
	// (gorm Limit(0) nghĩa là bỏ limit).
	if limit <= 0 {
		limit = 10
	}
	articles := make([]*domain.Article, 0, limit)
	now := time.Now()
	statusCol, retryCol := "chunk_status", "chunk_next_retry_at"
	if embed {
		statusCol, retryCol = "embed_status", "embed_next_retry_at"
	}
	err := dbConn(ctx, a.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		q := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(statusCol+" IN ? AND "+retryCol+" <= ?", []string{domain.QueuePending, domain.QueueProcessing}, now).
			Where("storage_key <> ''").
			Order("id ASC").
			Limit(limit)
		if embed {
			// Phase 2 chỉ chạy khi phase 1 xong — đúng thứ tự chunk hết file
			// rồi mới embedding.
			q = q.Where("chunk_status = ?", domain.QueueDone)
		}
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
		updates := map[string]any{"updated_at": now, retryCol: now.Add(lease)}
		if embed {
			updates["embed_status"] = domain.QueueProcessing
		} else {
			updates["chunk_status"] = domain.QueueProcessing
		}
		return tx.Model(&domain.Article{}).Where("id IN ?", ids).Updates(updates).Error
	})
	if err != nil {
		return nil, err
	}
	return articles, nil
}

// MarkChunkDone đánh dấu chunk xong 1 bài.
func (a *ArticleRepository) MarkChunkDone(ctx context.Context, id int64) error {
	return dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id = ?", id).
		Updates(map[string]any{"chunk_status": domain.QueueDone, "updated_at": time.Now()}).Error
}

// markError dùng chung cho chunk/embed: quá MaxQueueAttempts thì failed để
// người xử lý, còn lại pending + lùi giờ thử theo QueueBackoff. attempts là
// số lần lỗi tính tới hiện tại — ghi luôn vào cột để lần sau cộng tiếp (claim
// không tăng, chỉ lỗi mới tăng).
func (a *ArticleRepository) markError(ctx context.Context, id int64, attempts int, embed bool) error {
	status := domain.QueuePending
	nextRetry := domain.QueueBackoff(attempts)
	if attempts >= domain.MaxQueueAttempts {
		status = domain.QueueFailed
	}
	updates := map[string]any{"updated_at": time.Now()}
	if embed {
		updates["embed_status"] = status
		updates["embed_attempts"] = attempts
		updates["embed_next_retry_at"] = nextRetry
	} else {
		updates["chunk_status"] = status
		updates["chunk_attempts"] = attempts
		updates["chunk_next_retry_at"] = nextRetry
	}
	return dbConn(ctx, a.db).Model(&domain.Article{}).Where("id = ?", id).Updates(updates).Error
}

// MarkChunkError ghi lỗi chunk 1 bài.
func (a *ArticleRepository) MarkChunkError(ctx context.Context, id int64, attempts int) error {
	return a.markError(ctx, id, attempts, false)
}

// MarkEmbedDone đánh dấu embed xong 1 bài.
func (a *ArticleRepository) MarkEmbedDone(ctx context.Context, id int64) error {
	return dbConn(ctx, a.db).Model(&domain.Article{}).
		Where("id = ?", id).
		Updates(map[string]any{"embed_status": domain.QueueDone, "updated_at": time.Now()}).Error
}

// MarkEmbedError ghi lỗi embed 1 bài.
func (a *ArticleRepository) MarkEmbedError(ctx context.Context, id int64, attempts int) error {
	return a.markError(ctx, id, attempts, true)
}
