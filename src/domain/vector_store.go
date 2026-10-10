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

// SearchFilter khóa phân quyền query — UserID bắt buộc (owner từ token,
// không tin client); CategoryID/ArticleID > 0 thì drill-down thêm 1 nấc.
type SearchFilter struct {
	UserID     int64
	CategoryID int64
	ArticleID  int64
}

// ScoredChunk là 1 hit search — service map sang response cho API/agent.
type ScoredChunk struct {
	PointID    string
	Score      float32
	ArticleID  int64
	ChunkIndex int
	PageNum    int
	Text       string
}

// IVectorStore hợp đồng với kho vector — service chỉ phụ thuộc interface này
// nên đổi Qdrant → Weaviate/Milvus chỉ cần viết adapter mới. Triển khai ở
// infrastructure/qdrant (official go-client, gRPC cổng 6334).
type IVectorStore interface {
	// UpsertPoints ghi đè point theo ID — idempotent, crash giữa chừng chạy
	// lại vẫn hội tụ.
	UpsertPoints(ctx context.Context, points []*VectorPoint) error
	// DeleteByArticle xóa toàn bộ point của 1 bài (filter article_id) — purge
	// gọi khi dọn bài để vector không thành rác mồ côi.
	DeleteByArticle(ctx context.Context, articleID int64) error
	// CountByArticle đếm point của 1 bài — worker embed dùng để guard nhánh
	// done-rỗng (chunk hết nhưng chưa chắc vector đã lên): count 0 thì không
	// được done, phải retry thay vì done âm thầm với search trống.
	CountByArticle(ctx context.Context, articleID int64) (int64, error)
	// Search trả top chunk gần vector nhất trong phạm vi filter — agent RAG
	// gọi qua service search. ScoreThreshold > 0 thì lọc hit yếu để không
	// trả lời bừa; limit <= 0 thì adapter tự lấy mặc định.
	Search(ctx context.Context, vector []float32, filter SearchFilter, limit int, scoreThreshold float32) ([]*ScoredChunk, error)
	// SearchText tìm chunk khớp từ khóa trong payload text (full-text
	// MatchText) cùng phạm vi filter — nửa keyword của hybrid search. Trả theo
	// thứ tự Qdrant với Score = 0 (Scroll không có score; service fuse bằng
	// RRF theo rank chứ không theo score). Cần text index trên field "text"
	// (ensureCollection tạo best-effort) — collection cũ chưa có thì báo lỗi
	// để service degraded dense-only, không fail cả query.
	SearchText(ctx context.Context, query string, filter SearchFilter, limit int) ([]*ScoredChunk, error)
	// IsPermanentError báo lỗi có retry cũng vậy không (sai dim, mã gRPC
	// InvalidArgument/NotFound/Unauthenticated...) — service dùng để failed
	// luôn thay vì backoff. Triển khai ở infra.
	IsPermanentError(err error) bool
}

// Quy ước cho API search (khi xây): chỉ search bài embed_status=done, kèm
// score_threshold để không trả lời bừa khi không có đoạn nào gần — giữa lúc
// embed đang chạy Qdrant phục vụ kết quả thiếu là trạng thái trung gian bình
// thường, không phải lỗi.
