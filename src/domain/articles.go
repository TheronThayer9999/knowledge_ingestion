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
	Description string `json:"description,omitempty"`

	UserID     int64 `json:"user_id" gorm:"not null;index"`
	CategoryID int64 `json:"category_id" gorm:"not null;index"`

	// Cặp cột queue cho worker chunk phase 1 — bài nào chunk_status pending
	// (hoặc processing quá lease) thì worker claim bằng FOR UPDATE SKIP
	// LOCKED nên N worker không giẫm nhau. json:"-" vì nội bộ worker.
	ChunkStatus      string    `json:"-" gorm:"not null;default:'pending'"`
	ChunkAttempts    int       `json:"-" gorm:"not null;default:0"`
	ChunkNextRetryAt time.Time `json:"-" gorm:"not null;default:now()"`
	// Cặp cột queue cho worker embed phase 2 — chỉ chạy khi chunk done, đúng
	// thứ tự "chunk hết file rồi mới embedding".
	EmbedStatus      string    `json:"-" gorm:"not null;default:'pending'"`
	EmbedAttempts    int       `json:"-" gorm:"not null;default:0"`
	EmbedNextRetryAt time.Time `json:"-" gorm:"not null;default:now()"`
	// EmbeddedAt mốc giờ embed xong (nil khi chưa xong) — audit/tracing, set
	// cùng transaction với done + trim chunk nên không lệch trạng thái.
	EmbeddedAt *time.Time `json:"-" gorm:"index"`
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

const (
	// MaxQueueAttempts số lần thử tối đa 1 bài trước khi đánh failed — file
	// lỗi thật (docx hỏng, ollama chết hẳn) không được retry vô hạn.
	MaxQueueAttempts = 10
	// QueueRetryBase cơ số backoff giữa các lần thử: 1ph, 2ph, 4ph...
	QueueRetryBase = time.Minute
	// MaxQueueDelay trần backoff — lỗi lâu thì thử lại mỗi 30 phút.
	MaxQueueDelay = 30 * time.Minute
)

// QueueBackoff tính giờ thử lại sau lần lỗi thứ attempts (đã tính lần vừa
// lỗi) — repo tự gọi trong Mark*Error nên service không tính tay.
func QueueBackoff(attempts int) time.Time {
	d := QueueRetryBase
	for i := 1; i < attempts && d < MaxQueueDelay; i++ {
		d *= 2
	}
	if d > MaxQueueDelay {
		d = MaxQueueDelay
	}
	return time.Now().Add(d)
}

func (Article) TableName() string {
	return "articles"
}

type IArticleRepository interface {
	Create(ctx context.Context, article *Article) error
	Update(ctx context.Context, article *Article) error
	Delete(ctx context.Context, article *Article) error

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

	// ClaimChunkPending hốt batch bài tới hạn chunk cho worker trong đúng 1
	// transaction (FOR UPDATE SKIP LOCKED, cùng pattern outbox ClaimPending)
	// — nhiều worker cùng poll không giẫm nhau; worker nào crash giữa chừng
	// thì row processing quá lease tự đủ điều kiện cho lần hốt sau. Chỉ lấy
	// bài loại StorageKey (URL chưa xử lý phase này).
	ClaimChunkPending(ctx context.Context, limit int, lease time.Duration) ([]*Article, error)
	// MarkChunkDone đánh dấu chunk xong 1 bài.
	MarkChunkDone(ctx context.Context, id int64) error
	// MarkChunkError ghi lỗi 1 bài: quá MaxQueueAttempts thì failed, còn lại
	// pending + lùi giờ thử theo QueueBackoff. attempts là số lần đã thử
	// (gồm lần vừa lỗi) — repo tự quyết, service chỉ truyền số.
	MarkChunkError(ctx context.Context, id int64, attempts int) error

	// ClaimEmbedPending như ClaimChunkPending nhưng cho phase 2 — chỉ hốt bài
	// đã chunk done (đúng thứ tự chunk hết file rồi mới embedding).
	ClaimEmbedPending(ctx context.Context, limit int, lease time.Duration) ([]*Article, error)
	// MarkEmbedDone đánh dấu embed xong 1 bài.
	MarkEmbedDone(ctx context.Context, id int64) error
	// MarkEmbedError như MarkChunkError cho phase 2.
	MarkEmbedError(ctx context.Context, id int64, attempts int) error

	// HardDelete xóa hẳn 1 row đã xóa mềm — chỉ worker janitor gọi.
	HardDelete(ctx context.Context, id int64) error
}

// Role interface cho worker — mỗi service chỉ phụ thuộc đúng phương thức nó
// gọi (ISP) thay vì ôm cả IArticleRepository ~20 method. Struct
// ArticleRepository implement tất cả nên loader/test không đổi gì ngoài kiểu
// tham số constructor.

// ArticleChunkQueue vai queue phase 1 của worker chunk.
type ArticleChunkQueue interface {
	ClaimChunkPending(ctx context.Context, limit int, lease time.Duration) ([]*Article, error)
	MarkChunkDone(ctx context.Context, id int64) error
	MarkChunkError(ctx context.Context, id int64, attempts int) error
	IsAlive(ctx context.Context, id int64) (bool, error)
}

// ArticleEmbedQueue vai queue phase 2 của worker embed.
type ArticleEmbedQueue interface {
	ClaimEmbedPending(ctx context.Context, limit int, lease time.Duration) ([]*Article, error)
	MarkEmbedDone(ctx context.Context, id int64) error
	MarkEmbedError(ctx context.Context, id int64, attempts int) error
	IsAlive(ctx context.Context, id int64) (bool, error)
}

// ArticleJanitor vai dọn dẹp của worker purge.
type ArticleJanitor interface {
	ListSoftDeleted(ctx context.Context, before time.Time, limit int) ([]*Article, error)
	HardDelete(ctx context.Context, id int64) error
}
