package services

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
)

// TestAdminResetExtractDeletesPagesBeforeStatus khoá thứ tự bắt buộc của
// admin.ResetExtract: xoá article_pages TRƯỚC, đổi extract_status SAU.
//
// Đảo thứ tự sinh cửa sổ "trang cũ còn, extract đã pending" — worker đọc thấy
// đã có trang nên bỏ qua OCR và đánh done với nội dung cũ, tức là lệnh reset
// không trích lại gì cả mà lại làm hỏng tiến trình.
func TestAdminResetExtractDeletesPagesBeforeStatus(t *testing.T) {
	arts, pages := &adminStubArticles{}, &adminStubPages{}
	svc := newAdminSvc(t, arts, pages)

	res := svc.ResetExtract(context.Background(), &dtos.ResetScopeRequest{ArticleIDs: []int64{7}})
	if res.Err != nil {
		t.Fatalf("ResetExtract lỗi: %v", res.Err)
	}
	// trace là MỘT dãy chung cho cả 2 repo — đó mới là thứ chứng minh THỨ TỰ.
	want := []string{"pages.DeleteByArticles(7)", "articles.ResetExtract(7)"}
	if len(arts.trace) != len(want) {
		t.Fatalf("trace = %v, want %v", arts.trace, want)
	}
	for i := range want {
		if arts.trace[i] != want[i] {
			t.Errorf("bước %d = %q, want %q", i, arts.trace[i], want[i])
		}
	}
	if res.Data == nil || res.Data.ResetArticles != 1 {
		t.Errorf("response = %+v, want reset_articles = 1", res.Data)
	}
}

// TestAdminResetExtractRejectsEmptyScope khoá chặn "reset toàn bộ": không rõ
// scope thì 400, không phải xoá sạch hàng đợi cả hệ thống.
func TestAdminResetExtractRejectsEmptyScope(t *testing.T) {
	arts, pages := &adminStubArticles{}, &adminStubPages{}
	svc := newAdminSvc(t, arts, pages)

	res := svc.ResetExtract(context.Background(), &dtos.ResetScopeRequest{})
	if res.Err == nil {
		t.Fatal("scope rỗng phải báo lỗi")
	}
	if len(arts.trace) != 0 {
		t.Errorf("scope rỗng mà vẫn ghi (%v) — lọt chặn", arts.trace)
	}
}

// TestAdminResetExtractRejectsForeignIDs khoá chặn quyền: id của người khác
// thì dừng TRƯỚC khi xoá trang — nếu xoá trước rồi mới kiểm tra thì đã mất
// nội dung của người khác rồi mới trả 403.
func TestAdminResetExtractRejectsForeignIDs(t *testing.T) {
	arts, pages := &adminStubArticles{foreign: []int64{99}}, &adminStubPages{}
	svc := newAdminSvc(t, arts, pages)

	res := svc.ResetExtract(context.Background(), &dtos.ResetScopeRequest{ArticleIDs: []int64{7}})
	if res.Err == nil {
		t.Fatal("id không thuộc của mình phải báo lỗi")
	}
	if len(arts.trace) != 0 {
		t.Errorf("id người khác mà vẫn xoá/ghi (%v) — lọt chặn quyền", arts.trace)
	}
}

// ---- stub ----

// adminTrace là dãy bước dùng chung cho cả 2 stub, để test khẳng định được
// THỨ TỰ xoá-trước-đổi-sau chứ không chỉ "cuối cùng có gọi hay không".
type adminTrace []string

func (t *adminTrace) add(step string, ids []int64) {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	*t = append(*t, step+"("+strings.Join(parts, ",")+")")
}

// adminStubArticles hiện thân domain.ArticleAdminQueue (9 phương thức).
type adminStubArticles struct {
	trace   adminTrace
	foreign []int64
}

func (a *adminStubArticles) IDsByCategory(context.Context, int64, int64) ([]int64, error) {
	return []int64{7}, nil
}
func (a *adminStubArticles) ForeignIDs(context.Context, []int64, int64) ([]int64, error) {
	return a.foreign, nil
}
func (a *adminStubArticles) ResetQueue(_ context.Context, ids []int64, _ int64) (int64, error) {
	a.trace.add("articles.ResetQueue", ids)
	return int64(len(ids)), nil
}
func (a *adminStubArticles) ResetExtract(_ context.Context, ids []int64, _ int64) (int64, error) {
	a.trace.add("articles.ResetExtract", ids)
	return int64(len(ids)), nil
}
func (a *adminStubArticles) QueueStats(context.Context) (*domain.QueueStats, error) {
	return &domain.QueueStats{}, nil
}
func (a *adminStubArticles) ListByExtractStatus(context.Context, string, int, int) ([]*domain.Article, error) {
	return nil, nil
}
func (a *adminStubArticles) ListByIndexStatus(context.Context, string, int, int) ([]*domain.Article, error) {
	return nil, nil
}
func (a *adminStubArticles) ListSoftDeleted(context.Context, time.Time, int) ([]*domain.Article, error) {
	return nil, nil
}
func (a *adminStubArticles) ListSoftDeletedSince(context.Context, time.Time, int) ([]*domain.Article, error) {
	return nil, nil
}

// adminStubPages hiện thân domain.IArticlePageRepository.
type adminStubPages struct {
	trace adminTrace
}

func (p *adminStubPages) CreateBatch(context.Context, []*domain.ArticlePage) error { return nil }
func (p *adminStubPages) Progress(context.Context, int64) (*domain.PageProgress, error) {
	return &domain.PageProgress{DoneSeq: -1}, nil
}
func (p *adminStubPages) ListByArticle(context.Context, int64) ([]*domain.ArticlePage, error) {
	return nil, nil
}
func (p *adminStubPages) ListPagesRange(context.Context, int64, int, int) ([]*domain.ArticlePage, error) {
	return nil, nil
}
func (p *adminStubPages) DeleteByArticleID(context.Context, int64) error { return nil }
func (p *adminStubPages) DeleteOrphan(context.Context) (int64, error)    { return 0, nil }
func (p *adminStubPages) DeleteByArticles(_ context.Context, ids []int64) error {
	p.trace.add("pages.DeleteByArticles", ids)
	return nil
}

// adminStubCurrentUser trả user_id cố định — route admin đã qua auth nên
// middleware luôn có sẵn, không cần kiểm tra gì ở tầng service.
type adminStubCurrentUser struct{ id int64 }

func (u adminStubCurrentUser) UserID(context.Context) (int64, bool) { return u.id, true }

// adminStubVectors KHÔNG được gọi trong ResetExtract — reset extract cố ý
// không đụng vector (search vẫn chạy được tới khi ai đó gọi rebuild-vectors).
// Stub gọi t.Error nếu bị chạm để khoá luôn điều đó.
type adminStubVectors struct{ t *testing.T }

func (v adminStubVectors) UpsertPoints(context.Context, []*domain.VectorPoint) error { return nil }
func (v adminStubVectors) DeleteByArticle(context.Context, int64) error {
	v.t.Error("ResetExtract không được xoá vector của 1 bài")
	return nil
}
func (v adminStubVectors) DeleteByArticles(context.Context, []int64) error {
	v.t.Error("ResetExtract không được xoá vector")
	return nil
}
func (v adminStubVectors) CountByArticle(context.Context, int64) (int64, error) { return 0, nil }
func (v adminStubVectors) CountByFilter(context.Context, domain.SearchFilter) (int64, error) {
	return 0, nil
}
func (v adminStubVectors) CountCollection(context.Context) (int64, error) { return 0, nil }
func (v adminStubVectors) Search(context.Context, []float32, domain.SearchFilter, int, float32) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v adminStubVectors) SearchText(context.Context, string, domain.SearchFilter, int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v adminStubVectors) ScrollNeighbors(context.Context, domain.SearchFilter, int, int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v adminStubVectors) ScrollRange(context.Context, domain.SearchFilter, int, int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v adminStubVectors) ListSections(context.Context, domain.SearchFilter) ([]domain.SectionSpan, error) {
	return nil, nil
}
func (v adminStubVectors) IsPermanentError(error) bool { return false }

// sharedTracePages là adapter đẩy bước xoá trang vào CÙNG dãy trace với
// articleRepo — tách riêng 2 dãy thì test không chứng minh được thứ tự.
type sharedTracePages struct {
	*adminStubPages
	shared *adminTrace
}

func (p *sharedTracePages) DeleteByArticles(_ context.Context, ids []int64) error {
	p.shared.add("pages.DeleteByArticles", ids)
	return nil
}

// newAdminSvc nối 2 stub qua trace CHUNG để test đọc được thứ tự thật.
func newAdminSvc(t *testing.T, arts *adminStubArticles, pages *adminStubPages) IArticleAdminService {
	t.Helper()
	return NewArticleAdminService(
		arts,
		&sharedTracePages{adminStubPages: pages, shared: &arts.trace},
		adminStubVectors{t: t},
		adminStubCurrentUser{id: 1},
	)
}

var (
	_ domain.ArticleAdminQueue      = (*adminStubArticles)(nil)
	_ domain.IArticlePageRepository = (*adminStubPages)(nil)
	_ domain.IArticlePageRepository = (*sharedTracePages)(nil)
	_ domain.IVectorStore           = adminStubVectors{}
	_ middlewares.ICurrentUser      = adminStubCurrentUser{}
)
