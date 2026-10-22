package services

import (
	"context"
	stderrors "errors"
	"sync/atomic"
	"testing"
	"time"

	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/caches"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestCompileOne khoá đường đi chính của compileOne (P2.4 phase 1b) mà không
// dựng fx + Qdrant + LLM thật: PageIndex từ vector stub, text từ ScrollRange
// stub, summary từ summarizer stub, ghi qua sectionRepo stub. Cache thật nối
// miniredis để test hit/miss.

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
// bài compile lại không đổi nội dung thì KHÔNG tốn lượt LLM nào.
type compileStubSummarizer struct {
	calls int
	model string
	err   error
}

func (s *compileStubSummarizer) ModelID() string { return s.model }
func (s *compileStubSummarizer) Summarize(_ context.Context, name, text string) (string, error) {
	s.calls++
	if s.err != nil {
		return "", s.err
	}
	return "tóm tắt của " + name, nil
}

// compileStubSectionRepo bắt bản ghi Replace cuối để đối chiếu.
type compileStubSectionRepo struct {
	replaced  []*domain.ArticleSection
	replaceID int64
	err       error
}

func (r *compileStubSectionRepo) Replace(_ context.Context, articleID int64, sections []*domain.ArticleSection) error {
	r.replaceID = articleID
	r.replaced = sections
	return r.err
}
func (r *compileStubSectionRepo) ListByArticle(context.Context, int64) ([]*domain.ArticleSection, error) {
	return nil, nil
}
func (r *compileStubSectionRepo) DeleteByArticles(context.Context, []int64) error { return nil }

// compileStubQueue giả vai ArticleCompileQueue — ghi nhận done/error để test
// đường đi trạng thái mà không cần DB.
type compileStubQueue struct {
	domain.ArticleCompileQueue
	pending     []*domain.Article
	done        map[int64]bool
	errAttempts map[int64]int
	claimErr    error
}

func (q *compileStubQueue) ClaimCompilePending(context.Context, int, time.Duration) ([]*domain.Article, error) {
	return q.pending, q.claimErr
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

func newCompileSvc(repo domain.ArticleCompileQueue, sectionRepo domain.IArticleSectionRepository, vectors domain.IVectorStore, summ domain.ISummarizer, rc *redis.Client) *articleCompileService {
	var cache caches.ICache
	if rc != nil {
		cache = &compileCache{client: rc}
	}
	s := NewArticleCompileService(repo, sectionRepo, vectors, summ, cache).(*articleCompileService)
	s.maxChunksPerSection = 6
	return s
}

// compileCache bọc *redis.Client thành caches.ICache cho test.
type compileCache struct{ client *redis.Client }

func (c *compileCache) GetClient() *redis.Client { return c.client }

func TestCompileOneWritesSummaries(t *testing.T) {
	vectors := &compileStubVectors{
		sections: []domain.SectionSpan{{Name: "ĐIỀU 5", From: 40}, {Name: "ĐIỀU 6", From: 42}},
		texts:    map[int]string{40: "a", 41: "b", 42: "c"},
	}
	summ := &compileStubSummarizer{model: "test-model"}
	sectionRepo := &compileStubSectionRepo{}
	svc := newCompileSvc(nil, sectionRepo, vectors, summ, nil)

	article := &domain.Article{BaseModel: domain.BaseModel{ID: 8}, UserID: 7}
	if err := svc.compileOne(context.Background(), "trace", article); err != nil {
		t.Fatalf("compileOne: %v", err)
	}
	// Phân quyền: filter mang owner + article id.
	if vectors.gotFilt.UserID != 7 || vectors.gotFilt.ArticleID != 8 {
		t.Errorf("filter sai quyền: %+v", vectors.gotFilt)
	}
	if sectionRepo.replaceID != 8 {
		t.Errorf("Replace bài sai: %d", sectionRepo.replaceID)
	}
	if len(sectionRepo.replaced) != 2 {
		t.Fatalf("mong 2 mục, nhận %d", len(sectionRepo.replaced))
	}
	first := sectionRepo.replaced[0]
	if first.Name != "ĐIỀU 5" || first.StartChunk != 40 || first.Summary != "tóm tắt của ĐIỀU 5" {
		t.Errorf("mục đầu sai: %+v", first)
	}
	if summ.calls != 2 {
		t.Errorf("miss cache hoàn toàn phải gọi LLM 2 lần, nhận %d", summ.calls)
	}
}

func TestCompileOneNoSectionsReplacesEmpty(t *testing.T) {
	vectors := &compileStubVectors{}
	sectionRepo := &compileStubSectionRepo{}
	summ := &compileStubSummarizer{}
	svc := newCompileSvc(nil, sectionRepo, vectors, summ, nil)
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

func TestCompileOneUsesCacheAcrossRuns(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	vectors := &compileStubVectors{
		sections: []domain.SectionSpan{{Name: "ĐIỀU 5", From: 0}},
		texts:    map[int]string{0: "nội dung A"},
	}
	summ := &compileStubSummarizer{model: "m"}
	svc := newCompileSvc(nil, &compileStubSectionRepo{}, vectors, summ, rc)
	article := &domain.Article{BaseModel: domain.BaseModel{ID: 8}, UserID: 1}

	if err := svc.compileOne(context.Background(), "t", article); err != nil {
		t.Fatalf("lần 1: %v", err)
	}
	if summ.calls != 1 {
		t.Fatalf("lần 1 phải gọi LLM 1 lượt, nhận %d", summ.calls)
	}
	// Lần 2 cùng nội dung + cùng model → ăn cache, không tốn LLM.
	if err := svc.compileOne(context.Background(), "t", article); err != nil {
		t.Fatalf("lần 2: %v", err)
	}
	if summ.calls != 1 {
		t.Errorf("lần 2 phải ăn cache (không gọi LLM), tổng lượt=%d", summ.calls)
	}
}

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

func TestCompileClaimedDoneAndError(t *testing.T) {
	t.Run("xong thi MarkCompileDone", func(t *testing.T) {
		repo := &compileStubQueue{done: map[int64]bool{}}
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
		repo := &compileStubQueue{errAttempts: map[int64]int{}}
		vectors := &compileStubVectors{
			sections: []domain.SectionSpan{{Name: "A", From: 0}},
			texts:    map[int]string{0: "t"},
		}
		svc := newCompileSvc(repo, &compileStubSectionRepo{}, vectors, &compileStubSummarizer{err: stderrors.New("boom")}, nil)
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
		done: map[int64]bool{},
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
