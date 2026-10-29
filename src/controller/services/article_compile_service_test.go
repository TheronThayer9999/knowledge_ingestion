package services

import (
	"context"
	stderrors "errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"knowledge_ingestion/src/common/llmusage"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/domain"
)

// TestCompileOne khoá đường đi chính của compileOne (P2.4 phase 1b) mà không
// dựng fx + Qdrant + LLM thật: PageIndex từ vector stub, text từ ScrollRange
// stub, summary từ summarizer stub, ghi qua sectionRepo stub. Cache không
// dựng ở unit test (xem ghi chú cuối file — đã kiểm bằng e2e Redis thật).

type compileStubVectors struct {
	domain.IVectorStore
	sections []domain.SectionSpan
	texts    map[int]string // chunk from -> text (ScrollRange trả theo from)
	err      error
	permErr  bool
	gotFilt  domain.SearchFilter
	scrolled []int // các from đã ScrollRange
}

func (v *compileStubVectors) ListSections(_ context.Context, f domain.SearchFilter) ([]domain.SectionSpan, error) {
	v.gotFilt = f
	return v.sections, v.err
}

func (v *compileStubVectors) ScrollRange(_ context.Context, _ domain.SearchFilter, from, limit int) ([]*domain.ScoredChunk, error) {
	out := make([]*domain.ScoredChunk, 0, limit)
	for i := 0; i < limit; i++ {
		if txt, ok := v.texts[from+i]; ok {
			out = append(out, &domain.ScoredChunk{ChunkIndex: from + i, Text: txt})
		}
	}
	v.scrolled = append(v.scrolled, from)
	return out, nil
}

func (v *compileStubVectors) IsPermanentError(error) bool { return v.permErr }

// compileStubSummarizer đếm số lượt gọi LLM thật (cache miss) — test đo được
// bài compile lại không đổi nội dung thì KHÔNG tốn lượt LLM nào. Mỗi lượt
// gọi cũng giả lập handler của Eino: cộng số liệu vào collector đi theo ctx.
type compileStubSummarizer struct {
	calls    int
	batches  [][]domain.SectionContent
	model    string
	provider string
	err      error
}

func (s *compileStubSummarizer) ModelID() string    { return s.model }
func (s *compileStubSummarizer) ProviderID() string { return s.provider }

func (s *compileStubSummarizer) SummarizeBatch(ctx context.Context, items []domain.SectionContent) ([]string, error) {
	s.calls++
	s.batches = append(s.batches, items)
	if c, ok := llmusage.FromCtx(ctx); ok {
		c.Add(utils.LLMSiteFromCtx(ctx), domain.LLMUsage{
			Calls: 1, PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15,
		})
	}
	if s.err != nil {
		return nil, s.err
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, "tóm tắt của "+it.Name)
	}
	return out, nil
}

// compileStubSectionRepo bắt bản ghi Upsert/Replace để đối chiếu.
type compileStubSectionRepo struct {
	replaced  []*domain.ArticleSection
	replaceID int64
	err       error
	upserted  []*domain.ArticleSection
	upsertErr error
	events    *[]string
}

func (r *compileStubSectionRepo) note(evt string) {
	if r.events != nil {
		*r.events = append(*r.events, evt)
	}
}

func (r *compileStubSectionRepo) Replace(_ context.Context, articleID int64, sections []*domain.ArticleSection) error {
	r.replaceID = articleID
	r.replaced = sections
	r.note("replace")
	return r.err
}

func (r *compileStubSectionRepo) Upsert(_ context.Context, sections []*domain.ArticleSection) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	r.upserted = append(r.upserted, sections...)
	r.note("upsert")
	return nil
}

func (r *compileStubSectionRepo) ListByArticle(context.Context, int64) ([]*domain.ArticleSection, error) {
	return nil, nil
}
func (r *compileStubSectionRepo) DeleteByArticles(context.Context, []int64) error { return nil }

// compileStubQueue giả vai ArticleCompileQueue — ghi nhận done/error/checkpoint
// để test đường đi trạng thái mà không cần DB.
type compileStubQueue struct {
	domain.ArticleCompileQueue
	pending     []*domain.Article
	done        map[int64]bool
	errAttempts map[int64]int
	progress    map[int64]int
	claimErr    error
	events      *[]string
}

func (q *compileStubQueue) ClaimCompilePending(context.Context, int, time.Duration) ([]*domain.Article, error) {
	return q.pending, q.claimErr
}
func (q *compileStubQueue) MarkCompileProgress(_ context.Context, id int64, done int) error {
	q.progress[id] = done
	if q.events != nil {
		*q.events = append(*q.events, "progress")
	}
	return nil
}
func (q *compileStubQueue) MarkCompileDone(_ context.Context, id int64) error {
	q.done[id] = true
	return nil
}
func (q *compileStubQueue) MarkCompileError(_ context.Context, id int64, attempts int) error {
	q.errAttempts[id] = attempts
	return nil
}
func (q *compileStubQueue) ReleaseCompileClaims(context.Context, []int64) error { return nil }
func (q *compileStubQueue) IsAlive(context.Context, int64) (bool, error)        { return true, nil }

// compileStubUsageRepo bắt dòng audit usage để kiểm tra compile có ghi
// chi phí LLM theo tài khoản hay không.
type compileStubUsageRepo struct {
	recs []domain.LLMUsageRecord
}

func (r *compileStubUsageRepo) Record(_ context.Context, recs []domain.LLMUsageRecord) error {
	r.recs = append(r.recs, recs...)
	return nil
}
func (r *compileStubUsageRepo) List(context.Context, domain.LLMUsageFilter) ([]*domain.LLMUsageRow, error) {
	return nil, nil
}

func newCompileSvc(repo domain.ArticleCompileQueue, sectionRepo domain.IArticleSectionRepository,
	vectors domain.IVectorStore, summ domain.ISummarizer, usageRepo domain.ILLMUsageRepository) *articleCompileService {
	// Cache truyền nil — đường hit/miss của Redis đã được kiểm bằng e2e live
	// với Redis thật (key compile:sum:*), không cần dựng Redis giả trong unit
	// test.
	s := NewArticleCompileService(repo, sectionRepo, vectors, summ, nil, usageRepo).(*articleCompileService)
	s.maxChunksPerSection = 6
	return s
}

// compileQueue trả queue giả rỗng — compileOne cần nó để ghi checkpoint.
func compileQueue() *compileStubQueue {
	return &compileStubQueue{done: map[int64]bool{}, errAttempts: map[int64]int{}, progress: map[int64]int{}}
}

// compileSections dựng danh sách mục giả: mỗi mục cách nhau 10 chunk để
// cửa sổ nạp của mục không lấn mục kế tiếp.
func compileSections(n int) []domain.SectionSpan {
	out := make([]domain.SectionSpan, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, domain.SectionSpan{Name: fmt.Sprintf("MỤC %d", i+1), From: i * 10})
	}
	return out
}

// compileTexts điền text cho chunk của từng mục — mục nào không có chunk nào
// trong map thì không có text (không tốn lượt LLM nào).
func compileTexts(sections []domain.SectionSpan, span int) map[int]string {
	texts := map[int]string{}
	for _, sec := range sections {
		for i := 0; i < span; i++ {
			texts[sec.From+i] = "nội dung " + sec.Name
		}
	}
	return texts
}

// TestCompileOneBatchesSectionsGộp nhiều mục vào 1 lượt gọi LLM — trước đây
// 1 mục 1 lượt nên bài nhiều mục tốn hàng nghìn request.
func TestCompileOneBatchesSectionsGop(t *testing.T) {
	sections := compileSections(10)
	vectors := &compileStubVectors{sections: sections, texts: compileTexts(sections, 6)}
	summ := &compileStubSummarizer{model: "test-model", provider: "9router"}
	sectionRepo := &compileStubSectionRepo{}
	repo := &compileStubQueue{progress: map[int64]int{}}
	svc := newCompileSvc(repo, sectionRepo, vectors, summ, nil)
	svc.batchSize = 4

	article := &domain.Article{BaseModel: domain.BaseModel{ID: 8}, UserID: 7}
	if err := svc.compileOne(context.Background(), "trace", article); err != nil {
		t.Fatalf("compileOne: %v", err)
	}
	// 10 mục, lô 4 → 3 lượt gọi LLM (4 + 4 + 2) thay vì 10 lượt.
	if summ.calls != 3 {
		t.Errorf("số lượt gọi LLM = %d, muốn 3 (gộp lô 4)", summ.calls)
	}
	if len(summ.batches) != 3 {
		t.Fatalf("số lô = %d, muốn 3", len(summ.batches))
	}
	if len(summ.batches[0]) != 4 || len(summ.batches[2]) != 2 {
		t.Errorf("kích thước lô sai: %d/%d/%d, muốn 4/4/2",
			len(summ.batches[0]), len(summ.batches[1]), len(summ.batches[2]))
	}
	if len(sectionRepo.upserted) != 10 {
		t.Errorf("số mục ghi vào article_sections = %d, muốn 10", len(sectionRepo.upserted))
	}
	if repo.progress[8] != 10 {
		t.Errorf("checkpoint = %d, muốn 10", repo.progress[8])
	}
	if svc.cycleLLMCalls.Load() != 3 {
		t.Errorf("bộ đếm kỳ quét = %d, muốn 3", svc.cycleLLMCalls.Load())
	}
}

// TestCompileOneResumesFromCheckpoint bài chết giữa chừng thì kỳ sau chỉ làm
// nốt phần còn lại — không tóm tắt lại từ đầu.
func TestCompileOneResumesFromCheckpoint(t *testing.T) {
	sections := compileSections(10)
	vectors := &compileStubVectors{sections: sections, texts: compileTexts(sections, 6)}
	summ := &compileStubSummarizer{model: "test-model"}
	sectionRepo := &compileStubSectionRepo{}
	repo := &compileStubQueue{progress: map[int64]int{}}
	svc := newCompileSvc(repo, sectionRepo, vectors, summ, nil)
	svc.batchSize = 4

	article := &domain.Article{BaseModel: domain.BaseModel{ID: 8}, UserID: 7, CompileDoneSections: 4}
	if err := svc.compileOne(context.Background(), "trace", article); err != nil {
		t.Fatalf("compileOne: %v", err)
	}
	// Chỉ 6 mục còn lại mới tóm tắt: 1 lô 4 + 1 lô 2.
	if summ.calls != 2 {
		t.Errorf("số lượt gọi LLM = %d, muốn 2 (bỏ qua 4 mục đã xong)", summ.calls)
	}
	if len(sectionRepo.upserted) != 6 {
		t.Errorf("số mục ghi = %d, muốn 6", len(sectionRepo.upserted))
	}
	if sectionRepo.upserted[0].Name != "MỤC 5" {
		t.Errorf("phải làm nốt từ MỤC 5, nhận %q", sectionRepo.upserted[0].Name)
	}
	// Chạy lại có checkpoint thì KHÔNG dọn dòng cũ — dọn là xoá luôn phần
	// đã làm xong.
	if sectionRepo.replaceID != 0 {
		t.Errorf("có checkpoint thì không được dọn dòng cũ, replaceID = %d", sectionRepo.replaceID)
	}
	if repo.progress[8] != 10 {
		t.Errorf("checkpoint = %d, muốn 10", repo.progress[8])
	}
}

// TestCompileOneUpsertsBeforeCheckpoint khoá thứ tự ghi: article_sections
// phải có trước khi checkpoint tăng — nếu ngược lại thì crash giữa chừng
// làm mất mục (checkpoint LEAD, không bao giờ làm lại).
func TestCompileOneUpsertsBeforeCheckpoint(t *testing.T) {
	sections := compileSections(3)
	vectors := &compileStubVectors{sections: sections, texts: compileTexts(sections, 6)}
	summ := &compileStubSummarizer{model: "test-model"}
	events := []string{}
	sectionRepo := &compileStubSectionRepo{events: &events}
	repo := &compileStubQueue{progress: map[int64]int{}, events: &events}
	svc := newCompileSvc(repo, sectionRepo, vectors, summ, nil)
	svc.batchSize = 3

	article := &domain.Article{BaseModel: domain.BaseModel{ID: 5}, UserID: 7}
	if err := svc.compileOne(context.Background(), "trace", article); err != nil {
		t.Fatalf("compileOne: %v", err)
	}
	want := []string{"replace", "upsert", "progress"}
	if len(events) != len(want) {
		t.Fatalf("chuỗi sự kiện = %v, muốn %v", events, want)
	}
	for i, evt := range want {
		if events[i] != evt {
			t.Fatalf("thứ tự sự kiện = %v, muốn %v", events, want)
		}
	}
}

// TestCompileOneEmptySectionNoLLM mục không có chunk nào thì không tốn lượt
// LLM nào (bộ đếm chi phí phải khớp số lượt gọi thật).
func TestCompileOneEmptySectionNoLLM(t *testing.T) {
	sections := compileSections(3)
	texts := compileTexts(sections, 6)
	for i := 0; i < 6; i++ {
		delete(texts, sections[1].From+i)
	}
	vectors := &compileStubVectors{sections: sections, texts: texts}
	summ := &compileStubSummarizer{model: "m"}
	sectionRepo := &compileStubSectionRepo{}
	svc := newCompileSvc(compileQueue(), sectionRepo, vectors, summ, nil)
	svc.batchSize = 3

	article := &domain.Article{BaseModel: domain.BaseModel{ID: 1}, UserID: 7}
	if err := svc.compileOne(context.Background(), "t", article); err != nil {
		t.Fatalf("compileOne: %v", err)
	}
	if summ.calls != 1 {
		t.Errorf("lượt gọi LLM = %d, muốn 1 (mục rỗng không tốn lượt nào)", summ.calls)
	}
	if len(summ.batches[0]) != 2 {
		t.Errorf("lô gửi LLM phải có 2 mục, nhận %d", len(summ.batches[0]))
	}
	// Cả 3 mục vẫn được ghi (mục rỗng có summary rỗng) để cây mục đầy đủ.
	if len(sectionRepo.upserted) != 3 {
		t.Errorf("số mục ghi = %d, muốn 3", len(sectionRepo.upserted))
	}
	if sectionRepo.upserted[1].Summary != "" {
		t.Errorf("mục rỗng phải có summary rỗng, nhận %q", sectionRepo.upserted[1].Summary)
	}
}

func TestCompileOneWritesSummaries(t *testing.T) {
	sections := []domain.SectionSpan{{Name: "ĐIỀU 5", From: 40}, {Name: "ĐIỀU 6", From: 42}}
	vectors := &compileStubVectors{sections: sections, texts: map[int]string{40: "a", 41: "b", 42: "c"}}
	summ := &compileStubSummarizer{model: "test-model"}
	sectionRepo := &compileStubSectionRepo{}
	svc := newCompileSvc(compileQueue(), sectionRepo, vectors, summ, nil)

	article := &domain.Article{BaseModel: domain.BaseModel{ID: 8}, UserID: 7}
	if err := svc.compileOne(context.Background(), "trace", article); err != nil {
		t.Fatalf("compileOne: %v", err)
	}
	// Phân quyền: filter mang owner + article id.
	if vectors.gotFilt.UserID != 7 || vectors.gotFilt.ArticleID != 8 {
		t.Errorf("filter sai quyền: %+v", vectors.gotFilt)
	}
	if len(sectionRepo.upserted) != 2 {
		t.Fatalf("mong 2 mục, nhận %d", len(sectionRepo.upserted))
	}
	first := sectionRepo.upserted[0]
	if first.Name != "ĐIỀU 5" || first.StartChunk != 40 || first.Summary != "tóm tắt của ĐIỀU 5" {
		t.Errorf("mục đầu sai: %+v", first)
	}
	if summ.calls != 1 {
		t.Errorf("2 mục gộp 1 lô thì gọi LLM 1 lần, nhận %d", summ.calls)
	}
}

func TestCompileOneNoSectionsReplacesEmpty(t *testing.T) {
	vectors := &compileStubVectors{}
	sectionRepo := &compileStubSectionRepo{}
	summ := &compileStubSummarizer{}
	svc := newCompileSvc(compileQueue(), sectionRepo, vectors, summ, nil)
	if err := svc.compileOne(context.Background(), "t", &domain.Article{BaseModel: domain.BaseModel{ID: 3}}); err != nil {
		t.Fatalf("compileOne: %v", err)
	}
	if sectionRepo.replaceID != 3 || sectionRepo.replaced != nil {
		t.Errorf("bài không mục phải Replace rỗng, nhận id=%d rows=%v", sectionRepo.replaceID, sectionRepo.replaced)
	}
	if summ.calls != 0 {
		t.Errorf("không mục thì không gọi LLM, nhận %d", summ.calls)
	}
}

// Đường hit/miss của cache Redis KHÔNG test bằng Redis giả (miniredis) ở
// đây — đã xác nhận bằng e2e live với shop-redis thật: compile xong bài thì
// key compile:sum:<hash> xuất hiện, compile lại không đổi nội dung không tốn
// lượt LLM.

func TestCompileOneSummarizeErrorPropagates(t *testing.T) {
	vectors := &compileStubVectors{
		sections: []domain.SectionSpan{{Name: "ĐIỀU 5", From: 0}},
		texts:    map[int]string{0: "x"},
	}
	boom := stderrors.New("llm down")
	svc := newCompileSvc(nil, &compileStubSectionRepo{}, vectors, &compileStubSummarizer{err: boom}, nil)
	err := svc.compileOne(context.Background(), "t", &domain.Article{BaseModel: domain.BaseModel{ID: 1}})
	if err == nil || !stderrors.Is(err, boom) {
		t.Fatalf("lỗi LLM phải nổi lên để backoff, nhận: %v", err)
	}
}

// TestCompileRecordsUsageAudit compile phải đẩy chi phí LLM vào bảng audit
// theo tài khoản — nền của việc quản lý "acc này dùng bao nhiêu LLM".
func TestCompileRecordsUsageAudit(t *testing.T) {
	sections := compileSections(3)
	vectors := &compileStubVectors{sections: sections, texts: compileTexts(sections, 6)}
	summ := &compileStubSummarizer{model: "test-model", provider: "9router"}
	usageRepo := &compileStubUsageRepo{}
	svc := newCompileSvc(compileQueue(), &compileStubSectionRepo{}, vectors, summ, usageRepo)
	svc.batchSize = 3

	article := &domain.Article{BaseModel: domain.BaseModel{ID: 4}, UserID: 42}
	if err := svc.compileOne(context.Background(), "t", article); err != nil {
		t.Fatalf("compileOne: %v", err)
	}
	if len(usageRepo.recs) != 1 {
		t.Fatalf("số dòng audit = %d, muốn 1", len(usageRepo.recs))
	}
	rec := usageRepo.recs[0]
	if rec.UserID != 42 || rec.Site != "compile" || rec.Provider != "9router" || rec.Model != "test-model" {
		t.Errorf("dòng audit sai: %+v", rec)
	}
	if rec.Calls != 1 || rec.TotalTokens != 15 {
		t.Errorf("số liệu audit sai: %+v", rec)
	}
}

func TestCompileClaimedDoneAndError(t *testing.T) {
	t.Run("xong thi MarkCompileDone", func(t *testing.T) {
		repo := &compileStubQueue{done: map[int64]bool{}, progress: map[int64]int{}}
		vectors := &compileStubVectors{sections: []domain.SectionSpan{{Name: "A", From: 0}}, texts: map[int]string{0: "t"}}
		svc := newCompileSvc(repo, &compileStubSectionRepo{}, vectors, &compileStubSummarizer{model: "m"}, nil)
		var cnt atomic.Int64
		article := &domain.Article{BaseModel: domain.BaseModel{ID: 9}, UserID: 1, CompileAttempts: 2}
		svc.compileClaimed(context.Background(), "t", article, &cnt)
		if !repo.done[9] {
			t.Error("compile xong phải đánh dấu done")
		}
		if cnt.Load() != 1 {
			t.Errorf("bộ đếm compiled = %d, muốn 1", cnt.Load())
		}
	})
	t.Run("loi thi MarkCompileError tang attempts", func(t *testing.T) {
		repo := &compileStubQueue{errAttempts: map[int64]int{}, progress: map[int64]int{}}
		vectors := &compileStubVectors{
			sections: []domain.SectionSpan{{Name: "A", From: 0}},
			texts:    map[int]string{0: "t"},
		}
		svc := newCompileSvc(repo, &compileStubSectionRepo{}, vectors, &compileStubSummarizer{model: "m", err: stderrors.New("boom")}, nil)
		var cnt atomic.Int64
		article := &domain.Article{BaseModel: domain.BaseModel{ID: 9}, UserID: 1, CompileAttempts: 2}
		svc.compileClaimed(context.Background(), "t", article, &cnt)
		// attempts = article.CompileAttempts + 1 = 3.
		if repo.errAttempts[9] != 3 {
			t.Errorf("attempts = %d, muốn 3", repo.errAttempts[9])
		}
		if cnt.Load() != 0 {
			t.Error("bài lỗi không được tăng bộ đếm done")
		}
	})
}

func TestCompilePendingClaimAndReturn(t *testing.T) {
	repo := &compileStubQueue{
		pending: []*domain.Article{
			{BaseModel: domain.BaseModel{ID: 1}, UserID: 1},
			{BaseModel: domain.BaseModel{ID: 2}, UserID: 1},
		},
		done:     map[int64]bool{},
		progress: map[int64]int{},
	}
	vectors := &compileStubVectors{sections: []domain.SectionSpan{{Name: "A", From: 0}}, texts: map[int]string{0: "t"}}
	svc := newCompileSvc(repo, &compileStubSectionRepo{}, vectors, &compileStubSummarizer{model: "m"}, nil)
	n, err := svc.CompilePending(context.Background())
	if err != nil {
		t.Fatalf("CompilePending: %v", err)
	}
	if n != 2 {
		t.Errorf("compiled = %d, muốn 2", n)
	}
	if !repo.done[1] || !repo.done[2] {
		t.Errorf("cả 2 bài phải done: %v", repo.done)
	}
}
