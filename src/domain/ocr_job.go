package domain

import (
	"context"
	"time"
)

// OCRJob là một lần "quét tài liệu" ở tab Quét: một tệp ảnh/PDF scan được đưa vào,
// OCR từng trang thành chữ rồi trả về tệp PDF kết quả.
//
// VÌ SAO BẢNG RIÊNG, KHÔNG DÙNG LẠI articles: hai đường có mục tiêu khác nhau.
// articles là KHO TRI THỨC (extract → index → compile, text thô nằm ở
// article_pages); ocr_jobs là PHÒNG LÀM VIỆC TẠM — người dùng upload, xem, sửa
// tay, tải PDF rồi mới quyết định đẩy vào kho hay bỏ. Trộn hai thứ vào một bảng
// thì hoặc kho phải mang cột của UI, hoặc bản nháp bị pipeline nuốt mất. Đây
// cũng là lý do OCR của tính năng này ở src/common/scanocr, tách khỏi
// src/common/extractor.
//
// VÌ SAO CÓ JOB CẤP TRÊN TRONG KHI HÀNG ĐỢI THỰC SESTE THEO TRANG: người dùng
// cần biết "bài của tôi tới đâu" (tab Lịch sử + thanh tiến độ), còn worker thì
// claim từng trang. Job là chỗ gom số đếm, trang là chỗ chịu lease.
type OCRJob struct {
	BaseModel

	UserID int64 `json:"user_id" gorm:"not null;index"`
	// SourceName là tên tệp NGƯỜI DÙNG upload — chỉ để hiển thị. Không đưa vào
	// log (tên tệp có thể nhạy cảm, đã có convention ở src/common/logs).
	SourceName string `json:"source_name" gorm:"not null"`
	// SourceKey/OutputKey là object trong bucket tạm (scan/<job>/…), KHÔNG phải
	// key của kho. OutputKey rỗng khi chưa dựng PDF.
	SourceKey string `json:"source_key" gorm:"not null"`
	OutputKey string `json:"output_key" gorm:"not null;default:''"`

	// Status: pending|processing|done|failed|cancelled (Queue* ở articles.go +
	// OCRJobCancelled). Đây là trạng thái TỔNG HỢP từ các trang, worker chỉ đổi
	// khi không còn trang nào đang chạy — nên không thể có "done mà còn trang
	// pending".
	Status string `json:"status" gorm:"not null;default:'pending';index"`
	// TotalPages ghi ngay lúc tạo job (đã đo bằng pdfinfo / số ảnh upload) để
	// thanh tiến độ có mẫu số TRƯỚC khi trang đầu xong — nếu suy từ count row
	// thì job vừa tạo hiện 0/0.
	TotalPages  int `json:"total_pages" gorm:"not null;default:0"`
	DonePages   int `json:"done_pages" gorm:"not null;default:0"`
	FailedPages int `json:"failed_pages" gorm:"not null;default:0"`
	// OCRTargetPages là số trang THỰC SỰ cần OCR (B.4 — đo theo TỪNG trang),
	// khác TotalPages khi vài trang đã có chữ thật. Lưu chứ không suy lại từ bảng
	// trang vì tab Lịch sử phải nói được "198/200 trang là ảnh, giữ nguyên 2 trang
	// đã có chữ" sau khi job xong.
	OCRTargetPages int `json:"ocr_target_pages" gorm:"not null;default:0"`

	// Lang/DPI/PSM/Engine là lựa chọn của LẦN QUÉT này, lưu theo job để: (a) làm
	// lại trang không cần hỏi lại người dùng, (b) Lịch sử giải thích được vì sao
	// hai job cùng tệp cho kết quả khác nhau. Mặc định lấy từ scan_ocr.* lúc tạo.
	Lang   string `json:"lang" gorm:"not null;default:'vie'"`
	DPI    int    `json:"dpi" gorm:"not null;default:300"`
	PSM    int    `json:"psm" gorm:"not null;default:6"`
	Engine string `json:"engine" gorm:"not null;default:'tesseract'"`
	// ForceOCR: OCR cả những trang đã có text layer. Người dùng bấm khi PDF có
	// chữ nhưng chữ đó là rác (font không nhúng, pdftotext ra dấu ?).
	ForceOCR bool `json:"force_ocr" gorm:"not null;default:false"`

	ErrorCode    string `json:"error_code,omitempty" gorm:"size:64"`
	ErrorMessage string `json:"error_message,omitempty" gorm:"type:text"`

	// Attempts/NextRetryAt là lease cấp job — cùng khuôn với
	// extract_attempts/extract_next_retry_at: claim là hẹn NextRetry, job chết
	// thì kỳ sau nhận lại đúng hạn đó chứ không đổ dồn vào hàng đợi.
	Attempts    int       `json:"-" gorm:"not null;default:0"`
	NextRetryAt time.Time `json:"-" gorm:"not null;default:now()"`

	// ExpiresAt chốt mốc janitor dọn object tạm + row (B.11). Job đã push vào
	// kho được gia hạn dài hơn vì bản tạm không còn là nguồn duy nhất.
	ExpiresAt time.Time `json:"expires_at" gorm:"not null;index"`
	// PushedAt mốc người dùng bấm "Đưa vào kho" — nil là chưa. Có nó janitor
	// mới phân biệt được TTL ngắn/dài mà không cần đọc lại article.
	PushedAt *time.Time `json:"pushed_at,omitempty"`
	// PushedArticleID bài sinh ra từ job (audit ngược khi người dùng hỏi "file
	// scan của tôi nằm đâu trong kho").
	PushedArticleID *int64 `json:"pushed_article_id,omitempty"`
}

func (OCRJob) TableName() string {
	return "ocr_jobs"
}

// OCRJobCancelled là trạng thái duy nhất của hàng đợi quét không có trong bộ
// Queue* chung: người dùng bấm Huỷ. Huỷ KHÁC failed — failed là hệ thống làm
// hỏng còn cancelled là ý người dùng, nên UI không hiện nút "Thử lại".
const OCRJobCancelled = "cancelled"

// Trạng thái của MỘT trang. Tách khỏi trạng thái job vì người dùng có thể chỉ
// OCR lại 1 trang (V1) và vì một trang fail không được kéo cả job xuống.
const (
	// OCRPagePending: chờ tới lượt (worker claim theo trang, không theo job).
	OCRPagePending = "pending"
	// OCRPageProcessing: đã có worker giữ lease.
	OCRPageProcessing = "processing"
	// OCRPageDone: OCR xong, words/text đã nằm ở row này.
	OCRPageDone = "ocr_done"
	// OCRPageEdited: người dùng sửa tay sau OCR. Phân biệt với ocr_done để UI
	// hiện badge "đã sửa" và để không ai vô tình ghi đè công sửa bằng một lần
	// OCR lại hàng loạt.
	OCRPageEdited = "edited"
	// OCRPageFailed: hết trần thử, còn lỗi — người dùng bấm Thử lại thì service
	// đưa về pending + reset attempts.
	OCRPageFailed = "failed"
	// OCRPageSkippedHasText: trang đã có text layer đủ dày nên KHÔNG OCR (B.4 —
	// chỗ autoai đo cả file rồi bỏ sót trang scan).
	OCRPageSkippedHasText = "skipped_has_text"
	// OCRPageCancelled: trang chưa kịp chạy vì job bị huỷ.
	OCRPageCancelled = "cancelled"
)

// OCRJobPage là một trang của job. KHOÁ LÀ CẶP (job_id, page_num) — cùng khuôn
// CategoryClosure: hai field cùng primaryKey, không có BaseModel.
//
// VÌ SAO KHÔNG có id riêng + unique(job_id, page_num): bảng này chỉ được truy
// cập theo cặp (job, trang); PK ghép cho chính cái index đó mà không thừa cột
// vô nghĩa. Muốn tham chiếu 1 trang từ nơi khác thì thêm id — hiện chưa có.
//
// VÌ SAO LƯU words JSONB CHỮ KHÔNG TÁCH BẢNG ocr_job_page_words: mọi nhu cầu
// đọc đều là "cả trang" (dựng PDF, render overlay, hiển thị tab Xem & sửa),
// không có query nào trên TỪNG từ. Tách bảng là 200 dòng JOIN cho mỗi trang mà
// chẳng trả lại gì. Region re-OCR (V1) ghi đè nguyên mảng nên tính "dòng nào
// thay dòng nào" vẫn nằm trong một lần UPDATE.
type OCRJobPage struct {
	// CreatedAt/UpdatedAt để suy ra được "trang này nằm chờ từ bao giờ" — trang
	// kẹt lâu hơn lease là bằng chứng worker chết, không phải OCR chậm.
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	JobID   int64 `json:"job_id" gorm:"primaryKey"`
	PageNum int   `json:"page_num" gorm:"primaryKey"` // 1-based, khớp số trang trong tệp

	Status string `json:"status" gorm:"not null;default:'pending'"`

	// RenderKey là object ảnh đã render ở bucket tạm (scan/<job>/p3.png) — để
	// tab Xem & sửa lấy ảnh bằng presigned URL chứ không render lại bằng
	// pdftoppm mỗi lần mở. Rỗng khi chưa render.
	RenderKey string `json:"render_key" gorm:"not null;default:''"`
	RenderDPI int    `json:"render_dpi" gorm:"not null;default:0"`
	// WidthPx/HeightPx/Rotation là bằng chứng đo lúc render — needed cho OCR lại
	// một vùng (B.6): box tỉ lệ của vùng nhân ngược ra pixel ảnh gốc.
	WidthPx      int  `json:"width_px" gorm:"not null;default:0"`
	HeightPx     int  `json:"height_px" gorm:"not null;default:0"`
	Rotation     int  `json:"rotation" gorm:"not null;default:0"`
	HasTextLayer bool `json:"has_text_layer" gorm:"not null;default:false"`
	// CharsExisting là số KÝ TỰ của text layer có sẵn (đã đếm rune, bỏ khoảng
	// trắng) — lưu để UI nói được "trang này đã có 812 ký tự, không OCR".
	CharsExisting int `json:"chars_existing" gorm:"not null;default:0"`

	// Words là JSONB mảng từ kèm box chuẩn hoá 0..1 + conf (§B.3). Đây là BẢN
	// MỚI NHẤT (đã tính sửa tay), dựng PDF từ nó.
	Words string `json:"words" gorm:"type:jsonb;not null;default:'[]'"`
	// MeanConf/LowConfWords do engine trả về, lưu để tab Lịch sử lọc được
	// "trang nào cần người xem lại" mà không parse JSONB từng trang.
	MeanConf     float64 `json:"mean_conf" gorm:"not null;default:0"`
	LowConfWords int     `json:"low_conf_words" gorm:"not null;default:0"`
	// Text là bản chữ TUẦN TỰ của trang (nối các từ) — dùng cho dựng PDF/A và
	// cho push vào kho; dẫn xuất từ Words nên coi như cache, không phải nguồn.
	Text string `json:"text" gorm:"type:text;not null;default:''"`

	// Attempts/NextRetryAt: lease CẤP TRANG — worker nhận 1 lô trang, mỗi trang
	// tự chịu timeout và retry riêng. Trang 47 của file 300 trang hỏng thì chỉ
	// trang 47 hẹn lại, 299 trang còn không bị kéo xuống.
	Attempts    int       `json:"-" gorm:"not null;default:0"`
	NextRetryAt time.Time `json:"-" gorm:"not null;default:now()"`
	LastError   string    `json:"last_error,omitempty" gorm:"type:text"`
}

func (OCRJobPage) TableName() string {
	return "ocr_job_pages"
}

// OCRWordsEmpty là giá trị jsonb hợp lệ của một trang CHƯA có từ nào. Bắt buộc
// phải là "[]" chứ không phải "": rỗng là jsonb KHÔNG HỢP LỆ, INSERT cả job 200
// trang sẽ chết vì một trang trắng.
const OCRWordsEmpty = "[]"

// ClaimedPage là một trang vừa được worker giữ kèm job của nó — kiểu trả về
// riêng vì service cần cả hai (lang/dpi/engine nằm ở job, page_num ở trang) mà
// không phải gọi thêm 1 câu SELECT.
type ClaimedPage struct {
	Job  *OCRJob
	Page *OCRJobPage
}

// ScanJobProgress là số đếm trạng thái của job, đọc 1 câu cho endpoint tiến độ
// và cho worker quyết định đổi status job.
type ScanJobProgress struct {
	Done    int
	Failed  int
	Pending int
	Running int
	Total   int
}

// OCRJobStatusFromProgress chốt trạng thái job từ đếm trang — hàm thuần nên test
// được không cần DB, và vì CHỈ một chỗ suy ra trạng thái nên service với endpoint
// tiến độ không thể bất đồng (SyncCounts ghi cột, GET chỉ đọc cột).
//
// Pending > 0 vẫn ra processing: hàm này chỉ được gọi SAU một hoạt động của
// worker (vừa xong/hỏng một trang), nên "còn trang chờ" nghĩa là hàng đợi đang
// chạy chứ không phải đứng. cancelled thì KHÔNG suy ra ở đây — người dùng huỷ là
// ý cuối cùng, SyncCounts bỏ qua job đã cancelled.
func OCRJobStatusFromProgress(p ScanJobProgress) string {
	switch {
	case p.Running > 0 || p.Pending > 0:
		return QueueProcessing
	case p.Total > 0 && p.Failed == p.Total:
		// Mọi trang đều hỏng: không có PDF nào để xem, báo failed để người dùng
		// thấy nút Thử lại thay vì "done" rồi tải về file rỗng.
		return QueueFailed
	default:
		return QueueDone
	}
}

// IOCRJobRepository là hàng đợi quét. MỌI method đều nhận userID để scope
// (quy ước của repo: không tin client, lọc theo chủ ở câu SQL chứ không
// kiểm tra ở service sau khi đọc); riêng đường worker (Claim*) không có userID
// vì worker phục vụ mọi người dùng và đã bị khoá bởi lease.
type IOCRJobRepository interface {
	// Create lưu job kèm toàn bộ trang trong MỘT transaction: không được có
	// trạng thái "job đã có mà trang chưa sinh" — worker claim được job rỗng
	// rồi đánh done ngay.
	Create(ctx context.Context, job *OCRJob, pages []*OCRJobPage) error
	Get(ctx context.Context, jobID, userID int64) (*OCRJob, error)
	GetPage(ctx context.Context, jobID int64, pageNum int) (*OCRJobPage, error)
	// ListPages trả trang của job theo thứ tự trang, fromPage 1-based, tối đa
	// limit — dựng PDF đọc theo cửa sổ để file 300 trang không lên RAM 1 lần.
	ListPages(ctx context.Context, jobID int64, fromPage, limit int) ([]*OCRJobPage, error)
	// List theo chủ, mới nhất trước, kèm tổng để phân trang tab Lịch sử.
	List(ctx context.Context, userID int64, status string, limit, offset int) ([]*OCRJob, int64, error)
	// UpdatePageSettings lưu bằng chứng render (ảnh + khổ) và/hoặc kết quả OCR.
	// Tách khỏi Claim để đường sửa tay (PATCH) không đụng máy trạng thái queue.
	UpdatePage(ctx context.Context, page *OCRJobPage) error
	// SavePageWords ghi kết quả OCR: words/text/conf + chuyển status sang
	// ocr_done, đồng thời reset lease. attempts tăng ở FailPage chứ không ở đây.
	SavePageWords(ctx context.Context, jobID int64, pageNum int, words, text string,
		meanConf float64, lowConf int) error
	// FailPage ghi nhận một trang hỏng: nếu còn lượt thì hẹn lại (backoff), hết
	// lượt thì mới thành failed. Trả status mới của trang để service log đúng.
	FailPage(ctx context.Context, jobID int64, pageNum int, lastErr string,
		maxAttempts int, backoff time.Duration) (string, error)
	// ResetPage đưa trang về pending cho nút "Thử lại" (kể cả trang failed lẫn
	// trang người dùng muốn OCR lại).
	ResetPage(ctx context.Context, jobID int64, pageNum int) error
	// Progress đếm trang theo trạng thái — nguồn của GET /scan/jobs/:id.
	Progress(ctx context.Context, jobID int64) (*ScanJobProgress, error)
	// SyncCounts viết DonePages/FailedPages từ Progress rồi đổi status job
	// trong cùng transaction (đọc-count-rồi-ghi, không để service tự suy).
	SyncCounts(ctx context.Context, jobID int64) (*ScanJobProgress, error)
	// MarkRunning hẹn lease cấp job và đưa job về processing (claim đầu tiên).
	MarkRunning(ctx context.Context, jobID int64, lease time.Duration) error
	// MarkDone chốt job + output_key; MarkFailed chốt lỗi hiển thị cho người dùng.
	MarkDone(ctx context.Context, jobID int64, outputKey string) error
	MarkFailed(ctx context.Context, jobID int64, code, message string) error
	// ClaimPages giữ lease cho TỐI ĐA limit trang pending tới hạn trên toàn bộ
	// job chưa bị huỷ — FOR UPDATE SKIP LOCKED nên nhiều worker không giẫm
	// nhau. Đây là đơn vị claim thật của tính năng (theo trang, không theo file).
	ClaimPages(ctx context.Context, limit int, lease time.Duration) ([]*ClaimedPage, error)
	// ListReadyToBuild trả job đang pending/processing mà KHÔNG còn trang pending/running
	// (vd. PDF đã có text layer → mọi trang skipped) để worker dựng PDF và chốt done.
	ListReadyToBuild(ctx context.Context) ([]*OCRJob, error)
	// ReleaseUnfinished trả mọi trang đang processing về pending khi worker tắt
	// — cùng lý do với ReleaseExtractClaims: không để trang kẹt đến hết lease.
	ReleaseUnfinished(ctx context.Context) (int64, error)
	// Cancel đánh dấu huỷ: job thành cancelled, trang pending thành cancelled,
	// trang đang chạy thì worker tự cắt qua ctx. Job của người khác hoặc đã xong
	// thì trả domain.ErrNotFound — service dịch thành 404/409, không im lặng.
	Cancel(ctx context.Context, jobID, userID int64) error
	// Delete xoá job của ĐÚNG chủ: row trang xoá cứng (bảng không có deleted_at),
	// row job xoá mềm theo convention BaseModel. Trả job vừa xoá để service biết
	// key nào cần xoá khỏi bucket — thứ tự là row trước, object sau.
	Delete(ctx context.Context, jobID, userID int64) (*OCRJob, error)
	// ListExpired trả job quá hạn để janitor xoá object rồi mới xoá row theo ids.
	ListExpired(ctx context.Context, now time.Time, limit int) ([]*OCRJob, error)
	// DeleteIDs xoá cứng theo danh sách id cho janitor (janitor là hệ thống,
	// không có userID — nó chỉ được gọi với id lấy từ ListExpired).
	DeleteIDs(ctx context.Context, ids []int64) (int64, error)
	// MarkPushed ghi bài kho sinh ra từ job (kèm mốc) để janitor gia hạn TTL.
	MarkPushed(ctx context.Context, jobID, userID, articleID int64, expiresAt time.Time) error
	// SetExpiresAt đổi hạn TTL — service dùng khi đổi lựa chọn giữ file.
	SetExpiresAt(ctx context.Context, jobID, userID int64, expiresAt time.Time) error
}
