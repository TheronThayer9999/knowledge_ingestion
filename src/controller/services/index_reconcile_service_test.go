package services

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/domain"
)

// TestReconcileGroupsResetByOwner khoá cách bộ reconcile chữa bài ma: dùng lại
// đúng ResetQueue của admin (không có câu SQL "bỏ qua chủ" nào), nên mỗi chủ
// MỘT lô — userID lấy từ chính row đang xét.
//
// Bài IndexDoneChunks = 0 phải bị bỏ qua: "done 0 chunk" là bài rỗng chữ, không
// phải bài mất vector, requeue nó chỉ đốt embed vô ích.
func TestReconcileGroupsResetByOwner(t *testing.T) {
	arts := &reconcileStubArticles{pages: map[int][]*domain.Article{
		0: {
			doneArticle(1, 3, 5),
			doneArticle(2, 6, 7),
			doneArticle(3, 3, 0), // bài rỗng: không phải ma
			doneArticle(4, 3, 9), // vẫn có vector: không phải ma
		},
	}}
	vec := &indexStubVectors{countByArticle: func(id int64) (int64, error) {
		if id == 4 {
			return 9, nil
		}
		return 0, nil
	}}
	svc := NewIndexReconcileService(arts, vec)

	n, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile lỗi: %v", err)
	}
	if n != 2 {
		t.Errorf("requeued = %d, muốn 2 (bài 1 và 2)", n)
	}
	got := arts.resetTrace()
	want := []string{"user=3 ids=[1]", "user=6 ids=[2]"}
	if !slices.Equal(got, want) {
		t.Errorf("trace reset = %v, muốn %v (gom theo chủ, 1 UPDATE mỗi chủ)", got, want)
	}
}

// TestReconcileCountErrorDoesNotRequeue khoá chiều an toàn: Qdrant chập chờn
// (đếm lỗi) thì KHÔNG được requeue — requeue oan nghĩa là embed lại toàn bộ bài
// đang yên ổn. Bằng chứng để chữa phải là "collection trả về 0 point".
func TestReconcileCountErrorDoesNotRequeue(t *testing.T) {
	arts := &reconcileStubArticles{pages: map[int][]*domain.Article{
		0: {doneArticle(1, 3, 5)},
	}}
	vec := &indexStubVectors{countByArticle: func(int64) (int64, error) {
		return 0, fmt.Errorf("qdrant: unavailable")
	}}
	svc := NewIndexReconcileService(arts, vec)

	report, err := svc.ScanGhosts(context.Background())
	if err != nil {
		t.Fatalf("ScanGhosts lỗi: %v", err)
	}
	if report.Scanned != 1 || len(report.Ghosts) != 0 {
		t.Errorf("scanned=%d ghosts=%d, muốn 1/0 — đếm lỗi mà coi là mất vector",
			report.Scanned, len(report.Ghosts))
	}
	if len(arts.resets) != 0 {
		t.Errorf("đếm lỗi mà vẫn requeue (%v) — requeue oan", arts.resetTrace())
	}
}

// TestScanGhostsStopsAtCap khoá trần RECONCILE_MAX_ARTICLES: mỗi bài là 1 câu
// Count gửi lên Qdrant, kho triệu bài thì một lượt quét không được biến thành
// triệu round trip. Quét chưa hết PHẢI báo truncated để người đọc không kết luận
// "sạch" từ một lượt cắt cụt.
func TestScanGhostsStopsAtCap(t *testing.T) {
	arts := &reconcileStubArticles{pages: synthDonePages(constants.RECONCILE_MAX_ARTICLES + 10)}
	vec := &indexStubVectors{countByArticle: func(int64) (int64, error) { return 0, nil }}
	svc := NewIndexReconcileService(arts, vec)

	report, err := svc.ScanGhosts(context.Background())
	if err != nil {
		t.Fatalf("ScanGhosts lỗi: %v", err)
	}
	if report.Scanned != constants.RECONCILE_MAX_ARTICLES {
		t.Errorf("scanned = %d, muốn đúng trần %d", report.Scanned, constants.RECONCILE_MAX_ARTICLES)
	}
	if !report.Truncated {
		t.Error("quét chưa hết mà không báo truncated — endpoint sẽ nói nhầm là sạch")
	}
	if len(report.Ghosts) != constants.RECONCILE_MAX_ARTICLES {
		t.Errorf("ghosts = %d, muốn %d", len(report.Ghosts), constants.RECONCILE_MAX_ARTICLES)
	}
	if len(arts.calls) > constants.RECONCILE_MAX_ARTICLES/constants.RECONCILE_PAGE_SIZE {
		t.Errorf("gọi DB %d lần, vượt số lô cần thiết", len(arts.calls))
	}
}

// TestScanGhostsReportsTotals khoá hai con số để so: tổng point trên Qdrant và
// tổng chunk DB claim — chính là phép thử "DB nói 45533 chunk, kho có 17299
// point" mà mắt thường phải tìm ra bằng tay trước khi có endpoint này.
func TestScanGhostsReportsTotals(t *testing.T) {
	arts := &reconcileStubArticles{pages: map[int][]*domain.Article{
		0: {doneArticle(1, 3, 5), doneArticle(2, 3, 7)},
	}}
	vec := &indexStubVectors{carried: 12}
	svc := NewIndexReconcileService(arts, vec)

	report, err := svc.ScanGhosts(context.Background())
	if err != nil {
		t.Fatalf("ScanGhosts lỗi: %v", err)
	}
	if report.Points != 12 || report.Claimed != 12 {
		t.Errorf("points=%d claimed=%d, muốn 12/12 (kho đủ nên không có ma)",
			report.Points, report.Claimed)
	}
	if len(report.Ghosts) != 0 {
		t.Errorf("vector còn đủ mà báo %d bài ma — báo nhầm sẽ requeue cả kho", len(report.Ghosts))
	}
}

// ---- stub ----

func doneArticle(id, userID int64, chunks int) *domain.Article {
	return &domain.Article{
		BaseModel:       domain.BaseModel{ID: id},
		UserID:          userID,
		IndexStatus:     domain.QueueDone,
		IndexDoneChunks: chunks,
	}
}

// synthDonePages dựng bài done giả theo đúng lô RECONCILE_PAGE_SIZE để test
// trần quét — id tăng dần để assert vẫn hội tụ khi chạy lại.
func synthDonePages(total int) map[int][]*domain.Article {
	pages := make(map[int][]*domain.Article, total/constants.RECONCILE_PAGE_SIZE+1)
	for offset := 0; offset < total; offset += constants.RECONCILE_PAGE_SIZE {
		batch := make([]*domain.Article, 0, constants.RECONCILE_PAGE_SIZE)
		for i := offset; i < offset+constants.RECONCILE_PAGE_SIZE && i < total; i++ {
			batch = append(batch, doneArticle(int64(i)+1, int64(i)%3+1, 4))
		}
		pages[offset] = batch
	}
	return pages
}

// reconcileStubArticles là vai domain.ArticleAdminQueue — chỉ 2 phương thức
// reconcile thật sự gọi có hành vi, phần còn lại trả rỗng.
type reconcileStubArticles struct {
	pages  map[int][]*domain.Article
	calls  []int
	resets []reconcileReset
}

type reconcileReset struct {
	userID int64
	ids    []int64
}

// resetTrace vẽ các lần ResetQueue thành chuỗi đã sắp xếp — map owner trong
// service duyệt theo thứ tự không xác định nên phải sắp mới so được.
func (s *reconcileStubArticles) resetTrace() []string {
	out := make([]string, 0, len(s.resets))
	for _, r := range s.resets {
		ids := slices.Clone(r.ids)
		slices.Sort(ids)
		out = append(out, fmt.Sprintf("user=%d ids=%v", r.userID, ids))
	}
	slices.Sort(out)
	return out
}

func (s *reconcileStubArticles) ListByIndexStatus(_ context.Context, status string, limit, offset int) ([]*domain.Article, error) {
	if status != domain.QueueDone {
		return nil, fmt.Errorf("reconcile chỉ được hỏi bài done, nhận %q", status)
	}
	s.calls = append(s.calls, offset)
	batch := s.pages[offset]
	if limit > 0 && len(batch) > limit {
		batch = batch[:limit]
	}
	return batch, nil
}

func (s *reconcileStubArticles) ResetQueue(_ context.Context, ids []int64, userID int64) (int64, error) {
	s.resets = append(s.resets, reconcileReset{userID: userID, ids: slices.Clone(ids)})
	return int64(len(ids)), nil
}

func (s *reconcileStubArticles) IDsByCategory(context.Context, int64, int64) ([]int64, error) {
	return nil, nil
}
func (s *reconcileStubArticles) ForeignIDs(context.Context, []int64, int64) ([]int64, error) {
	return nil, nil
}
func (s *reconcileStubArticles) ResetExtract(context.Context, []int64, int64) (int64, error) {
	return 0, nil
}
func (s *reconcileStubArticles) QueueStats(context.Context) (*domain.QueueStats, error) {
	return &domain.QueueStats{}, nil
}
func (s *reconcileStubArticles) ListByExtractStatus(context.Context, string, int, int) ([]*domain.Article, error) {
	return nil, nil
}
func (s *reconcileStubArticles) ListSoftDeleted(context.Context, time.Time, int) ([]*domain.Article, error) {
	return nil, nil
}
func (s *reconcileStubArticles) ListSoftDeletedSince(context.Context, time.Time, int) ([]*domain.Article, error) {
	return nil, nil
}

var _ domain.ArticleAdminQueue = (*reconcileStubArticles)(nil)
