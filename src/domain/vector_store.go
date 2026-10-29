package domain

import "context"

// VectorPoint là 1 điểm worker index upsert lên Qdrant — service dựng thẳng
// từ chunk vừa cắt (+ owner lấy từ article), infra/qdrant map thành payload.
type VectorPoint struct {
	// ID là PointID TẤT ĐỊNH theo (article_id, chunk_index) — utils.NewPointID
	// (UUIDv5). Không lưu ở bảng nào: sinh lại được từ cùng text + cùng cấu
	// hình chunker, nên chạy lại vẫn ra đúng ID và upsert là GHI ĐỀ, không
	// bao giờ sinh point trùng. Đổi chunker sinh NÊN chunk khác thì phần dư
	// của lần cũ thành mồ côi — đó là lý do index chạy lại từ checkpoint 0
	// phải DeleteByArticle trước.
	ID          string
	Vector      []float32 // content vector (primary)
	TitleVector []float32 // title vector (article name) — dual embedding
	ArticleID   int64
	UserID      int64
	CategoryID  int64
	ChunkIndex  int
	PageNum     int
	// Section tiêu đề mục chứa chunk — payload để agent attribute đúng Điều.
	Section  string
	Text     string
	Keywords []string
}

// SearchFilter khóa phân quyền query — UserID bắt buộc (owner từ token,
// không tin client); CategoryIDs rỗng là mọi chủ đề, có thì khoanh nhiều chủ
// đề (match 1 trong các ID); ArticleID > 0 thì drill-down thêm 1 bài.
type SearchFilter struct {
	UserID      int64
	CategoryIDs []int64
	ArticleID   int64
}

// ScoredChunk là 1 hit search — service map sang response cho API/agent.
type ScoredChunk struct {
	PointID    string
	Score      float32
	ArticleID  int64
	ChunkIndex int
	PageNum    int
	Section    string
	Text       string
}

// SectionSpan là 1 mục của bài + vị trí chunk bắt đầu — bản đồ điều hướng
// cho tool read_article: LLM thấy "Điều 5 từ chunk 40" thì nhảy đọc thẳng
// từ 40 thay vì cuộn toàn file (file 500 trang không đọc nổi từng trang).
type SectionSpan struct {
	Name string
	From int
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
	// DeleteByArticles như DeleteByArticle nhưng nhiều bài 1 lần gọi — API
	// rebuild đưa cả danh mục về hàng đợi thì 1 filter MatchInts thay vì
	// N round trip. Rỗng thì no-op (idempotent như bản 1 bài).
	DeleteByArticles(ctx context.Context, articleIDs []int64) error
	// CountByArticle đếm point của 1 bài — worker embed dùng để guard nhánh
	// done-rỗng (chunk hết nhưng chưa chắc vector đã lên): count 0 thì không
	// được done, phải retry thay vì done âm thầm với search trống.
	CountByArticle(ctx context.Context, articleID int64) (int64, error)
	// CountByFilter đếm point khớp filter phân quyền (user_id + category_ids
	// + article_id) — read_article dùng để total_chunk không vượt phạm vi
	// scope. CountByArticle giữ nguyên cho worker (guard nội bộ, không scope).
	CountByFilter(ctx context.Context, filter SearchFilter) (int64, error)
	// CountCollection đếm TOÀN BỘ point của collection, không filter — con số
	// rẻ (1 round trip) để so với tổng articles.index_done_chunks: lệch nhau
	// là dấu collection bị drop/recreate mà DB vẫn báo done. Chỉ dùng cho
	// bước kiểm tra vận hành (reconcile lúc boot, endpoint consistency), không
	// phải đường search vì nó không phân quyền.
	CountCollection(ctx context.Context) (int64, error)
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
	// ScrollNeighbors đọc các chunk kề chunkIndex của 1 bài trong phạm vi
	// filter — tool read_context cho agent đọc thêm ngữ cảnh khi đoạn trích
	// bị cắt giữa Điều. Trả theo ChunkIndex tăng dần, Score = 0 (không phải
	// rank search). DB chunk đã trim sau embed nên đọc từ Qdrant payload.
	ScrollNeighbors(ctx context.Context, filter SearchFilter, center, window int) ([]*ScoredChunk, error)
	// ScrollRange đọc 1 lát chunk của 1 bài từ from (chunk_index >= from)
	// tối đa limit điểm, trả theo ChunkIndex tăng dần — tool read_article
	// cho agent đọc trực tiếp file theo trang (câu liệt kê/tóm tắt toàn
	// bài). ArticleID bắt buộc; tổng số chunk lấy qua CountByFilter.
	ScrollRange(ctx context.Context, filter SearchFilter, from, limit int) ([]*ScoredChunk, error)
	// ListSections trả các mục (section) của 1 bài theo thứ tự chunk kèm
	// chunk bắt đầu — tool list_sections đếm/liệt kê Điều (ground-truth) +
	// bản đồ cho read_article nhảy vùng. ArticleID bắt buộc, rỗng thì rỗng.
	ListSections(ctx context.Context, filter SearchFilter) ([]SectionSpan, error)
	// IsPermanentError báo lỗi có retry cũng vậy không (sai dim, mã gRPC
	// InvalidArgument/NotFound/Unauthenticated...) — service dùng để failed
	// luôn thay vì backoff. Triển khai ở infra.
	IsPermanentError(err error) bool
}

// Quy ước cho API search: chỉ search bài index_status=done, kèm
// score_threshold để không trả lời bừa khi không có đoạn nào gần — giữa lúc
// index đang chạy Qdrant phục vụ kết quả thiếu là trạng thái trung gian bình
// thường, không phải lỗi.
