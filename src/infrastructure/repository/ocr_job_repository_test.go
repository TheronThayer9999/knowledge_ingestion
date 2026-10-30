package repository

import (
	"context"
	stderrors "errors"
	"slices"
	"testing"
	"time"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/domain"
)

// mkJob dựng job của một chủ độc quyền cho test; trang 1..n ở trạng thái pending
// tới hạn (next_retry_at = quá khứ) để claim được ngay.
func mkJob(userID int64, pageCount int) (*domain.OCRJob, []*domain.OCRJobPage) {
	now := time.Now().Add(-time.Minute)
	job := &domain.OCRJob{
		UserID:      userID,
		SourceName:  "quet.png",
		SourceKey:   constants.SCAN_OBJECT_PREFIX + "probe/source.png",
		Status:      domain.QueuePending,
		TotalPages:  pageCount,
		Lang:        "vie",
		DPI:         300,
		PSM:         6,
		Engine:      "tesseract",
		NextRetryAt: now,
		ExpiresAt:   now.Add(constants.SCAN_TMP_TTL),
	}
	pages := make([]*domain.OCRJobPage, 0, pageCount)
	for i := 1; i <= pageCount; i++ {
		pages = append(pages, &domain.OCRJobPage{
			PageNum:     i,
			Status:      domain.OCRPagePending,
			NextRetryAt: now,
		})
	}
	return job, pages
}

// TestJSONArrayList chống cái bẫy jsonb: trang OCR ra 0 từ (trang trắng) có
// words rỗng — ghi nguyên chuỗi "" vào cột jsonb NOT NULL là Postgres từ chối cả
// cú ghi, một trang vô nghĩa làm hỏng cả job.
func TestJSONArrayList(t *testing.T) {
	for _, in := range []string{"", "  ", "\t"} {
		if got := jsonArrayOrEmpty(in); got != "[]" {
			t.Fatalf("jsonArrayOrEmpty(%q) = %q, muốn \"[]\"", in, got)
		}
	}
	if got := jsonArrayOrEmpty(`[{"text":"hồ"}]`); got != `[{"text":"hồ"}]` {
		t.Fatalf("chuỗi json thật bị sửa đổi: %q", got)
	}
}

// TestOCRJobRepositoryClaimProgressAndDone đi hết vòng đời một job: tạo → claim
// hai lô → ghi kết quả → chốt số đếm.
func TestOCRJobRepositoryClaimProgressAndDone(t *testing.T) {
	db := newPageIssuesTestDB(t)
	repo := NewOCRJobRepository(db)
	ctx := context.Background()

	userID := time.Now().Unix()
	job, pages := mkJob(userID, 3)
	if err := repo.Create(ctx, job, pages); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("job_id = ?", job.ID).Delete(&domain.OCRJobPage{})
		db.Unscoped().Delete(&domain.OCRJob{}, job.ID)
	})

	// Claim lô 2: đúng 2 trang, đánh processing + hẹn lease, job sang processing.
	first, err := repo.ClaimPages(ctx, 2, constants.SCAN_PAGE_LEASE)
	if err != nil {
		t.Fatalf("ClaimPages: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("lô đầu phải 2 trang, nhận %d", len(first))
	}
	for _, cp := range first {
		if cp.Job == nil || cp.Job.UserID != userID {
			t.Fatalf("ClaimedPage thiếu job đúng chủ: %+v", cp)
		}
		if cp.Page.Status != domain.OCRPageProcessing {
			t.Fatalf("trang %d = %q, muốn processing", cp.Page.PageNum, cp.Page.Status)
		}
	}

	// Claim lần hai CHỈ còn trang 3: hai trang trên đã bị giữ lease nên không
	// được xuất hiện lại — đó là toàn bộ ý nghĩa của SKIP LOCKED + lease.
	second, err := repo.ClaimPages(ctx, 4, constants.SCAN_PAGE_LEASE)
	if err != nil {
		t.Fatalf("ClaimPages lượt 2: %v", err)
	}
	for _, cp := range second {
		if cp.Page.JobID != job.ID {
			continue // trang của test khác chạy song song
		}
		if cp.Page.PageNum != 3 {
			t.Fatalf("claim lại trang %d của job đang chạy", cp.Page.PageNum)
		}
	}

	for i := 1; i <= 3; i++ {
		// words = "" là trang OCR ra 0 từ (trang trắng) — repo phải dịch sang
		// "[]" chứ không đẩy jsonb lỗi "invalid input syntax".
		if err := repo.SavePageWords(ctx, job.ID, i, "", "", 91.5, 2); err != nil {
			t.Fatalf("SavePageWords trang %d: %v", i, err)
		}
	}
	row, err := repo.GetPage(ctx, job.ID, 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if row.Words != "[]" {
		t.Fatalf("words = %q, muốn \"[]\"", row.Words)
	}
	if row.Status != domain.OCRPageDone || row.LastError != "" {
		t.Fatalf("trang 1 = %+v, muốn ocr_done sạch lỗi", row)
	}

	p, err := repo.SyncCounts(ctx, job.ID)
	if err != nil {
		t.Fatalf("SyncCounts: %v", err)
	}
	if p.Total != 3 || p.Done != 3 || p.Pending != 0 {
		t.Fatalf("progress = %+v, muốn 3/3 done", p)
	}
	back, err := repo.Get(ctx, job.ID, userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if back.Status != domain.QueueDone || back.DonePages != 3 {
		t.Fatalf("job = %q done_pages=%d, muốn done/3", back.Status, back.DonePages)
	}

	// Get theo chủ KHÁC phải như không tồn tại — mọi endpoint quét đều scope
	// theo user_id ở tầng SQL.
	if _, err := repo.Get(ctx, job.ID, userID+1); !stderrors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get sai chủ = %v, muốn domain.ErrNotFound", err)
	}
}

func TestOCRJobRepositoryFailPageThenReset(t *testing.T) {
	db := newPageIssuesTestDB(t)
	repo := NewOCRJobRepository(db)
	ctx := context.Background()

	userID := time.Now().Unix()
	job, pages := mkJob(userID, 1)
	if err := repo.Create(ctx, job, pages); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("job_id = ?", job.ID).Delete(&domain.OCRJobPage{})
		db.Unscoped().Delete(&domain.OCRJob{}, job.ID)
	})

	// maxAttempts 3: hai lần đầu còn hẹn lại, lần ba mới thành failed.
	for i, want := range []string{domain.OCRPagePending, domain.OCRPagePending, domain.OCRPageFailed} {
		status, err := repo.FailPage(ctx, job.ID, 1, "engine exit 1", 3, 200*time.Millisecond)
		if err != nil {
			t.Fatalf("FailPage lần %d: %v", i+1, err)
		}
		if status != want {
			t.Fatalf("lần thử %d = %q, muốn %q", i+1, status, want)
		}
	}
	row, err := repo.GetPage(ctx, job.ID, 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if row.Attempts != 3 || row.LastError != "engine exit 1" {
		t.Fatalf("attempts/last_error = %d/%q, muốn 3/\"engine exit 1\"", row.Attempts, row.LastError)
	}

	// Trang failed thì ResetPage mở đường cho nút "Thử lại".
	if err := repo.ResetPage(ctx, job.ID, 1); err != nil {
		t.Fatalf("ResetPage: %v", err)
	}
	row, err = repo.GetPage(ctx, job.ID, 1)
	if err != nil {
		t.Fatalf("GetPage sau reset: %v", err)
	}
	if row.Status != domain.OCRPagePending || row.Attempts != 0 {
		t.Fatalf("sau reset = %q attempts=%d, muốn pending/0", row.Status, row.Attempts)
	}

	// Trang ĐANG chạy thì reset phải bị từ chối: ghi đè lease của worker khác là
	// hai engine cùng OCR một trang rồi trang sau đè trang trước.
	claimed, err := repo.ClaimPages(ctx, 1, constants.SCAN_PAGE_LEASE)
	if err != nil {
		t.Fatalf("ClaimPages: %v", err)
	}
	found := false
	for _, cp := range claimed {
		if cp.Page.JobID == job.ID {
			found = true
		}
	}
	if !found {
		t.Skip("trang của job này chưa tới hạn backoff — bỏ qua phần chống reset trang đang chạy")
	}
	if err := repo.ResetPage(ctx, job.ID, 1); !stderrors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ResetPage trang đang chạy = %v, muốn ErrNotFound", err)
	}
}

func TestOCRJobRepositoryCancelStopsClaimAndSync(t *testing.T) {
	db := newPageIssuesTestDB(t)
	repo := NewOCRJobRepository(db)
	ctx := context.Background()

	userID := time.Now().Unix()
	job, pages := mkJob(userID, 2)
	if err := repo.Create(ctx, job, pages); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("job_id = ?", job.ID).Delete(&domain.OCRJobPage{})
		db.Unscoped().Delete(&domain.OCRJob{}, job.ID)
	})

	if err := repo.Cancel(ctx, job.ID, userID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// Huỷ rồi thì KHÔNG claim nhầm trang của job bị huỷ — người dùng bấm Huỷ mà
	// vẫn thấy trang mới chạy là mất niềm tin vào nút đó.
	claimed, err := repo.ClaimPages(ctx, 4, constants.SCAN_PAGE_LEASE)
	if err != nil {
		t.Fatalf("ClaimPages sau huỷ: %v", err)
	}
	for _, cp := range claimed {
		if cp.Page.JobID == job.ID {
			t.Fatalf("job đã huỷ vẫn được claim (trang %d)", cp.Page.PageNum)
		}
	}
	rows, err := repo.ListPages(ctx, job.ID, 1, 10)
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	for _, p := range rows {
		if p.Status != domain.OCRPageCancelled {
			t.Fatalf("trang %d = %q, muốn cancelled", p.PageNum, p.Status)
		}
	}

	// SyncCounts không được "hồi sinh" job đã huỷ thành done.
	if _, err := repo.SyncCounts(ctx, job.ID); err != nil {
		t.Fatalf("SyncCounts: %v", err)
	}
	back, err := repo.Get(ctx, job.ID, userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if back.Status != domain.OCRJobCancelled {
		t.Fatalf("job = %q, muốn giữ nguyên cancelled", back.Status)
	}

	// Huỷ job của người khác / đã done → ErrNotFound để service trả 404/409.
	if err := repo.Cancel(ctx, job.ID, userID+1); !stderrors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Cancel sai chủ = %v, muốn ErrNotFound", err)
	}
}

func TestOCRJobRepositoryExpiredJanitorPath(t *testing.T) {
	db := newPageIssuesTestDB(t)
	repo := NewOCRJobRepository(db)
	ctx := context.Background()

	userID := time.Now().Unix()
	job, pages := mkJob(userID, 1)
	job.ExpiresAt = time.Now().Add(-time.Hour)
	if err := repo.Create(ctx, job, pages); err != nil {
		t.Fatalf("Create: %v", err)
	}

	expired, err := repo.ListExpired(ctx, time.Now(), 200)
	if err != nil {
		t.Fatalf("ListExpired: %v", err)
	}
	var ids []int64
	for _, j := range expired {
		ids = append(ids, j.ID)
	}
	if !slices.Contains(ids, job.ID) {
		t.Fatal("job quá hạn không xuất hiện trong ListExpired")
	}
	removed, err := repo.DeleteIDs(ctx, []int64{job.ID})
	if err != nil {
		t.Fatalf("DeleteIDs: %v", err)
	}
	if removed != 1 {
		t.Fatalf("DeleteIDs = %d, muốn 1", removed)
	}
	// Xoá cứng rồi thì đọc lại phải không thấy, kể cả theo đúng chủ.
	if _, err := repo.Get(ctx, job.ID, userID); !stderrors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get sau xoá = %v, muốn ErrNotFound", err)
	}
	var left int64
	if err := db.Model(&domain.OCRJobPage{}).Where("job_id = ?", job.ID).Count(&left).Error; err != nil {
		t.Fatalf("Count trang: %v", err)
	}
	// FK ON DELETE CASCADE là điều kiện để janitor chỉ cần xoá job.
	if left != 0 {
		t.Fatalf("còn %d trang mồ côi — cascade không chạy", left)
	}
}
