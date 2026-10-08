package domain

import "context"

// VectorPoint là 1 điểm worker embed upsert lên Qdrant — service dựng từ
// ArticleChunk (+ owner từ join articles), infra/qdrant map thành payload.
type VectorPoint struct {
	// ID là PointID đã lưu ở article_chunks — upsert cùng ID là ghi đè nên
	// embed chạy lại không bao giờ trùng point.
	ID         string
	Vector     []float32
	ArticleID  int64
	UserID     int64
	CategoryID int64
	ChunkIndex int
	PageNum    int
	Text       string
}

// IVectorStore hợp đồng với kho vector — service chỉ phụ thuộc interface này
// nên đổi Qdrant → Weaviate/Milvus chỉ cần viết adapter mới. Triển khai ở
// infrastructure/qdrant (REST cổng 6333, stdlib net/http).
type IVectorStore interface {
	// UpsertPoints ghi đè point theo ID — idempotent, crash giữa chừng chạy
	// lại vẫn hội tụ.
	UpsertPoints(ctx context.Context, points []VectorPoint) error
	// DeleteByArticle xóa toàn bộ point của 1 bài (filter article_id) — purge
	// gọi khi dọn bài để vector không thành rác mồ côi.
	DeleteByArticle(ctx context.Context, articleID int64) error
}
