package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// Fake gọn cho admin service — chỉ implement đúng method service gọi, không
// ôm full interface repo (giống pattern fake worker service).

type fakeAdminArticleRepo struct {
	idsByCat map[int64][]int64
	resetIDs []int64
	resetN   int64
	resetErr error
}

func (f *fakeAdminArticleRepo) IDsByCategory(_ context.Context, categoryID int64, _ int64) ([]int64, error) {
	return f.idsByCat[categoryID], nil
}

func (f *fakeAdminArticleRepo) ResetQueue(_ context.Context, ids []int64, _ int64) (int64, error) {
	f.resetIDs = append([]int64{}, ids...)
	if f.resetErr != nil {
		return 0, f.resetErr
	}
	return f.resetN, nil
}

type fakeAdminChunkRepo struct {
	deleted []int64
}

func (f *fakeAdminChunkRepo) DeleteByArticleID(_ context.Context, articleID int64) error {
	f.deleted = append(f.deleted, articleID)
	return nil
}

type fakeAdminVectors struct {
	deleted []int64
	delErr  error
}

func (f *fakeAdminVectors) DeleteByArticle(_ context.Context, articleID int64) error {
	if f.delErr != nil {
		return f.delErr
	}
	f.deleted = append(f.deleted, articleID)
	return nil
}

type fakeAdminUser struct{ id int64 }

func (f *fakeAdminUser) UserID(_ context.Context) (int64, bool) { return f.id, true }

func newAdminSvc(repo *fakeAdminArticleRepo, chunks *fakeAdminChunkRepo, vectors *fakeAdminVectors) IArticleAdminService {
	return NewArticleAdminService(adminRepoAdapter{repo}, adminChunkAdapter{chunks}, adminVectorsAdapter{vectors}, &fakeAdminUser{id: 7})
}

// Adapter từ fake gọn sang interface domain — fake chỉ cần đúng method dùng,
// không phải implement cả interface ~20 method.
type adminRepoAdapter struct{ f *fakeAdminArticleRepo }

func (a adminRepoAdapter) IDsByCategory(ctx context.Context, categoryID int64, userID int64) ([]int64, error) {
	return a.f.IDsByCategory(ctx, categoryID, userID)
}
func (a adminRepoAdapter) ResetQueue(ctx context.Context, ids []int64, userID int64) (int64, error) {
	return a.f.ResetQueue(ctx, ids, userID)
}

func (a adminRepoAdapter) ListDoneIDs(_ context.Context, _ int64, ids []int64) ([]int64, error) {
	return ids, nil
}

func (a adminRepoAdapter) Create(_ context.Context, _ *domain.Article) error { return nil }

func (a adminRepoAdapter) Update(_ context.Context, _ *domain.Article) error { return nil }

func (a adminRepoAdapter) Delete(_ context.Context, _ *domain.Article) error { return nil }

func (a adminRepoAdapter) GetByID(_ context.Context, _ int64, _ int64) (*domain.Article, error) {
	return nil, domain.ErrNotFound
}

func (a adminRepoAdapter) IsAlive(_ context.Context, _ int64) (bool, error) { return true, nil }

func (a adminRepoAdapter) ExistsByURL(_ context.Context, _ string, _ int64) (bool, error) {
	return false, nil
}

func (a adminRepoAdapter) ExistsByStorageKey(_ context.Context, _ string, _ int64) (bool, error) {
	return false, nil
}

func (a adminRepoAdapter) ListByCategoryID(_ context.Context, _ int64, _ int64, _ int, _ int) ([]*domain.Article, error) {
	return nil, nil
}

func (a adminRepoAdapter) ListByCategoryName(_ context.Context, _ string, _ int64, _ int, _ int) ([]*domain.Article, error) {
	return nil, nil
}

func (a adminRepoAdapter) ListSoftDeleted(_ context.Context, _ time.Time, _ int) ([]*domain.Article, error) {
	return nil, nil
}

func (a adminRepoAdapter) ClaimChunkPending(_ context.Context, _ int, _ time.Duration) ([]*domain.Article, error) {
	return nil, nil
}

func (a adminRepoAdapter) MarkChunkDone(_ context.Context, _ int64) error { return nil }

func (a adminRepoAdapter) MarkChunkError(_ context.Context, _ int64, _ int) error { return nil }

func (a adminRepoAdapter) ClaimEmbedPending(_ context.Context, _ int, _ time.Duration) ([]*domain.Article, error) {
	return nil, nil
}

func (a adminRepoAdapter) MarkEmbedDone(_ context.Context, _ int64) error { return nil }

func (a adminRepoAdapter) MarkEmbedError(_ context.Context, _ int64, _ int) error { return nil }

func (a adminRepoAdapter) HardDelete(_ context.Context, _ int64) error { return nil }

type adminChunkAdapter struct{ f *fakeAdminChunkRepo }

func (a adminChunkAdapter) CreateBatch(_ context.Context, _ []*domain.ArticleChunk) error {
	return nil
}

func (a adminChunkAdapter) ExistsByArticleID(_ context.Context, _ int64) (bool, error) {
	return false, nil
}

func (a adminChunkAdapter) DeleteByArticleID(ctx context.Context, articleID int64) error {
	return a.f.DeleteByArticleID(ctx, articleID)
}

func (a adminChunkAdapter) ListUnembeddedByArticle(_ context.Context, _ int64) ([]*domain.ChunkWithOwner, error) {
	return nil, nil
}

func (a adminChunkAdapter) DeleteStaleChunks(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (a adminChunkAdapter) MarkEmbedded(_ context.Context, _ []int64) error { return nil }

type adminVectorsAdapter struct{ f *fakeAdminVectors }

func (a adminVectorsAdapter) DeleteByArticle(ctx context.Context, articleID int64) error {
	return a.f.DeleteByArticle(ctx, articleID)
}

func (a adminVectorsAdapter) Search(_ context.Context, _ []float32, _ domain.SearchFilter, _ int, _ float32) ([]*domain.ScoredChunk, error) {
	return nil, nil
}

func (a adminVectorsAdapter) SearchText(_ context.Context, _ string, _ domain.SearchFilter, _ int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}

func (a adminVectorsAdapter) UpsertPoints(_ context.Context, _ []*domain.VectorPoint) error {
	return nil
}

func (a adminVectorsAdapter) CountByArticle(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

func (a adminVectorsAdapter) IsPermanentError(_ error) bool { return false }

// Scope rỗng → 400, không đụng repo/vector nào.
func TestRebuildVectors_EmptyScope(t *testing.T) {
	repo := &fakeAdminArticleRepo{}
	svc := newAdminSvc(repo, &fakeAdminChunkRepo{}, &fakeAdminVectors{})
	res := svc.RebuildVectors(context.Background(), &dtos.RebuildVectorsRequest{})
	if res.Err == nil {
		t.Fatal("scope rỗng phải 400")
	}
}

// Theo ids: xóa vector + chunk từng bài rồi reset, trả đúng count.
func TestRebuildVectors_ByIDs(t *testing.T) {
	repo := &fakeAdminArticleRepo{resetN: 2}
	chunks := &fakeAdminChunkRepo{}
	vectors := &fakeAdminVectors{}
	svc := newAdminSvc(repo, chunks, vectors)
	res := svc.RebuildVectors(context.Background(), &dtos.RebuildVectorsRequest{ArticleIDs: []int64{8, 9}})
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	if res.Data.ResetArticles != 2 || len(res.Data.ArticleIDs) != 2 {
		t.Fatalf("response sai: %+v", res.Data)
	}
	if len(vectors.deleted) != 2 || len(chunks.deleted) != 2 || len(repo.resetIDs) != 2 {
		t.Fatalf("thiếu bước dọn: vectors=%v chunks=%v reset=%v", vectors.deleted, chunks.deleted, repo.resetIDs)
	}
	// Thứ tự vector-trước: vector bị xóa trước khi DB reset — check gián tiếp
	// qua việc cả 3 đều chạy đủ, không fail giữa chừng.
}

// Theo category: resolve ids rồi làm như theo ids.
func TestRebuildVectors_ByCategory(t *testing.T) {
	repo := &fakeAdminArticleRepo{idsByCat: map[int64][]int64{5: {8}}, resetN: 1}
	svc := newAdminSvc(repo, &fakeAdminChunkRepo{}, &fakeAdminVectors{})
	res := svc.RebuildVectors(context.Background(), &dtos.RebuildVectorsRequest{CategoryID: 5})
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	if res.Data.ResetArticles != 1 {
		t.Fatalf("response sai: %+v", res.Data)
	}
}

// Category rỗng → 404.
func TestRebuildVectors_EmptyCategory(t *testing.T) {
	repo := &fakeAdminArticleRepo{idsByCat: map[int64][]int64{}}
	svc := newAdminSvc(repo, &fakeAdminChunkRepo{}, &fakeAdminVectors{})
	if res := svc.RebuildVectors(context.Background(), &dtos.RebuildVectorsRequest{CategoryID: 9}); res.Err == nil {
		t.Fatal("category rỗng phải 404")
	}
}

// Lỗi xóa vector thì fail ngay, không reset DB bừa.
func TestRebuildVectors_VectorError(t *testing.T) {
	repo := &fakeAdminArticleRepo{}
	vectors := &fakeAdminVectors{delErr: errors.New("qdrant down")}
	svc := newAdminSvc(repo, &fakeAdminChunkRepo{}, vectors)
	if res := svc.RebuildVectors(context.Background(), &dtos.RebuildVectorsRequest{ArticleIDs: []int64{8}}); res.Err == nil {
		t.Fatal("lỗi vector phải fail")
	}
	if len(repo.resetIDs) != 0 {
		t.Fatal("lỗi vector mà vẫn reset DB")
	}
}
