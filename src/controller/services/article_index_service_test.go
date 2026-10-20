package services

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"knowledge_ingestion/src/common/chunker"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/domain"
)

// TestIndexOneResumesFromCheckpoint khoá đúng cơ chế checkpoint của phase
// index: job chết giữa file lớn thì articles.index_done_chunks cho biết đã
// upsert tới đâu, lần sau chỉ làm nốt phần còn lại — và vì PointID tất định nên
// phần làm lại (nếu có) vẫn ghi đè đúng point cũ chứ không nhân bản.

// indexStubArticles là vai ArticleIndexQueue.
type indexStubArticles struct {
	markedDone  bool
	progressAt  []int
	alive       bool
	markedError int
	claim       []*domain.Article
	released    [][]int64
}

func (a *indexStubArticles) ClaimIndexPending(context.Context, int, time.Duration) ([]*domain.Article, error) {
	return a.claim, nil
}
func (a *indexStubArticles) MarkIndexProgress(_ context.Context, _ int64, done int) error {
	a.progressAt = append(a.progressAt, done)
	return nil
}
func (a *indexStubArticles) MarkIndexDone(context.Context, int64) error {
	a.markedDone = true
	return nil
}
func (a *indexStubArticles) MarkIndexError(context.Context, int64, int) error {
	a.markedError++
	return nil
}
func (a *indexStubArticles) ReleaseIndexClaims(_ context.Context, ids []int64) error {
	a.released = append(a.released, append([]int64(nil), ids...))
	return nil
}
func (a *indexStubArticles) IsAlive(ctx context.Context, _ int64) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return a.alive, nil
}

// TestEnsureAlivePropagatesContextError khoá lỗi cũ: IsAlive lỗi (nhất là
// ctx.Canceled lúc shutdown) bị đổi thành false, indexOne trả nil và bài bị
// MarkIndexDone dù chưa index xong. Giờ lỗi phải nổi lên để indexClaimed
// release claim hoặc đánh lỗi đúng đường.
func TestEnsureAlivePropagatesContextError(t *testing.T) {
	svc := &articleIndexService{articleRepo: &indexStubArticles{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	alive, err := svc.ensureAlive(ctx, "trace", &domain.Article{BaseModel: domain.BaseModel{ID: 1}})
	if err == nil {
		t.Fatal("muốn lỗi ctx.Canceled, nhận nil")
	}
	if alive {
		t.Fatal("muốn alive=false khi IsAlive lỗi")
	}
}

// TestIndexPendingReleasesUnstartedInOneBatch khoá 2 thứ:
//   - bài claim được nhưng ctx hủy trước khi vào goroutine phải được trả về
//     pending (không nằm processing tới hết lease);
//   - trả cả lô bằng ĐÚNG 1 lời gọi repo, không N+1.
func TestIndexPendingReleasesUnstartedInOneBatch(t *testing.T) {
	stub := &indexStubArticles{claim: []*domain.Article{
		{BaseModel: domain.BaseModel{ID: 1}},
		{BaseModel: domain.BaseModel{ID: 2}},
		{BaseModel: domain.BaseModel{ID: 3}},
	}}
	svc := &articleIndexService{articleRepo: stub}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // giả lập runner cancel trước khi kịp chạy bài nào

	if _, err := svc.IndexPending(ctx); err != nil {
		t.Fatalf("IndexPending: %v", err)
	}
	if len(stub.released) != 1 {
		t.Fatalf("muốn 1 lời gọi release theo lô, nhận %d", len(stub.released))
	}
	ids := stub.released[0]
	slices.Sort(ids)
	if !slices.Equal(ids, []int64{1, 2, 3}) {
		t.Fatalf("ids release = %v, muốn [1 2 3]", ids)
	}
}

// indexStubPages là nguồn nội dung của phase index — mô phỏng bảng
// article_pages, trả về CỬA SỔ TRƯỢT y hệt repo thật (tôn trọng limit, sắp
// theo page_num) để test bắt được lỗi "kéo cả file lên một lượt".
type indexStubPages struct {
	pages    []*domain.ArticlePage
	calls    int
	maxLimit int
}

func (p *indexStubPages) ListPagesRange(_ context.Context, _ int64, fromSeq, limit int) ([]*domain.ArticlePage, error) {
	p.calls++
	if p.maxLimit > 0 && limit > p.maxLimit {
		limit = p.maxLimit
	}
	out := make([]*domain.ArticlePage, 0, limit)
	for _, pg := range p.pages {
		if pg.Seq < fromSeq {
			continue
		}
		if len(out) >= limit {
			break
		}
		out = append(out, pg)
	}
	return out, nil
}

func (p *indexStubPages) ListByArticle(context.Context, int64) ([]*domain.ArticlePage, error) {
	return p.pages, nil
}
func (p *indexStubPages) CreateBatch(context.Context, []*domain.ArticlePage) error { return nil }
func (p *indexStubPages) Progress(context.Context, int64) (*domain.PageProgress, error) {
	return &domain.PageProgress{DoneSeq: -1}, nil
}
func (p *indexStubPages) DeleteByArticleID(context.Context, int64) error  { return nil }
func (p *indexStubPages) DeleteByArticles(context.Context, []int64) error { return nil }
func (p *indexStubPages) DeleteOrphan(context.Context) (int64, error)     { return 0, nil }

// indexStubEmbedder trả vector giả — chỉ cần đúng SỐ LƯỢNG, giá trị không ai
// assert.
type indexStubEmbedder struct{}

func (indexStubEmbedder) Embed(context.Context, string) ([]float32, error) { return nil, nil }
func (indexStubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i), 1}
	}
	return out, nil
}
func (indexStubEmbedder) Dimension() int    { return 2 }
func (indexStubEmbedder) ModelName() string { return "stub" }
func (indexStubEmbedder) IsPermanentError(error) bool {
	return false
}

// indexStubVectors ghi lại point đã upsert + các lần xoá.
type indexStubVectors struct {
	points  []*domain.VectorPoint
	deleted int
}

func (v *indexStubVectors) UpsertPoints(_ context.Context, points []*domain.VectorPoint) error {
	v.points = append(v.points, points...)
	return nil
}
func (v *indexStubVectors) DeleteByArticle(context.Context, int64) error    { v.deleted++; return nil }
func (v *indexStubVectors) DeleteByArticles(context.Context, []int64) error { return nil }
func (v *indexStubVectors) CountByArticle(context.Context, int64) (int64, error) {
	return int64(len(v.points)), nil
}
func (v *indexStubVectors) Search(context.Context, []float32, domain.SearchFilter, int, float32) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v *indexStubVectors) SearchText(context.Context, string, domain.SearchFilter, int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v *indexStubVectors) ScrollNeighbors(context.Context, domain.SearchFilter, int, int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v *indexStubVectors) ScrollRange(context.Context, domain.SearchFilter, int, int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v *indexStubVectors) ListSections(context.Context, domain.SearchFilter) ([]domain.SectionSpan, error) {
	return nil, nil
}
func (v *indexStubVectors) IsPermanentError(error) bool { return false }

// pagesFrom dựng article_pages từ các đoạn text — nguồn nội dung duy nhất
// của phase index sau khi tách pipeline. Seq = thứ tự đọc, PageNum = số trang
// thật (giả lập PDF để PageNum có ý nghĩa trong assert).
func pagesFrom(texts ...string) []*domain.ArticlePage {
	out := make([]*domain.ArticlePage, 0, len(texts))
	for i, t := range texts {
		out = append(out, &domain.ArticlePage{
			ArticleID: 1,
			Seq:       i + 1,
			PageNum:   i + 1,
			Text:      t,
		})
	}
	return out
}

func newIndexSvc(arts domain.ArticleIndexQueue, pages *indexStubPages, vec *indexStubVectors) IArticleIndexService {
	return NewArticleIndexService(arts, pages, indexStubEmbedder{}, vec, nil, chunker.Manual{})
}

func TestIndexOneChunkIndexAndPage(t *testing.T) {
	body := strings.Repeat("Điều khoản này quy định rõ ràng. ", 60)
	arts := &indexStubArticles{alive: true}
	vec := &indexStubVectors{}
	pages := &indexStubPages{pages: pagesFrom(body)}
	svc := newIndexSvc(arts, pages, vec).(*articleIndexService)

	article := &domain.Article{BaseModel: domain.BaseModel{ID: 7}, StorageKey: "uploads/a.txt", PageCount: 1}
	if err := svc.indexOne(context.Background(), "trace-1", article); err != nil {
		t.Fatalf("indexOne lỗi: %v", err)
	}
	if len(vec.points) == 0 {
		t.Fatal("chưa upsert point nào")
	}
	for i, p := range vec.points {
		if p.ChunkIndex != i {
			t.Errorf("point[%d].ChunkIndex = %d, want %d", i, p.ChunkIndex, i)
		}
		if p.ArticleID != article.ID {
			t.Errorf("point[%d].ArticleID = %d, want %d", i, p.ArticleID, article.ID)
		}
		if p.PageNum != 1 {
			t.Errorf("point[%d].PageNum = %d, want 1", i, p.PageNum)
		}
		if p.ID == "" {
			t.Errorf("point[%d] không có ID — mất PointID tất định", i)
		}
	}
	// Lần chạy đầu phải dọn vector cũ trước khi ghi.
	if vec.deleted != 1 {
		t.Errorf("DeleteByArticle gọi %d lần, want 1 (lần chạy đầu phải dọn)", vec.deleted)
	}
	// Checkpoint cuối cùng phải bằng tổng số chunk.
	if len(arts.progressAt) == 0 {
		t.Fatal("chưa ghi checkpoint")
	}
	if last := arts.progressAt[len(arts.progressAt)-1]; last != len(vec.points) {
		t.Errorf("checkpoint cuối = %d, want %d", last, len(vec.points))
	}
}

func TestIndexOneResumeSkipsCheckpoint(t *testing.T) {
	body := strings.Repeat("Điều khoản này quy định rõ ràng. ", 60)
	arts := &indexStubArticles{alive: true}
	vec := &indexStubVectors{}
	pages := &indexStubPages{pages: pagesFrom(body)}
	svc := newIndexSvc(arts, pages, vec).(*articleIndexService)

	// Giả vờ lần trước đã upsert 3 chunk đầu.
	article := &domain.Article{
		BaseModel:       domain.BaseModel{ID: 9},
		StorageKey:      "uploads/a.txt",
		PageCount:       1,
		IndexDoneChunks: 3,
	}
	if err := svc.indexOne(context.Background(), "trace-2", article); err != nil {
		t.Fatalf("indexOne lỗi: %v", err)
	}
	// Phải bỏ qua 3 chunk đầu, chỉ upsert phần còn lại.
	for _, p := range vec.points {
		if p.ChunkIndex < 3 {
			t.Errorf("upsert lại chunk %d đã xong — mất tính idempotent", p.ChunkIndex)
		}
	}
	if len(arts.progressAt) > 0 && arts.progressAt[0] <= 3 {
		t.Errorf("checkpoint đầu = %d, phải > 3 (đã bỏ qua phần xong)", arts.progressAt[0])
	}
	// Có checkpoint thì KHÔNG dọn vector — dọn sẽ xoá cả phần đã xong.
	if vec.deleted != 0 {
		t.Errorf("DeleteByArticle gọi %d lần khi có checkpoint, want 0", vec.deleted)
	}
}

func TestIndexOneRejectsEmptyPages(t *testing.T) {
	arts := &indexStubArticles{alive: true}
	vec := &indexStubVectors{}
	pages := &indexStubPages{} // không có trang nào
	svc := newIndexSvc(arts, pages, vec).(*articleIndexService)

	article := &domain.Article{BaseModel: domain.BaseModel{ID: 1}, StorageKey: "uploads/a.txt"}
	if err := svc.indexOne(context.Background(), "trace-3", article); err == nil {
		t.Fatal("bài không có trang nào phải báo lỗi, không được đánh done giả")
	}
}

func TestIndexOneMarksErrorWhenArticleDeleted(t *testing.T) {
	body := strings.Repeat("Điều khoản này quy định rõ ràng. ", 60)
	arts := &indexStubArticles{alive: false} // user xoá bài giữa lúc đang index
	vec := &indexStubVectors{}
	pages := &indexStubPages{pages: pagesFrom(body)}
	svc := newIndexSvc(arts, pages, vec).(*articleIndexService)

	article := &domain.Article{BaseModel: domain.BaseModel{ID: 5}, StorageKey: "uploads/a.txt", PageCount: 1}
	if err := svc.indexOne(context.Background(), "trace-4", article); err != nil {
		t.Fatalf("indexOne lỗi: %v", err)
	}
	if arts.markedDone {
		t.Error("bài đã xoá không được đánh done")
	}
}

// TestIndexOneReadsInWindows là test khoá câu hỏi "đọc 1 lần có kéo hết file
// lên RAM không": stub cố tình giới hạn mỗi lượt chỉ trả về 1 trang, nên nếu
// service gọi ListPagesRange(limit=INDEX_PAGE_WINDOW) thì vẫn đúng — nhưng nếu
// nó đổi sang đọc toàn bộ bằng ListByArticle thì test này phải đỏ.
func TestIndexOneReadsInWindows(t *testing.T) {
	const totalPages = 25
	body := strings.Repeat("Điều khoản này quy định rõ ràng. ", 30)
	texts := make([]string, 0, totalPages)
	for i := 0; i < totalPages; i++ {
		texts = append(texts, body)
	}
	arts := &indexStubArticles{alive: true}
	vec := &indexStubVectors{}
	pages := &indexStubPages{pages: pagesFrom(texts...), maxLimit: 1}
	svc := newIndexSvc(arts, pages, vec).(*articleIndexService)

	article := &domain.Article{
		BaseModel:  domain.BaseModel{ID: 3},
		StorageKey: "uploads/a.pdf",
		PageCount:  totalPages,
	}
	if err := svc.indexOne(context.Background(), "trace-5", article); err != nil {
		t.Fatalf("indexOne lỗi: %v", err)
	}
	// Mỗi lượt chỉ lấy 1 trang → phải đọc hết số trang, không gộp làm 1.
	if pages.calls < totalPages {
		t.Errorf("ListPagesRange gọi %d lần, want >= %d (đọc cửa sổ trượt)",
			pages.calls, totalPages)
	}
	if len(vec.points) == 0 {
		t.Fatal("không upsert gì cả")
	}
	// Số trang phải tăng dần và không vượt quá số trang thật.
	seen := make(map[int]bool)
	for _, p := range vec.points {
		if p.PageNum < 1 || p.PageNum > totalPages {
			t.Fatalf("PageNum %d nằm ngoài 1..%d — map sai trang", p.PageNum, totalPages)
		}
		seen[p.PageNum] = true
	}
	if len(seen) != totalPages {
		t.Errorf("chunk chỉ phủ %d/%d trang — sót trang", len(seen), totalPages)
	}
}

// TestPageWindowCuts khoá phép map offset → số trang của cửa sổ, vì đây là
// chỗ duy nhất quyết định PageNum của từng chunk có đúng không.
func TestPageWindowCuts(t *testing.T) {
	w := newPageWindow()
	w.add(1, "AAAA")
	w.add(2, "BBBB")
	w.add(3, "CCCC")

	all := w.cuts(0, w.chars())
	if len(all) != 3 {
		t.Fatalf("cuts toàn cửa sổ ra %d mảnh, want 3", len(all))
	}
	for i, want := range []struct {
		page int
		text string
	}{{1, "AAAA"}, {2, "BBBB"}, {3, "CCCC"}} {
		if all[i].page != want.page || all[i].text != want.text {
			t.Errorf("mảnh %d = {page %d, %q}, want {page %d, %q}",
				i, all[i].page, all[i].text, want.page, want.text)
		}
	}
	// Cắt giữa trang 2 phải cho 2 mảnh với đúng số trang.
	mid := w.cuts(len("AAAA")+len(windowSep), len("AAAA")+len(windowSep)+len("BBBB"))
	if len(mid) != 1 || mid[0].page != 2 {
		t.Errorf("cắt trong trang 2 ra %v, want 1 mảnh page 2", mid)
	}
	if mid[0].text != "BBBB" {
		t.Errorf("text mảnh cắt = %q, want %q", mid[0].text, "BBBB")
	}
}

var (
	_ domain.ArticleIndexQueue      = (*indexStubArticles)(nil)
	_ domain.IArticlePageRepository = (*indexStubPages)(nil)
	_ domain.Embedder               = indexStubEmbedder{}
	_ domain.IVectorStore           = (*indexStubVectors)(nil)
	_                               = constants.INDEX_PAGE_WINDOW
)
