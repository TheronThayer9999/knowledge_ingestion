package domain

import (
	"context"
	"time"
)

// ArticleChunk là 1 mảnh text đã cắt từ 1 article để đưa đi embedding —
// phase 1 (worker chunk) chỉ cắt + lưu row, phase 2 (worker embed) đọc
// row này, embed rồi upsert Qdrant. Tách 2 phase để file nặng (pdf/docx)
// vẫn chunk xong nhanh, embedding lỗi thì chạy lại không cần đọc file.
type ArticleChunk struct {
	BaseModel

	ArticleID int64 `json:"article_id" gorm:"not null;index"`
	// ChunkIndex là thứ tự mảnh trong bài (0,1,2...) — đọc lại ORDER BY
	// index là đúng thứ tự gốc, Qdrant filter theo article cũng sort được.
	ChunkIndex int `json:"chunk_index" gorm:"not null"`
	// Content là text thô của mảnh, giữ nguyên bản extractor xuất (kể cả
	// tiền tố "Điều X" nếu có) — phase 2 prepend ngữ cảnh lúc embed, không
	// sửa ở đây để DB luôn là nguồn thật.
	Content string `json:"content" gorm:"type:text;not null"`
	// PointID là id point Qdrant phase 2 sẽ upsert — UUIDv7 sinh 1 lần duy
	// nhất lúc chunk rồi lưu DB, phase 2 chỉ đọc lại nên embed chạy lại cũng
	// ghi đè đúng point cũ, không trùng.
	PointID string `json:"point_id" gorm:"size:36;uniqueIndex"`
	// PageNum là số trang chứa chunk (PDF đếm từ 1, đúng thứ tự file) — file
	// không có khái niệm trang (txt/docx) thì 0. Search trả về ghi rõ trang
	// để trích dẫn chính xác.
	PageNum int `json:"page_num" gorm:"not null;default:0"`
	// EmbeddedAt nil nghĩa là chunk chưa lên Qdrant — worker embed phase 2
	// quét theo cột này. Upsert Qdrant xong mới set để crash giữa chừng kỳ
	// sau làm lại đúng chunk đó (point ghi đè idempotent).
	EmbeddedAt *time.Time `json:"embedded_at,omitempty" gorm:"index"`
}

func (ArticleChunk) TableName() string {
	return "article_chunks"
}

type IArticleChunkRepository interface {
	// CreateBatch lưu toàn bộ chunk của 1 article trong 1 lần insert —
	// article nào cũng nguyên vẹn (không có chunk mồ côi nửa chừng).
	CreateBatch(ctx context.Context, chunks []*ArticleChunk) error
	// ExistsByArticleID cho worker biết bài nào đã chunk xong để bỏ qua.
	ExistsByArticleID(ctx context.Context, articleID int64) (bool, error)
	// DeleteByArticleID xóa chunk khi re-chunk, khi purge dọn bài, hoặc khi
	// embed xong (vector + text đã nằm Qdrant payload nên DB không giữ nữa).
	DeleteByArticleID(ctx context.Context, articleID int64) error
	// ListUnembeddedByArticle trả toàn bộ chunk chưa lên Qdrant của 1 bài
	// (bài worker embed đã claim) — xử lý hết rồi mới đánh done, đúng thứ tự
	// chunk hết file rồi mới embedding.
	ListUnembeddedByArticle(ctx context.Context, articleID int64) ([]*ChunkWithOwner, error)
	// DeleteStaleChunks dọn chunk già: row đã embedded quá hạn (đường success
	// sót lại do crash đúng cửa sổ trim-done) + row mồ côi không còn bài cha
	// quá hạn. Chỉ đụng row embedded hoặc mồ côi — chunk đang chờ (embedded_at
	// NULL còn bài) không bao giờ bị xóa nên retry luôn an toàn.
	DeleteStaleChunks(ctx context.Context, before time.Time) (int64, error)
	// MarkEmbedded đánh dấu các chunk đã upsert Qdrant xong — chỉ gọi SAU khi
	// Qdrant báo thành công (thứ tự Qdrant-trước-DB-sau như purge).
	MarkEmbedded(ctx context.Context, ids []int64) error
}

// ChunkWithOwner là 1 chunk kèm owner để embed — join 1 lần ở repo thay vì
// service query từng bài.
type ChunkWithOwner struct {
	Chunk      *ArticleChunk
	UserID     int64
	CategoryID int64
}
