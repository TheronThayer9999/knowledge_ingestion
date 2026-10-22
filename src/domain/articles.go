package domain

import (
	"context"
	"time"
)

type Article struct {
	BaseModel

	// Article có đúng 1 trong 2 nguồn — service bắt đúng 1, không tin client:
	// URL là link web bên ngoài (worker fetch sau), StorageKey là object đã
	// upload lên kho qua presign (key dạng "uploads/<uuid>.<ext>").
	URL         string `json:"url" gorm:"not null"`
	StorageKey  string `json:"storage_key" gorm:"not null;index"`
	Name        string `json:"name" gorm:"not null"`
	ContentType string `json:"content_type" gorm:"not null"`
	// DocType là loại tài liệu để chọn chiến lược chunking: general, laws,
	// table, paper, book, qa, manual. Rỗng → worker tự detect từ content_type
	// + tên file. Đổi doc_type thì phải rebuild vector.
	DocType string `json:"doc_type" gorm:"not null;default:'general'"`
	Description string `json:"description,omitempty"`

	UserID     int64 `json:"user_id" gorm:"not null;index"`
	CategoryID int64 `json:"category_id" gorm:"not null;index"`

	// Metadata trích lúc worker chunk (best-effort, rỗng khi văn bản không
	// ghi): PageCount đếm trang extractor thấy, Version + EffectiveDate trích
	// từ đầu văn bản ("Phiên bản: 2024-Q3", "hiệu lực từ 01/10/2024"). Tool
	// article_info đọc các cột này để trả lời câu hỏi metadata mà không cần
	// đoán từ đoạn trích.
	PageCount     int    `json:"page_count" gorm:"not null;default:0"`
	Version       string `json:"version" gorm:"size:50;not null;default:''"`
	EffectiveDate string `json:"effective_date" gorm:"size:50;not null;default:''"`

	// Cặp cột queue cho worker extract phase 0 — bóc text từ blob (OCR nếu
	// là PDF scan) rồi ghi vào article_pages. Đứng trước index vì index đọc
	// nội dung từ đó theo cửa sổ trượt, không tải lại blob gốc.
	ExtractStatus      string    `json:"-" gorm:"not null;default:'pending'"`
	ExtractAttempts    int       `json:"-" gorm:"not null;default:0"`
	ExtractNextRetryAt time.Time `json:"-" gorm:"not null;default:now()"`
	// ExtractedAt mốc trích xong (nil khi chưa) — audit, set cùng lệnh đánh
	// done nên không lệch trạng thái.
	//
	// KHÔNG có cột "toàn văn bản" ở đây: text nằm ở article_pages theo từng
	// trang để phase index đọc theo cửa sổ, RAM độc lập kích thước file.
	// Gộp cả file vào 1 cột text thì mỗi lần index phải kéo hết lên RAM.
	ExtractedAt *time.Time `json:"-" gorm:"index"`

	// Cặp cột queue cho worker index phase 1 — cắt chunk rồi embed, gộp
	// trong 1 lần. Gộp vì chunk sau khi tách phase extract chỉ còn là CPU +
	// INSERT: không còn lý do để đi qua bảng trung gian, Qdrant đã giữ text
	// + vector trong payload.
	IndexStatus      string    `json:"-" gorm:"not null;default:'pending'"`
	IndexAttempts    int       `json:"-" gorm:"not null;default:0"`
	IndexNextRetryAt time.Time `json:"-" gorm:"not null;default:now()"`
	// IndexDoneChunks là checkpoint: đã upsert N chunk đầu tiên của bài. Giữ
	// để job chết giữa file lớn thì kỳ sau bỏ qua phần đã xong thay vì embed
	// lại — MỘT SỐ NGUYÊN thay cho cả bảng chunk. Chỉ dùng được vì PointID
	// tất định (utils.NewPointID), nên embed lại vẫn ghi đè đúng point cũ.
	IndexDoneChunks int `json:"-" gorm:"not null;default:0"`
	// IndexedAt mốc giờ index xong (nil khi chưa xong) — audit/tracing, set
	// cùng lúc đánh done nên không lệch trạng thái.
	IndexedAt *time.Time `json:"-" gorm:"index"`
}

// Trạng thái queue dùng chung cho chunk/embed — pending: chờ làm;
// processing: worker đã claim (kèm lease ở NextRetry); done: xong;
// failed: quá số lần thử, chờ người xử lý.
const (
	QueuePending    = "pending"
	QueueProcessing = "processing"
	QueueDone       = "done"
	QueueFailed     = "failed"
)

func (Article) TableName() string {
	return "articles"
}

// QueueStats là bảng chéo đếm bài theo trạng thái của 2 phase (extract ×
// index) — endpoint vận hành đọc để biết hàng đợi đang tắc ở phase nào.
// Các phase luôn chạy tuần tự nên phần lớn số ở cột "trước" done và cột "sau"
// pending.
type QueueStats struct {
	ExtractPending    int64
	ExtractProcessing int64
	ExtractDone       int64
	ExtractFailed     int64
	IndexPending      int64
	IndexProcessing   int64
	IndexDone         int64
	IndexFailed       int64
}

type IArticleRepository interface {
	Create(ctx context.Context, article *Article) error
	Update(ctx context.Context, article *Article) error
	// UpdateMetadata ghi 3 cột metadata chunk (số trang, phiên bản, hiệu
	// lực) mà không đụng gì khác — xem vai ArticleExtractQueue để biết vì sao
	// không dùng Update cho việc này.
	UpdateMetadata(ctx context.Context, id int64, pageCount int, version, effectiveDate string) error
	Delete(ctx context.Context, article *Article) error
	// DeleteByCategoryIDs xoá mềm mọi bài còn sống thuộc các danh mục
	// categoryIDs — cascade khi xoá folder cha. Trả số row vừa xoá; bài đã
	// xoá mềm từ trước không bị đụng nên giữ nguyên mốc purge.
	DeleteByCategoryIDs(ctx context.Context, categoryIDs []int64) (int64, error)

	// GetByID chỉ thấy bài của đúng owner — id của người khác thì 404 như không tồn tại.
	GetByID(ctx context.Context, id int64, userID int64) (*Article, error)

	// IsAlive báo bài còn sống không (tồn tại + chưa xóa mềm) — worker check
	// cuối mỗi bài đang xử lý: user xóa giữa chừng thì dọn orphan (chunk/
	// vector vừa tạo) thay vì để lại rác mà purge không thấy (purge chỉ thấy
	// bài xóa mềm, không thấy chunk mồ côi của bài đã xóa hẳn).
	IsAlive(ctx context.Context, id int64) (bool, error)

	// ExistsByURL dedup URL trong phạm vi từng user.
	ExistsByURL(ctx context.Context, url string, userID int64) (bool, error)

	// ExistsByStorageKey dedup object kho trong phạm vi từng user — 1 blob
	// chỉ map tới 1 article.
	ExistsByStorageKey(ctx context.Context, storageKey string, userID int64) (bool, error)

	ListByCategoryID(ctx context.Context, categoryID int64, userID int64, limit, offset int) ([]*Article, error)

	ListByCategoryName(ctx context.Context, categoryName string, userID int64, limit, offset int) ([]*Article, error)

	// ListSoftDeleted trả các bài đã xóa mềm trước mốc before (cũ nhất
	// trước) để worker dọn blob + xóa hẳn theo đợt.
	ListSoftDeleted(ctx context.Context, before time.Time, limit int) ([]*Article, error)

	// ClaimExtractPending hốt batch bài tới hạn trích text cho worker trong
	// đúng 1 transaction (FOR UPDATE SKIP LOCKED, cùng pattern chunk/embed).
	// Chỉ lấy bài loại StorageKey (URL chưa xử lý phase này).
	ClaimExtractPending(ctx context.Context, limit int, lease time.Duration) ([]*Article, error)
	// MarkExtractDone đánh dấu trích xong 1 bài. Nội dung nằm ở
	// article_pages (ghi trong lúc trích) nên lệnh này chỉ đóng trạng thái +
	// mốc thời gian — không có text nào phải gửi kèm.
	MarkExtractDone(ctx context.Context, id int64) error
	// MarkExtractError ghi lỗi trích 1 bài — cùng luật backoff/failed như
	// MarkIndexError.
	MarkExtractError(ctx context.Context, id int64, attempts int) error
	// ReleaseExtractClaims trả các bài đang processing về pending ngay khi job
	// bị hủy do shutdown (không tăng attempts) — 1 câu UPDATE cho cả lô, không
	// phải N query. Giữ article_pages để kỳ sau resume.
	ReleaseExtractClaims(ctx context.Context, ids []int64) error

	// ClaimIndexPending hốt batch bài tới hạn đánh chỉ mục cho worker trong
	// đúng 1 transaction (FOR UPDATE SKIP LOCKED, cùng pattern outbox
	// ClaimPending) — nhiều worker cùng poll không giẫm nhau; worker nào
	// crash giữa chừng thì row processing quá lease tự đủ điều kiện cho lần
	// hốt sau. Chỉ lấy bài đã trích xong (extract_status done).
	ClaimIndexPending(ctx context.Context, limit int, lease time.Duration) ([]*Article, error)
	// MarkIndexProgress ghi checkpoint sau mỗi đợt đã upsert — đây là chỗ
	// TIẾN TRÌNH ĐƯỢC GHI XUỐNG, nhờ nó job chết giữa file lớn thì kỳ sau
	// bỏ qua phần đã xong. done là TỔNG số chunk đã upsert (đếm từ đầu), nên
	// ghi là cộng dồn cũng được.
	MarkIndexProgress(ctx context.Context, id int64, done int) error
	// MarkIndexDone đánh dấu xong 1 bài (kèm mốc indexed_at) — service gọi
	// khi đã upsert hết chunk, Qdrant là nguồn sự thật.
	MarkIndexDone(ctx context.Context, id int64) error
	// MarkIndexError ghi lỗi 1 bài: quá utils.MaxQueueAttempts thì failed, còn
	// lại pending + lùi giờ thử theo utils.QueueBackoff. attempts là số lần
	// đã thử (gồm lần vừa lỗi) — repo tự quyết, service chỉ truyền số.
	MarkIndexError(ctx context.Context, id int64, attempts int) error
	// ReleaseIndexClaims trả các bài đang processing về pending ngay khi job
	// bị hủy do shutdown (không tăng attempts) — 1 câu UPDATE cho cả lô, giữ
	// index_done_chunks để kỳ sau resume.
	ReleaseIndexClaims(ctx context.Context, ids []int64) error

	// HardDeleteMany xóa hẳn nhiều row 1 câu — janitor purge gom các bài đã
	// dọn xong blob + vector trong kỳ, thay vì 1 DELETE mỗi bài. Không lọc
	// owner: đây là job hệ thống, id do chính ListSoftDeleted quyết định.
	HardDeleteMany(ctx context.Context, ids []int64) error

	// ListSoftDeletedSince bài đã xóa mềm TỪ mốc after trở đi (còn trong cửa
	// sổ undo) — phần đã quá grace thì ListSoftDeleted đã lo, 2 query bù
	// nhau phủ hết số bài đang xóa mềm. Chỉ endpoint vận hành gọi.
	ListSoftDeletedSince(ctx context.Context, after time.Time, limit int) ([]*Article, error)

	// QueueStats đếm bài theo trạng thái 2 phase — 1 câu GROUP BY trả về
	// bảng chéo, để endpoint vận hành vẽ được toàn cảnh hàng đợi mà không
	// phải kéo hết bài về đếm tay.
	QueueStats(ctx context.Context) (*QueueStats, error)

	// ListByExtractStatus / ListByIndexStatus lấy bài theo trạng thái 1 phase,
	// mới nhất trước — endpoint vận hành liệt kê "đang xử lý file nào" và
	// "bài nào fail" để canh tiến trình. Bài đã xóa mềm không lọt vào (gorm
	// tự loại) vì phần purge có mục riêng.
	ListByExtractStatus(ctx context.Context, status string, limit, offset int) ([]*Article, error)
	ListByIndexStatus(ctx context.Context, status string, limit, offset int) ([]*Article, error)

	// IDsByCategory trả id các bài còn sống trong 1 danh mục của đúng owner
	// — API rebuild resolve scope category thành danh sách id cụ thể.
	IDsByCategory(ctx context.Context, categoryID int64, userID int64) ([]int64, error)
	// ForeignIDs trả các id trong ids KHÔNG thuộc owner (bài của user khác,
	// bài đã xóa hẳn, hoặc id không tồn tại) — gate cho mọi thao tác xóa
	// hàng loạt theo id client gửi lên.
	//
	// Cần vì DeleteByArticleID/DeleteByArticles chỉ lọc article_id, KHÔNG có
	// user_id: nếu service xóa trước rồi mới kiểm tra quyền thì client chỉ
	// cần đoán ra id người khác là xoá sạch chunk + vector của họ, còn
	// ResetQueue thì khớp 0 row nên bài đó kẹt done mà không còn gì để
	// search — hỏng âm thầm.
	//
	// Unscoped để bài đã xóa mềm vẫn được kiểm đúng chủ: lọc soft-delete
	// thì id của chính mình đã xoá sẽ bị báo nhầm là của người khác.
	ForeignIDs(ctx context.Context, ids []int64, userID int64) ([]int64, error)
	// ListDoneIDs lọc trong ids chỉ giữ bài đã embed done của đúng owner —
	// service search chặn hit từ bài đang embed dở (vector thiếu) để agent
	// không trả lời trên ngữ cảnh nửa vời.
	ListDoneIDs(ctx context.Context, userID int64, ids []int64) ([]int64, error)
	// ResetQueue đưa các bài về pending ở phase INDEX (attempts 0, retry now,
	// indexed_at NULL, index_done_chunks 0) để worker cắt + embed lại từ đầu
	// — dùng khi Qdrant mất collection/chuyển cụm mới, hoặc đổi cấu hình
	// chunker.
	//
	// KHÔNG reset phase extract: nội dung đã trích nằm ở article_pages nên
	// index lại không tốn OCR lần nào — đó là lý do tách phase. ResetExtract là
	// hàm riêng cho đúng việc đó — chỉ gọi khi extractor đã cải thiện (vd đổi
	// renderer OCR) và text cũ cần trích lại.
	ResetQueue(ctx context.Context, ids []int64, userID int64) (int64, error)
	// ResetExtract đưa phase extract về pending để worker trích lại từ blob —
	// dùng khi muốn thay đổi chất lượng trích (OCR) mà không đụng phase nào
	// khác.
	//
	// Bắt buộc gọi IArticlePageRepository.DeleteByArticles TRƯỚC hàm này:
	// giữ trang cũ lại thì worker thấy "đã trích tới trang cuối" và đánh done
	// mà không trích gì, bài chỉ còn nửa nội dung cũ. Thứ tự này thuộc
	// tầng service, không phải tầng repo.
	ResetExtract(ctx context.Context, ids []int64, userID int64) (int64, error)
}

// Role interface cho worker — mỗi service chỉ phụ thuộc đúng phương thức nó
// gọi (ISP) thay vì ôm cả IArticleRepository ~20 method. Struct
// ArticleRepository implement tất cả nên loader/test không đổi gì ngoài kiểu
// tham số constructor.

// ArticleIndexQueue vai queue phase 1 của worker index (cắt chunk + embed).
type ArticleIndexQueue interface {
	ClaimIndexPending(ctx context.Context, limit int, lease time.Duration) ([]*Article, error)
	MarkIndexProgress(ctx context.Context, id int64, done int) error
	MarkIndexDone(ctx context.Context, id int64) error
	MarkIndexError(ctx context.Context, id int64, attempts int) error
	// ReleaseIndexClaims trả cả lô bài đang processing về pending khi job bị
	// hủy (shutdown), KHÔNG tăng attempts — lỗi không thuộc về bài. Giữ nguyên
	// index_done_chunks để kỳ sau resume, không embed lại từ đầu.
	ReleaseIndexClaims(ctx context.Context, ids []int64) error
	IsAlive(ctx context.Context, id int64) (bool, error)
}

// ArticleExtractQueue vai queue phase 0 của worker extract.
type ArticleExtractQueue interface {
	ClaimExtractPending(ctx context.Context, limit int, lease time.Duration) ([]*Article, error)
	MarkExtractDone(ctx context.Context, id int64) error
	MarkExtractError(ctx context.Context, id int64, attempts int) error
	// ReleaseExtractClaims trả cả lô bài đang processing về pending khi job bị
	// hủy (shutdown), KHÔNG tăng attempts — lỗi không thuộc về bài. Giữ nguyên
	// article_pages đã ghi để kỳ sau trích tiếp.
	ReleaseExtractClaims(ctx context.Context, ids []int64) error
	IsAlive(ctx context.Context, id int64) (bool, error)
	// UpdateMetadata ghi 3 cột metadata bài (số trang, phiên bản, hiệu lực).
	// Đặt ở phase extract vì đây là lúc duy nhất các trang vừa trích còn nằm
	// trong RAM — đọc vài trang đầu (nơi phiên bản/hiệu lực nằm) và đếm số
	// trang đều rẻ nhất đúng lúc này.
	// Update với map chứ không Save cả struct: Save ghi ĐÚNG mọi cột kể cả
	// cặp queue extract_*/index_*, nên worker extract có thể vô tình ghi đè
	// trạng thái mà worker index vừa đặt — chỉ đúng 3 cột cần thiết.
	UpdateMetadata(ctx context.Context, id int64, pageCount int, version, effectiveDate string) error
}

// ArticleJanitor vai dọn dẹp của worker purge.
type ArticleJanitor interface {
	ListSoftDeleted(ctx context.Context, before time.Time, limit int) ([]*Article, error)
	HardDeleteMany(ctx context.Context, ids []int64) error
}

// ArticleCategoryCascade vai cascade khi xoá folder — category service chỉ
// cần đúng 1 method, không ôm cả IArticleRepository.
type ArticleCategoryCascade interface {
	DeleteByCategoryIDs(ctx context.Context, categoryIDs []int64) (int64, error)
}

// ArticleAdminQueue vai của controller admin — 9 phương thức nó thật sự
// gọi, tách khỏi IArticleRepository (~26 method) theo đúng ISP các vai ở trên.
//
// Lý do tách: IArticleRepository là vai LỚN dành cho service CRUD đầy đủ,
// còn admin chỉ đọc queue + reset scope. Giữ nguyên vai lớn thì admin service
// (và test của nó) bị ràng buộc vào ~17 phương thức không dùng, mỗi phương
// thức thêm vào IArticleRepository lại kéo theo sửa ở đây dù không liên quan.
type ArticleAdminQueue interface {
	// ResetScope
	IDsByCategory(ctx context.Context, categoryID int64, userID int64) ([]int64, error)
	ForeignIDs(ctx context.Context, ids []int64, userID int64) ([]int64, error)
	ResetQueue(ctx context.Context, ids []int64, userID int64) (int64, error)
	ResetExtract(ctx context.Context, ids []int64, userID int64) (int64, error)
	// QueueStatus
	QueueStats(ctx context.Context) (*QueueStats, error)
	ListByExtractStatus(ctx context.Context, status string, limit, offset int) ([]*Article, error)
	ListByIndexStatus(ctx context.Context, status string, limit, offset int) ([]*Article, error)
	ListSoftDeleted(ctx context.Context, before time.Time, limit int) ([]*Article, error)
	ListSoftDeletedSince(ctx context.Context, after time.Time, limit int) ([]*Article, error)
}
