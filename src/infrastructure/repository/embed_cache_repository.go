package repository

import (
	"context"
	"time"

	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EmbedCacheRepository struct {
	db *gorm.DB
}

func NewEmbedCacheRepository(db *gorm.DB) domain.EmbedCache {
	return &EmbedCacheRepository{db: db}
}

// embedCacheFetchBatch số hash mỗi câu SELECT. Postgres chịu được vài nghìn
// giá trị IN, nhưng câu phải khớp INDEX (text_hash, model, dim) — quá nhiều
// thì planner bỏ index mà quét full, tệ hơn chia đôi.
const embedCacheFetchBatch = 500

// embedCacheWriteBatch số entry mỗi câu INSERT — 1024 chiều × 4 byte = 4KB
// mỗi vector nên batch phải nhỏ để không vượt trần tham số Postgres.
const embedCacheWriteBatch = 20

// Get trả map[text_hash]vector cho các hash tồn tại. Chỉ số 1 câu trên
// (text_hash, model, dim), đúng index PK — không quét bảng.
func (r *EmbedCacheRepository) Get(ctx context.Context, hashes []string, model string, dim int) (map[string][]float32, error) {
	out := make(map[string][]float32, len(hashes))
	if len(hashes) == 0 {
		return out, nil
	}
	for _, part := range utils.Batch(hashes, embedCacheFetchBatch) {
		var rows []*domain.EmbedCacheEntry
		err := dbConn(ctx, r.db).
			Select("text_hash", "vector").
			Where("text_hash IN ? AND model = ? AND dim = ?", part, model, dim).
			Find(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			v, err := domain.DecodeVector(row.Vector)
			if err != nil {
				// Cache hỏng (đổi cách encode) thì coi như KHÔNG có rồi
				// embed lại — hỏng cache không được làm hỏng cả job.
				continue
			}
			out[row.TextHash] = v
		}
	}
	return out, nil
}

// PutMany ghi vector mới, ON CONFLICT chỉ tăng counter. Không dùng DO NOTHING
// trần vì muốn giữ dấu vết "cache này đang được dùng" để sau này dọn theo
// LRU; vector cũ giữ nguyên vì cùng hash + cùng model thì kết quả tất định
// (trừ khi model chạy non-deterministic — khi đó đây chính là hành vi ta
// muốn: giữ vector đầu tiên cho ổn định).
func (r *EmbedCacheRepository) PutMany(ctx context.Context, entries []*domain.EmbedCacheEntry) error {
	if len(entries) == 0 {
		return nil
	}
	return dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		for _, batch := range utils.Batch(entries, embedCacheWriteBatch) {
			err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "text_hash"}, {Name: "model"}, {Name: "dim"}},
				DoUpdates: clause.Assignments(map[string]any{"chunk_count": gorm.Expr("embed_cache.chunk_count + 1")}),
			}).Create(&batch).Error
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Stats trả số entry + tổng dung lượng vector.
func (r *EmbedCacheRepository) Stats(ctx context.Context) (int64, int64, error) {
	var row struct {
		Count int64
		Bytes int64
	}
	err := dbConn(ctx, r.db).Model(&domain.EmbedCacheEntry{}).
		Select("COUNT(*) AS count, COALESCE(SUM(length(vector)), 0) AS bytes").
		Scan(&row).Error
	return row.Count, row.Bytes, err
}

// PurgeUnused xóa các entry ít dùng (chunk_count <= 1) cũ hơn olderThan.
// Janitor cho embed_cache — giữ bảng không phình khi hit rate thấp.
func (r *EmbedCacheRepository) PurgeUnused(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	res := dbConn(ctx, r.db).
		Where("chunk_count <= ? AND created_at < ?", 1, cutoff).
		Delete(&domain.EmbedCacheEntry{})
	return res.RowsAffected, res.Error
}

var (
	_ domain.EmbedCache = (*EmbedCacheRepository)(nil)
)