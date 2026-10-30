package repository

import (
	"context"
	stderrors "errors"
	"strings"
	"time"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OCRJobRepository là hàng đợi của tab Quét.
//
// KHÁC ba hàng đợi articles (extract/index/compile) ở chỗ ĐƠN VỊ CLAIM LÀ TRANG
// chứ không phải bài: một tệp 300 trang OCR mất ~10 phút, claim theo bài thì worker
// thứ hai ngồi nhìn, còn claim theo trang thì hai worker cùng tải một tệp và mỗi
// người làm một nửa. Vì vậy repo có HAI lớp lease (job: để Lịch sử không hiện "chờ"
// vĩnh viễn; trang: để retry thật sự), và vì vậy mọi UPDATE ở đây đều dùng cặp
// (job_id, page_num) — không có id trang để mà tham chiếu.
type OCRJobRepository struct {
	db *gorm.DB
}

func NewOCRJobRepository(db *gorm.DB) domain.IOCRJobRepository {
	return &OCRJobRepository{db: db}
}

// Create ghi job + toàn bộ trang trong MỘT transaction.
//
// VÌ SAO KHÔNG TÁCH HAI LẦN GỌI: worker claim theo trang, nên "job đã có mà trang
// chưa sinh" là trạng thái cho phép một job được đánh done ngay lập tức với 0 trang
// — người dùng thấy "hoàn tất" rồi tải về PDF rỗng.
func (r *OCRJobRepository) Create(ctx context.Context, job *domain.OCRJob, pages []*domain.OCRJobPage) error {
	return dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		// JobID do repo chốt, không tin caller điền — tạo job là một thao tác,
		// trang nào cũng phải trỏ đúng id vừa sinh.
		for _, p := range pages {
			p.JobID = job.ID
		}
		for _, batch := range utils.Batch(pages, constants.SCAN_PAGE_INSERT_BATCH) {
			if err := tx.Create(&batch).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *OCRJobRepository) Get(ctx context.Context, jobID, userID int64) (*domain.OCRJob, error) {
	var job domain.OCRJob
	if err := dbConn(ctx, r.db).First(&job, "id = ? AND user_id = ?", jobID, userID).Error; err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &job, nil
}

// GetPage đọc theo (job, trang) mà KHÔNG lọc user_id: chủ sở hữu đã được kiểm
// bằng Get(job) ở service trước khi tới đây — kiểm hai lần trên hai bảng khác
// nhau là hai chỗ để sửa sót.
func (r *OCRJobRepository) GetPage(ctx context.Context, jobID int64, pageNum int) (*domain.OCRJobPage, error) {
	var page domain.OCRJobPage
	err := dbConn(ctx, r.db).First(&page, "job_id = ? AND page_num = ?", jobID, pageNum).Error
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &page, nil
}

func (r *OCRJobRepository) ListPages(ctx context.Context, jobID int64, fromPage, limit int) ([]*domain.OCRJobPage, error) {
	if limit <= 0 {
		limit = constants.SCAN_PAGE_WINDOW
	}
	if fromPage < 1 {
		fromPage = 1
	}
	pages := make([]*domain.OCRJobPage, 0, limit)
	err := dbConn(ctx, r.db).Where("job_id = ? AND page_num >= ?", jobID, fromPage).
		Order("page_num ASC").Limit(limit).Find(&pages).Error
	if err != nil {
		return nil, err
	}
	return pages, nil
}

// List trả job của một chủ, mới nhất trước, kèm tổng row cho phân trang tab Lịch
// sử. status rỗng = mọi trạng thái.
func (r *OCRJobRepository) List(ctx context.Context, userID int64, status string, limit, offset int) ([]*domain.OCRJob, int64, error) {
	if limit <= 0 {
		limit = constants.SCAN_LIST_LIMIT_DEFAULT
	}
	// Trần ở tầng repo: một cú ?limit=1000000 từ client không được phép biến
	// trang Lịch sử thành một cú seq scan cả bảng.
	if limit > constants.SCAN_LIST_LIMIT_MAX {
		limit = constants.SCAN_LIST_LIMIT_MAX
	}
	q := dbConn(ctx, r.db).Where("user_id = ?", userID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Model(&domain.OCRJob{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	jobs := make([]*domain.OCRJob, 0, limit)
	if offset < 0 {
		offset = 0
	}
	err := q.Order("id DESC").Limit(limit).Offset(offset).Find(&jobs).Error
	if err != nil {
		return nil, 0, err
	}
	return jobs, total, nil
}

// UpdatePage ghi lại một trang sau khi NGƯỜI DÙNG sửa tay (tab Xem & sửa).
//
// VÌ SAO DÙNG Select tường minh chứ không Save: Save kéo theo mọi cột, bao gồm
// attempts/next_retry_at — một cú PATCH chữ sai chính tả mà reset được lease thì
// hàng đợi mất luôn lịch sử thử. Danh sách cột ở đây là hợp đồng "đường sửa tay
// chạm được vào gì".
func (r *OCRJobRepository) UpdatePage(ctx context.Context, page *domain.OCRJobPage) error {
	res := dbConn(ctx, r.db).Model(&domain.OCRJobPage{}).
		Where("job_id = ? AND page_num = ?", page.JobID, page.PageNum).
		Select("words", "text", "mean_conf", "low_conf_words", "status", "updated_at").
		Updates(map[string]any{
			"words":          jsonArrayOrEmpty(page.Words),
			"text":           page.Text,
			"mean_conf":      page.MeanConf,
			"low_conf_words": page.LowConfWords,
			"status":         page.Status,
			"updated_at":     time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SavePageWords chốt kết quả OCR của một trang.
func (r *OCRJobRepository) SavePageWords(ctx context.Context, jobID int64, pageNum int,
	words, text string, meanConf float64, lowConf int) error {
	return dbConn(ctx, r.db).Model(&domain.OCRJobPage{}).
		Where("job_id = ? AND page_num = ?", jobID, pageNum).
		Updates(map[string]any{
			"words":          jsonArrayOrEmpty(words),
			"text":           text,
			"mean_conf":      meanConf,
			"low_conf_words": lowConf,
			"status":         domain.OCRPageDone,
			// Xong là xoá dấu vết lần hỏng trước — nếu không trang này sẽ vẫn
			// mang last_error của cú OCR thất bại ở lần thử 1.
			"last_error":    "",
			"attempts":      0,
			"next_retry_at": time.Now(),
			"updated_at":    time.Now(),
		}).Error
}

// FailPage hẹn lại trang hỏng, hoặc chốt failed khi đã hết lượt.
//
// CẤU TRÚC "đọc attempts rồi mới ghi" là có chủ ý: để hàm thuần suy được
// "lần thử thứ mấy thì chết" mà không phải cài CASE WHEN vào SQL, và vì trang
// này ĐANG do worker này giữ lease nên không ai sửa attempts xen giữa hai câu.
func (r *OCRJobRepository) FailPage(ctx context.Context, jobID int64, pageNum int, lastErr string,
	maxAttempts int, backoff time.Duration) (string, error) {
	status := domain.OCRPagePending
	err := dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		var page domain.OCRJobPage
		if err := tx.First(&page, "job_id = ? AND page_num = ?", jobID, pageNum).Error; err != nil {
			return err
		}
		next := page.Attempts + 1
		retryAt := time.Now().Add(backoff)
		if next >= maxAttempts {
			status = domain.OCRPageFailed
			// Trang hết lượt thì đứng tại chỗ chờ người dùng bấm Thử lại, không
			// hẹn giờ — hẹn là hàng đợi tự chạy lại một việc đã chắc chắn hỏng.
			retryAt = time.Now()
		}
		return tx.Model(&domain.OCRJobPage{}).
			Where("job_id = ? AND page_num = ?", jobID, pageNum).
			Updates(map[string]any{
				"attempts":      next,
				"last_error":    lastErr,
				"status":        status,
				"next_retry_at": retryAt,
				"updated_at":    time.Now(),
			}).Error
	})
	if err != nil {
		return "", err
	}
	return status, nil
}

// ResetPage đưa trang về pending cho nút "Thử lại".
//
// Điều kiện `status <> processing` là phần quan trọng: trang đang có worker giữ
// lease mà bị reset thì hai worker cùng OCR một trang, trang sau ghi đè trang
// trước và kết quả là words của trang không khớp ảnh nữa.
func (r *OCRJobRepository) ResetPage(ctx context.Context, jobID int64, pageNum int) error {
	res := dbConn(ctx, r.db).Model(&domain.OCRJobPage{}).
		Where("job_id = ? AND page_num = ? AND status <> ?", jobID, pageNum, domain.OCRPageProcessing).
		Updates(map[string]any{
			"status":        domain.OCRPagePending,
			"attempts":      0,
			"last_error":    "",
			"next_retry_at": time.Now(),
			"updated_at":    time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Progress đếm trang theo trạng thái trong ĐÚNG MỘT câu.
//
// VÌ SAO FILTER CHỨ KHÔNG NHIỀU CÂU COUNT: năm câu count là năm lần quét index
// job_id và năm lần thấy một hàng đợi đang thay đổi — tổng với phần cộng có thể
// không khớp nhau ngay trong một lượt đọc tiến độ.
func (r *OCRJobRepository) Progress(ctx context.Context, jobID int64) (*domain.ScanJobProgress, error) {
	return countPages(dbConn(ctx, r.db), jobID)
}

func countPages(q *gorm.DB, jobID int64) (*domain.ScanJobProgress, error) {
	var p domain.ScanJobProgress
	err := q.Model(&domain.OCRJobPage{}).
		Select(`count(*) AS total,
			count(*) FILTER (WHERE status IN (?, ?, ?)) AS done,
			count(*) FILTER (WHERE status = ?) AS failed,
			count(*) FILTER (WHERE status = ?) AS pending,
			count(*) FILTER (WHERE status = ?) AS running`,
			domain.OCRPageDone, domain.OCRPageEdited, domain.OCRPageSkippedHasText,
			domain.OCRPageFailed, domain.OCRPagePending, domain.OCRPageProcessing).
		Where("job_id = ?", jobID).Scan(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SyncCounts chép số đếm vào cột của job để GET tiến độ là một cú đọc cột, không
// phải một cú count lại toàn bộ trang mỗi giây (FE poll 1s).
func (r *OCRJobRepository) SyncCounts(ctx context.Context, jobID int64) (*domain.ScanJobProgress, error) {
	var out *domain.ScanJobProgress
	err := dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		p, err := countPages(tx, jobID)
		if err != nil {
			return err
		}
		out = p
		return tx.Model(&domain.OCRJob{}).
			// Job đã cancelled thì KHÔNG được hồi sinh: người dùng huỷ là ý cuối
			// cùng, trang dở dang còn lại do trang ấy tự chốt.
			Where("id = ? AND status <> ?", jobID, domain.OCRJobCancelled).
			Updates(map[string]any{
				"total_pages":  p.Total,
				"done_pages":   p.Done,
				"failed_pages": p.Failed,
				"status":       domain.OCRJobStatusFromProgress(*p),
				"updated_at":   time.Now(),
			}).Error
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *OCRJobRepository) MarkRunning(ctx context.Context, jobID int64, lease time.Duration) error {
	return dbConn(ctx, r.db).Model(&domain.OCRJob{}).Where("id = ?", jobID).
		Updates(map[string]any{
			"status":        domain.QueueProcessing,
			"next_retry_at": time.Now().Add(lease),
			"updated_at":    time.Now(),
		}).Error
}

// MarkDone chốt PDF đã dựng xong. Điều kiện `status <> cancelled` giữ đúng lời
// hứa với người dùng đã bấm Huỷ: một worker về muộn không được phép biến job đã
// huỷ thành done rồi ghi output lên kho tạm.
func (r *OCRJobRepository) MarkDone(ctx context.Context, jobID int64, outputKey string) error {
	return dbConn(ctx, r.db).Model(&domain.OCRJob{}).
		Where("id = ? AND status <> ?", jobID, domain.OCRJobCancelled).
		Updates(map[string]any{
			"status":        domain.QueueDone,
			"output_key":    outputKey,
			"error_code":    "",
			"error_message": "",
			"updated_at":    time.Now(),
		}).Error
}

func (r *OCRJobRepository) MarkFailed(ctx context.Context, jobID int64, code, message string) error {
	return dbConn(ctx, r.db).Model(&domain.OCRJob{}).Where("id = ?", jobID).
		Updates(map[string]any{
			"status":        domain.QueueFailed,
			"error_code":    code,
			"error_message": message,
			"updated_at":    time.Now(),
		}).Error
}

// ClaimPages là đơn vị claim thật của tính năng: nhận lô trang tới hạn trên toàn
// bộ job chưa huỷ, khoá SKIP LOCKED rồi hẹn lease ngay trong cùng transaction —
// cùng khuôn ClaimExtractPending của articles.
func (r *OCRJobRepository) ClaimPages(ctx context.Context, limit int, lease time.Duration) ([]*domain.ClaimedPage, error) {
	if limit <= 0 {
		limit = constants.SCAN_CLAIM_PAGES
	}
	now := time.Now()
	pages := make([]*domain.OCRJobPage, 0, limit)
	byJob := make(map[int64][]int, limit)
	err := dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		if err := tx.Table("ocr_job_pages p").Select("p.*").
			Joins("JOIN ocr_jobs j ON j.id = p.job_id AND j.deleted_at IS NULL").
			Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "p"}, Options: "SKIP LOCKED"}).
			Where("p.status = ? AND p.next_retry_at <= ?", domain.OCRPagePending, now).
			Where("j.status IN ?", []string{domain.QueuePending, domain.QueueProcessing}).
			Order("p.job_id ASC, p.page_num ASC").
			Limit(limit).Find(&pages).Error; err != nil {
			return err
		}
		if len(pages) == 0 {
			return nil
		}
		return lockClaimedPages(tx, pages, byJob, now, lease)
	})
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, nil
	}
	return attachJobs(dbConn(ctx, r.db), pages, byJob)
}

// lockClaimedPages đánh dấu lô trang vừa SELECT là đang chạy và nhắc lease của
// job tương ứng. Tách ra vì ClaimPages đã sát trần ngân sách hàm; byJob là
// tham chiếu map nên caller thấy kết quả ngay.
func lockClaimedPages(tx *gorm.DB, pages []*domain.OCRJobPage, byJob map[int64][]int,
	now time.Time, lease time.Duration) error {
	for _, p := range pages {
		byJob[p.JobID] = append(byJob[p.JobID], p.PageNum)
		// DB đã chuyển sang processing — bản đưa cho caller cũng phải nói thật,
		// nếu không worker đọc cp.Page.Status sẽ thấy "pending" của một trang mà
		// chính nó đang giữ lease.
		p.Status = domain.OCRPageProcessing
		p.NextRetryAt = now.Add(lease)
	}
	for jobID, nums := range byJob {
		// UPDATE theo (job, trang) của CHÍNH job này — không có id trang nên cặp
		// khoá là đường ghi duy nhất.
		if err := tx.Model(&domain.OCRJobPage{}).
			Where("job_id = ? AND page_num IN ?", jobID, nums).
			Updates(map[string]any{
				"status":        domain.OCRPageProcessing,
				"next_retry_at": now.Add(lease),
				"updated_at":    now,
			}).Error; err != nil {
			return err
		}
	}
	return tx.Model(&domain.OCRJob{}).Where("id IN ?", jobIDsOf(byJob)).
		Updates(map[string]any{
			// pending → processing; job đang processing thì chỉ gia hạn lease.
			"status":        gorm.Expr("CASE WHEN status = ? THEN ? ELSE status END", domain.QueuePending, domain.QueueProcessing),
			"next_retry_at": now.Add(lease),
			"updated_at":    now,
		}).Error
}

func jobIDsOf(m map[int64][]int) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// attachJobs nạp job của các trang vừa claim (KHÔNG khoá: settings của job là
// bất biến, chỉ status/lease mới đổi — và thứ đó đã đổi ở trên).
func attachJobs(q *gorm.DB, pages []*domain.OCRJobPage, byJob map[int64][]int) ([]*domain.ClaimedPage, error) {
	var jobs []*domain.OCRJob
	if err := q.Where("id IN ?", jobIDsOf(byJob)).Find(&jobs).Error; err != nil {
		return nil, err
	}
	byID := make(map[int64]*domain.OCRJob, len(jobs))
	for _, j := range jobs {
		byID[j.ID] = j
	}
	out := make([]*domain.ClaimedPage, 0, len(pages))
	for _, p := range pages {
		job := byID[p.JobID]
		if job == nil {
			// Hàng đợi chỉ claim trang của job còn sống (điều kiện join ở câu
			// SELECT), nên đây là lúc job bị xoá cứng đúng giữa lúc claim — bỏ
			// qua trang, worker sau sẽ thấy nó pending trở lại khi lease hết hạn.
			continue
		}
		out = append(out, &domain.ClaimedPage{Job: job, Page: p})
	}
	return out, nil
}

// ReleaseUnfinished trả mọi trang đang giữ lease về pending khi worker tắt —
// cùng lý do với ReleaseExtractClaims: một cú Ctrl+C không được phép khiến 40
// trang nằm bất động đến hết lease.
// ListReadyToBuild trả các job đang pending/processing mà KHÔNG còn trang
// pending/processing — ví dụ PDF đã có text layer (mọi trang skipped). Worker
// dùng để dựng PDF và chốt done dù không có trang nào phải OCR.
func (r *OCRJobRepository) ListReadyToBuild(ctx context.Context) ([]*domain.OCRJob, error) {
	jobs := make([]*domain.OCRJob, 0)
	err := dbConn(ctx, r.db).Where("deleted_at IS NULL").
		Where("status IN ?", []string{domain.QueuePending, domain.QueueProcessing}).
		Where("NOT EXISTS (SELECT 1 FROM ocr_job_pages p WHERE p.job_id = ocr_jobs.id AND p.status IN ?)",
			[]string{domain.OCRPagePending, domain.OCRPageProcessing}).
		Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *OCRJobRepository) ReleaseUnfinished(ctx context.Context) (int64, error) {
	res := dbConn(ctx, r.db).Model(&domain.OCRJobPage{}).
		Where("status = ?", domain.OCRPageProcessing).
		Updates(map[string]any{
			"status":        domain.OCRPagePending,
			"next_retry_at": time.Now(),
			"updated_at":    time.Now(),
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (r *OCRJobRepository) Cancel(ctx context.Context, jobID, userID int64) error {
	return dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		res := tx.Model(&domain.OCRJob{}).
			Where("id = ? AND user_id = ? AND status IN ?", jobID, userID,
				[]string{domain.QueuePending, domain.QueueProcessing}).
			Updates(map[string]any{"status": domain.OCRJobCancelled, "updated_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		// Chỉ thu hồi trang CHƯA chạy; trang đang processing để worker tự cắt
		// theo ctx, ghi đè status của nó lúc ấy là nói dối rằng trang đã huỷ.
		return tx.Model(&domain.OCRJobPage{}).
			Where("job_id = ? AND status = ?", jobID, domain.OCRPagePending).
			Updates(map[string]any{"status": domain.OCRPageCancelled, "updated_at": time.Now()}).Error
	})
}

// Delete xoá job của đúng chủ. Trả bản ghi để service xoá object tương ứng.
func (r *OCRJobRepository) Delete(ctx context.Context, jobID, userID int64) (*domain.OCRJob, error) {
	var job domain.OCRJob
	err := dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		if err := tx.First(&job, "id = ? AND user_id = ?", jobID, userID).Error; err != nil {
			if stderrors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		if err := tx.Where("job_id = ?", jobID).Delete(&domain.OCRJobPage{}).Error; err != nil {
			return err
		}
		return tx.Delete(&job).Error
	})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// ListExpired là đường của janitor: KHÔNG lọc user (hệ thống dọn cho mọi người),
// và lấy theo expires_at chứ không theo updated_at — một job done từ tuần trước mà
// không ai đụng vẫn phải hết hạn đúng lúc.
func (r *OCRJobRepository) ListExpired(ctx context.Context, now time.Time, limit int) ([]*domain.OCRJob, error) {
	if limit <= 0 {
		limit = constants.SCAN_JANITOR_BATCH
	}
	jobs := make([]*domain.OCRJob, 0, limit)
	err := dbConn(ctx, r.db).Where("expires_at <= ?", now).
		Order("expires_at ASC").Limit(limit).Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

// DeleteIDs xoá CỨNG (Unscoped) theo id do janitor lấy từ ListExpired.
//
// VÌ SAO KHÔNG xoá mềm như đường của người dùng: janitor chạy mỗi giờ một lần,
// giữ lại row "đã xoá" thì bảng chỉ phình thêm mà chẳng ai đọc — và object đã mất
// thì row có cũng không mở lại được.
func (r *OCRJobRepository) DeleteIDs(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var removed int64
	err := dbConn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		if err := tx.Where("job_id IN ?", ids).Delete(&domain.OCRJobPage{}).Error; err != nil {
			return err
		}
		res := tx.Unscoped().Where("id IN ?", ids).Delete(&domain.OCRJob{})
		removed = res.RowsAffected
		return res.Error
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

// MarkPushed ghi bài kho sinh ra từ job và đổi TTL sang hạn "đã push" — expiresAt
// do service tính (constants.SCAN_PUSHED_TTL hoặc cấu hình).
func (r *OCRJobRepository) MarkPushed(ctx context.Context, jobID, userID, articleID int64, expiresAt time.Time) error {
	res := dbConn(ctx, r.db).Model(&domain.OCRJob{}).
		Where("id = ? AND user_id = ?", jobID, userID).
		Updates(map[string]any{
			"pushed_at":         time.Now(),
			"pushed_article_id": articleID,
			"expires_at":        expiresAt,
			"updated_at":        time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *OCRJobRepository) SetExpiresAt(ctx context.Context, jobID, userID int64, expiresAt time.Time) error {
	res := dbConn(ctx, r.db).Model(&domain.OCRJob{}).
		Where("id = ? AND user_id = ?", jobID, userID).
		Update("expires_at", expiresAt)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// jsonArrayOrEmpty chặn cái bẫy jsonb: cột NOT NULL mà nhận chuỗi rỗng thì
// Postgres báo "invalid input syntax for type json" và cả cú ghi trang hỏng vì
// một trang OCR ra 0 từ (trang trắng — chuyện hoàn toàn bình thường).
func jsonArrayOrEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return domain.OCRWordsEmpty
	}
	return s
}
