package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// Fake gọn cho search service — chỉ implement đúng method service gọi.

type fakeSearchEmbedder struct {
	vec []float32
	err error
}

func (f *fakeSearchEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("empty")
	}
	return f.vec, nil
}

func (f *fakeSearchEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	return nil, nil
}

func (f *fakeSearchEmbedder) Dimension() int                  { return 4 }
func (f *fakeSearchEmbedder) ModelName() string               { return "test-model" }
func (f *fakeSearchEmbedder) IsPermanentError(_ error) bool   { return false }

type fakeSearchVectors struct {
	hits      []*domain.ScoredChunk
	err       error
	gotFilter domain.SearchFilter
	gotLimit  int
}

func (f *fakeSearchVectors) UpsertPoints(_ context.Context, _ []*domain.VectorPoint) error {
	return nil
}

func (f *fakeSearchVectors) DeleteByArticle(_ context.Context, _ int64) error { return nil }

func (f *fakeSearchVectors) CountByArticle(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

func (f *fakeSearchVectors) IsPermanentError(_ error) bool { return false }

func (f *fakeSearchVectors) Search(_ context.Context, _ []float32, filter domain.SearchFilter, limit int, _ float32) ([]*domain.ScoredChunk, error) {
	f.gotFilter = filter
	f.gotLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.hits, nil
}

type fakeSearchArticles struct {
	doneIDs []int64
}

func (f *fakeSearchArticles) ListDoneIDs(_ context.Context, _ int64, ids []int64) ([]int64, error) {
	allowed := make(map[int64]bool, len(f.doneIDs))
	for _, id := range f.doneIDs {
		allowed[id] = true
	}
	var out []int64
	for _, id := range ids {
		if allowed[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

type fakeSearchUser struct{ id int64 }

func (f *fakeSearchUser) UserID(_ context.Context) (int64, bool) { return f.id, true }

func newSearchSvc(embedder *fakeSearchEmbedder, vectors *fakeSearchVectors, articles *fakeSearchArticles) ISearchService {
	return &searchService{embedder: embedder, vectors: vectors, articleRepo: searchRepoAdapter{articles}, currentUser: &fakeSearchUser{id: 7}}
}

// Adapter từ fake gọn sang interface domain.
type searchRepoAdapter struct{ f *fakeSearchArticles }

func (a searchRepoAdapter) ListDoneIDs(ctx context.Context, userID int64, ids []int64) ([]int64, error) {
	return a.f.ListDoneIDs(ctx, userID, ids)
}

func (a searchRepoAdapter) Create(_ context.Context, _ *domain.Article) error { return nil }

func (a searchRepoAdapter) Update(_ context.Context, _ *domain.Article) error { return nil }

func (a searchRepoAdapter) Delete(_ context.Context, _ *domain.Article) error { return nil }

func (a searchRepoAdapter) GetByID(_ context.Context, _ int64, _ int64) (*domain.Article, error) {
	return nil, domain.ErrNotFound
}

func (a searchRepoAdapter) IsAlive(_ context.Context, _ int64) (bool, error) { return true, nil }

func (a searchRepoAdapter) ExistsByURL(_ context.Context, _ string, _ int64) (bool, error) {
	return false, nil
}

func (a searchRepoAdapter) ExistsByStorageKey(_ context.Context, _ string, _ int64) (bool, error) {
	return false, nil
}

func (a searchRepoAdapter) ListByCategoryID(_ context.Context, _ int64, _ int64, _ int, _ int) ([]*domain.Article, error) {
	return nil, nil
}

func (a searchRepoAdapter) ListByCategoryName(_ context.Context, _ string, _ int64, _ int, _ int) ([]*domain.Article, error) {
	return nil, nil
}

func (a searchRepoAdapter) IDsByCategory(_ context.Context, _ int64, _ int64) ([]int64, error) {
	return nil, nil
}

func (a searchRepoAdapter) ResetQueue(_ context.Context, _ []int64, _ int64) (int64, error) {
	return 0, nil
}

func (a searchRepoAdapter) ListSoftDeleted(_ context.Context, _ time.Time, _ int) ([]*domain.Article, error) {
	return nil, nil
}

func (a searchRepoAdapter) ClaimChunkPending(_ context.Context, _ int, _ time.Duration) ([]*domain.Article, error) {
	return nil, nil
}

func (a searchRepoAdapter) MarkChunkDone(_ context.Context, _ int64) error { return nil }

func (a searchRepoAdapter) MarkChunkError(_ context.Context, _ int64, _ int) error { return nil }

func (a searchRepoAdapter) ClaimEmbedPending(_ context.Context, _ int, _ time.Duration) ([]*domain.Article, error) {
	return nil, nil
}

func (a searchRepoAdapter) MarkEmbedDone(_ context.Context, _ int64) error { return nil }

func (a searchRepoAdapter) MarkEmbedError(_ context.Context, _ int64, _ int) error { return nil }

func (a searchRepoAdapter) HardDelete(_ context.Context, _ int64) error { return nil }

// Happy path: filter quyền đúng user, limit mặc định, hit bài done giữ lại,
// hit bài dở bị chặn.
func TestSearch_FiltersUndone(t *testing.T) {
	embedder := &fakeSearchEmbedder{vec: []float32{0.1, 0.2, 0.3, 0.4}}
	vectors := &fakeSearchVectors{hits: []*domain.ScoredChunk{
		{PointID: "p1", Score: 0.9, ArticleID: 8, ChunkIndex: 0, PageNum: 0, Text: "đoạn done"},
		{PointID: "p2", Score: 0.8, ArticleID: 9, ChunkIndex: 1, PageNum: 0, Text: "đoạn dở"},
	}}
	articles := &fakeSearchArticles{doneIDs: []int64{8}}
	svc := newSearchSvc(embedder, vectors, articles)

	res := svc.Search(context.Background(), &dtos.SearchRequest{Query: "go fx", CategoryID: 5})
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	if vectors.gotFilter.UserID != 7 || vectors.gotFilter.CategoryID != 5 {
		t.Fatalf("filter quyền sai: %+v", vectors.gotFilter)
	}
	if vectors.gotLimit != 5 {
		t.Fatalf("limit mặc định phải 5, got %d", vectors.gotLimit)
	}
	if res.Data.Total != 1 || res.Data.Hits[0].ArticleID != 8 || res.Data.Hits[0].Text != "đoạn done" {
		t.Fatalf("phải chặn hit bài dở, got %+v", res.Data)
	}
}

// Query rỗng → 400.
func TestSearch_EmptyQuery(t *testing.T) {
	svc := newSearchSvc(&fakeSearchEmbedder{}, &fakeSearchVectors{}, &fakeSearchArticles{})
	if res := svc.Search(context.Background(), &dtos.SearchRequest{Query: "   "}); res.Err == nil {
		t.Fatal("query rỗng phải 400")
	}
}

// Lỗi embed lan ra ngoài để handler 500.
func TestSearch_EmbedError(t *testing.T) {
	embedder := &fakeSearchEmbedder{err: errors.New("ollama down")}
	svc := newSearchSvc(embedder, &fakeSearchVectors{}, &fakeSearchArticles{})
	if res := svc.Search(context.Background(), &dtos.SearchRequest{Query: "x"}); res.Err == nil {
		t.Fatal("lỗi embed phải fail")
	}
}

// Qdrant rỗng → response rỗng, không lỗi.
func TestSearch_NoHits(t *testing.T) {
	embedder := &fakeSearchEmbedder{vec: []float32{0.1}}
	svc := newSearchSvc(embedder, &fakeSearchVectors{}, &fakeSearchArticles{})
	res := svc.Search(context.Background(), &dtos.SearchRequest{Query: "x"})
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	if res.Data.Total != 0 || len(res.Data.Hits) != 0 {
		t.Fatalf("phải rỗng, got %+v", res.Data)
	}
}
