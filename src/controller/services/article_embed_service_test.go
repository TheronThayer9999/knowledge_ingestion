package services

// Test luồng phase 2 EmbedPending: fake article repo (claim) + fake chunk repo
// + fake embedder + fake vector store, assert batch gọi ollama, point dựng
// đúng ID/payload, thứ tự Qdrant-trước-MarkEmbedded và done/error theo bài.
// Fakes có mutex vì service chạy pool song song.
// Chạy: go test -race ./src/controller/services/

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/qdrant"
)

var errEmbedDown = errors.New("ollama down")
var errVectorDown = errors.New("qdrant down")

type fakeEmbedArticleRepo struct {
	mu      sync.Mutex
	list    []*domain.Article
	listErr error
	done    []int64
	errs    map[int64]int
	// gone mô phỏng bài bị user xóa giữa chừng — IsAlive luôn false.
	gone bool
}

func (f *fakeEmbedArticleRepo) IsAlive(ctx context.Context, id int64) (bool, error) {
	return !f.gone, nil
}

func (f *fakeEmbedArticleRepo) ClaimEmbedPending(ctx context.Context, limit int, lease time.Duration) ([]*domain.Article, error) {
	return f.list, f.listErr
}
func (f *fakeEmbedArticleRepo) MarkEmbedDone(ctx context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = append(f.done, id)
	return nil
}
func (f *fakeEmbedArticleRepo) MarkEmbedError(ctx context.Context, id int64, attempts int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errs == nil {
		f.errs = map[int64]int{}
	}
	f.errs[id] = attempts
	return nil
}

func (f *fakeEmbedArticleRepo) Create(ctx context.Context, article *domain.Article) error { return nil }
func (f *fakeEmbedArticleRepo) Update(ctx context.Context, article *domain.Article) error { return nil }
func (f *fakeEmbedArticleRepo) Delete(ctx context.Context, article *domain.Article) error { return nil }
func (f *fakeEmbedArticleRepo) GetByID(ctx context.Context, id int64, userID int64) (*domain.Article, error) {
	return nil, nil
}
func (f *fakeEmbedArticleRepo) ExistsByURL(ctx context.Context, url string, userID int64) (bool, error) {
	return false, nil
}
func (f *fakeEmbedArticleRepo) ExistsByStorageKey(ctx context.Context, storageKey string, userID int64) (bool, error) {
	return false, nil
}
func (f *fakeEmbedArticleRepo) ListByCategoryID(ctx context.Context, categoryID int64, userID int64, limit, offset int) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeEmbedArticleRepo) ListByCategoryName(ctx context.Context, categoryName string, userID int64, limit, offset int) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeEmbedArticleRepo) ListSoftDeleted(ctx context.Context, before time.Time, limit int) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeEmbedArticleRepo) ClaimChunkPending(ctx context.Context, limit int, lease time.Duration) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeEmbedArticleRepo) MarkChunkDone(ctx context.Context, id int64) error { return nil }
func (f *fakeEmbedArticleRepo) MarkChunkError(ctx context.Context, id int64, attempts int) error {
	return nil
}
func (f *fakeEmbedArticleRepo) HardDelete(ctx context.Context, id int64) error { return nil }

type fakeEmbedChunkRepo struct {
	mu      sync.Mutex
	byID    map[int64][]*domain.ChunkWithOwner
	listErr error
	marked  []int64
	markErr error
	deleted []int64
}

func (f *fakeEmbedChunkRepo) CreateBatch(ctx context.Context, chunks []*domain.ArticleChunk) error {
	return nil
}
func (f *fakeEmbedChunkRepo) ExistsByArticleID(ctx context.Context, articleID int64) (bool, error) {
	return false, nil
}
func (f *fakeEmbedChunkRepo) DeleteByArticleID(ctx context.Context, articleID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, articleID)
	return nil
}
func (f *fakeEmbedChunkRepo) ListUnembeddedByArticle(ctx context.Context, articleID int64) ([]*domain.ChunkWithOwner, error) {
	return f.byID[articleID], f.listErr
}
func (f *fakeEmbedChunkRepo) MarkEmbedded(ctx context.Context, ids []int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markErr != nil {
		return f.markErr
	}
	f.marked = append(f.marked, ids...)
	return nil
}
func (f *fakeEmbedChunkRepo) DeleteStaleChunks(ctx context.Context, before time.Time) (int64, error) {
	return 0, nil
}

type fakeEmbedder struct {
	mu      sync.Mutex
	dim     int
	batches [][]string
	err     error
	// short mô phỏng embedder trả thiếu vector (sai contract) — service phải
	// báo lỗi thay vì index mù gây panic cả job.
	short bool
}

func (f *fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	v, err := f.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return v[0], nil
}
func (f *fakeEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.batches = append(f.batches, texts)
	out := make([][]float32, 0, len(texts))
	for i := range texts {
		v := make([]float32, f.dim)
		v[0] = float32(i + 1)
		out = append(out, v)
	}
	if f.short && len(out) > 0 {
		out = out[:len(out)-1]
	}
	return out, nil
}
func (f *fakeEmbedder) Dimension() int                  { return f.dim }
func (f *fakeEmbedder) ModelName() string               { return "test-model" }
func (f *fakeEmbedder) IsPermanentError(err error) bool { return false }

type fakeVectorStore struct {
	mu        sync.Mutex
	points    []domain.VectorPoint
	upsertErr error
	// permanent mô phỏng vector store báo lỗi vĩnh viễn (sai dim...) — service
	// phải failed luôn thay vì backoff.
	permanent bool
	deleted   []int64
}

func (f *fakeVectorStore) UpsertPoints(ctx context.Context, points []domain.VectorPoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.points = append(f.points, points...)
	return nil
}
func (f *fakeVectorStore) DeleteByArticle(ctx context.Context, articleID int64) error {
	f.deleted = append(f.deleted, articleID)
	return nil
}
func (f *fakeVectorStore) IsPermanentError(err error) bool { return f.permanent }
func (f *fakeVectorStore) CountByArticle(ctx context.Context, articleID int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, p := range f.points {
		if p.ArticleID == articleID {
			n++
		}
	}
	return n, nil
}

// fakeUnitOfWork chạy fn trực tiếp (không tx thật) — đủ để test composition
// done + trim trong finishEmbed vì thứ tự và lỗi đã được assert qua fakes.
type fakeUnitOfWork struct{}

func (fakeUnitOfWork) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func embedChunk(id, articleID int64, index int, text string) *domain.ChunkWithOwner {
	return &domain.ChunkWithOwner{
		Chunk: &domain.ArticleChunk{
			BaseModel:  domain.BaseModel{ID: id},
			ArticleID:  articleID,
			ChunkIndex: index,
			Content:    text,
			PointID:    fmt.Sprintf("point-%d-%d", articleID, index),
		},
		UserID:     7,
		CategoryID: 3,
	}
}

func newEmbedTest(articles []*domain.Article, chunks map[int64][]*domain.ChunkWithOwner) (*fakeEmbedArticleRepo, *fakeEmbedChunkRepo, *fakeEmbedder, *fakeVectorStore, IArticleEmbedService) {
	articleRepo := &fakeEmbedArticleRepo{list: articles}
	chunkRepo := &fakeEmbedChunkRepo{byID: chunks}
	embedder := &fakeEmbedder{dim: 4}
	vectors := &fakeVectorStore{}
	return articleRepo, chunkRepo, embedder, vectors, NewArticleEmbedService(articleRepo, chunkRepo, embedder, vectors, fakeUnitOfWork{})
}

func embedArticle(id int64) *domain.Article {
	return &domain.Article{BaseModel: domain.BaseModel{ID: id}, StorageKey: "uploads/x.txt"}
}

// 1 bài 2 chunk → 1 lần gọi embed, upsert 2 point đúng ID/payload, mark đủ, done bài.
func TestEmbedPending_OneArticle(t *testing.T) {
	articleRepo, chunkRepo, embedder, vectors, svc := newEmbedTest(
		[]*domain.Article{embedArticle(5)},
		map[int64][]*domain.ChunkWithOwner{5: {
			embedChunk(11, 5, 0, "đoạn một"),
			embedChunk(12, 5, 1, "đoạn hai"),
		}},
	)

	n, err := svc.EmbedPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 2 {
		t.Fatalf("embedded = %d, want 2", n)
	}
	if len(embedder.batches) != 1 || len(embedder.batches[0]) != 2 {
		t.Fatalf("phải gọi embed 1 lần với 2 text, got %v", embedder.batches)
	}
	if len(vectors.points) != 2 {
		t.Fatalf("points = %d, want 2", len(vectors.points))
	}
	p := vectors.points[0]
	if p.ID != "point-5-0" || p.Text != "đoạn một" || p.ArticleID != 5 ||
		p.UserID != 7 || p.CategoryID != 3 || p.ChunkIndex != 0 {
		t.Fatalf("point sai: %+v", p)
	}
	if len(chunkRepo.marked) != 2 {
		t.Fatalf("marked = %v, want [11 12]", chunkRepo.marked)
	}
	if len(articleRepo.done) != 1 || articleRepo.done[0] != 5 {
		t.Fatalf("done = %v, want [5]", articleRepo.done)
	}
	if len(chunkRepo.deleted) != 1 || chunkRepo.deleted[0] != 5 {
		t.Fatalf("finishEmbed phải dọn chunk bài 5 trong cùng InTx, got %v", chunkRepo.deleted)
	}
}

// 40 chunk 1 bài → embed chia 2 đợt (32 + 8), upsert + mark theo đợt.
func TestEmbedPending_SplitsBatches(t *testing.T) {
	var items []*domain.ChunkWithOwner
	for i := 0; i < 40; i++ {
		items = append(items, embedChunk(int64(100+i), 6, i, fmt.Sprintf("chunk %d", i)))
	}
	articleRepo, chunkRepo, embedder, _, svc := newEmbedTest(
		[]*domain.Article{embedArticle(6)},
		map[int64][]*domain.ChunkWithOwner{6: items},
	)

	n, err := svc.EmbedPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 40 {
		t.Fatalf("embedded = %d, want 40", n)
	}
	if len(embedder.batches) != 2 || len(embedder.batches[0]) != 32 || len(embedder.batches[1]) != 8 {
		t.Fatalf("phải chia 32+8, got %v", batchLens(embedder.batches))
	}
	if len(chunkRepo.marked) != 40 {
		t.Fatalf("marked = %d, want 40", len(chunkRepo.marked))
	}
	if len(articleRepo.done) != 1 {
		t.Fatalf("done = %v, want [6]", articleRepo.done)
	}
	if len(chunkRepo.deleted) != 1 || chunkRepo.deleted[0] != 6 {
		t.Fatalf("finishEmbed phải dọn chunk bài 6 trong cùng InTx, got %v", chunkRepo.deleted)
	}
}

func batchLens(b [][]string) []int {
	out := make([]int, 0, len(b))
	for _, x := range b {
		out = append(out, len(x))
	}
	return out
}

// Bài không còn chunk dở nhưng Qdrant đã có point (crash sau upsert/mark
// mà trước done) → done luôn.
func TestEmbedPending_NothingLeftMarksDone(t *testing.T) {
	articleRepo, chunkRepo, _, vectors, svc := newEmbedTest(
		[]*domain.Article{embedArticle(7)},
		map[int64][]*domain.ChunkWithOwner{},
	)
	vectors.points = append(vectors.points, domain.VectorPoint{ID: "point-7-0", ArticleID: 7})

	n, err := svc.EmbedPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 0 {
		t.Fatalf("embedded = %d, want 0", n)
	}
	if len(articleRepo.done) != 1 {
		t.Fatalf("done = %v, want [7]", articleRepo.done)
	}
	if len(chunkRepo.deleted) != 1 || chunkRepo.deleted[0] != 7 {
		t.Fatalf("nhánh rỗng cũng phải trim rồi done, got %v", chunkRepo.deleted)
	}
}

// Bài không còn chunk dở mà Qdrant cũng 0 point (chunk mất/vector chưa từng
// lên) → KHÔNG done, đánh lỗi retry thay vì done âm thầm với search trống.
func TestEmbedPending_EmptyNoVectorsRetries(t *testing.T) {
	articleRepo, _, _, _, svc := newEmbedTest(
		[]*domain.Article{embedArticle(12)},
		map[int64][]*domain.ChunkWithOwner{},
	)

	n, err := svc.EmbedPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 0 {
		t.Fatalf("embedded = %d, want 0", n)
	}
	if len(articleRepo.done) != 0 {
		t.Fatalf("0 point mà vẫn done, got %v", articleRepo.done)
	}
	if articleRepo.errs[12] != 1 {
		t.Fatalf("phải MarkEmbedError attempts=1, got %v", articleRepo.errs)
	}
}

// Ollama chết: bài lỗi MarkEmbedError, bài khác vẫn xong (pool cô lập).
func TestEmbedPending_EmbedErrorIsolatesArticle(t *testing.T) {
	articleRepo := &fakeEmbedArticleRepo{list: []*domain.Article{embedArticle(8)}}
	chunkRepo := &fakeEmbedChunkRepo{byID: map[int64][]*domain.ChunkWithOwner{
		8: {embedChunk(21, 8, 0, "bài hỏng")},
	}}
	embedder := &fakeEmbedder{dim: 4, err: errEmbedDown}
	vectors := &fakeVectorStore{}
	svc := NewArticleEmbedService(articleRepo, chunkRepo, embedder, vectors, fakeUnitOfWork{})

	n, err := svc.EmbedPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 0 || len(vectors.points) != 0 || len(chunkRepo.marked) != 0 {
		t.Fatalf("ollama chết mà vẫn upsert/mark: n=%d", n)
	}
	if articleRepo.errs[8] != 1 {
		t.Fatalf("phải MarkEmbedError attempts=1, got %v", articleRepo.errs)
	}
	if len(articleRepo.done) != 0 {
		t.Fatalf("không được done, got %v", articleRepo.done)
	}
}

// Qdrant lỗi → KHÔNG mark chunk, MarkEmbedError bài, kỳ sau làm lại.
func TestEmbedPending_VectorErrorNoMark(t *testing.T) {
	articleRepo := &fakeEmbedArticleRepo{list: []*domain.Article{embedArticle(9)}}
	chunkRepo := &fakeEmbedChunkRepo{byID: map[int64][]*domain.ChunkWithOwner{
		9: {embedChunk(31, 9, 0, "đoạn")},
	}}
	svc := NewArticleEmbedService(articleRepo, chunkRepo, &fakeEmbedder{dim: 4}, &fakeVectorStore{upsertErr: errVectorDown}, fakeUnitOfWork{})

	n, err := svc.EmbedPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 0 || len(chunkRepo.marked) != 0 {
		t.Fatalf("qdrant lỗi mà vẫn mark: n=%d marked=%v", n, chunkRepo.marked)
	}
	if articleRepo.errs[9] != 1 {
		t.Fatalf("phải MarkEmbedError bài 9, got %v", articleRepo.errs)
	}
}

// Qdrant báo sai request vĩnh viễn (sai dim) → failed luôn, không backoff.
func TestEmbedPending_PermanentVectorErrorFailsFast(t *testing.T) {
	articleRepo := &fakeEmbedArticleRepo{list: []*domain.Article{embedArticle(13)}}
	chunkRepo := &fakeEmbedChunkRepo{byID: map[int64][]*domain.ChunkWithOwner{
		13: {embedChunk(61, 13, 0, "đoạn")},
	}}
	svc := NewArticleEmbedService(articleRepo, chunkRepo, &fakeEmbedder{dim: 4},
		&fakeVectorStore{upsertErr: fmt.Errorf("upsert: %w", qdrant.ErrBadRequest), permanent: true}, fakeUnitOfWork{})

	n, err := svc.EmbedPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 0 || len(chunkRepo.marked) != 0 {
		t.Fatalf("lỗi vĩnh viễn mà vẫn mark: n=%d marked=%v", n, chunkRepo.marked)
	}
	if articleRepo.errs[13] != domain.MaxQueueAttempts {
		t.Fatalf("phải failed luôn, got %v", articleRepo.errs)
	}
}

// Bài bị user xóa giữa chừng → dừng ngay không gọi ollama, dọn vector +
// chunk, không done cũng không đánh lỗi retry.
func TestEmbedPending_DeletedMidway(t *testing.T) {
	articleRepo := &fakeEmbedArticleRepo{list: []*domain.Article{embedArticle(10)}, gone: true}
	chunkRepo := &fakeEmbedChunkRepo{byID: map[int64][]*domain.ChunkWithOwner{
		10: {embedChunk(41, 10, 0, "đoạn")},
	}}
	embedder := &fakeEmbedder{dim: 4}
	vectors := &fakeVectorStore{}
	svc := NewArticleEmbedService(articleRepo, chunkRepo, embedder, vectors, fakeUnitOfWork{})

	n, err := svc.EmbedPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 0 {
		t.Fatalf("embedded = %d, want 0", n)
	}
	if len(embedder.batches) != 0 {
		t.Fatalf("bài chết rồi còn gọi ollama: %v", embedder.batches)
	}
	if len(vectors.deleted) != 1 || vectors.deleted[0] != 10 {
		t.Fatalf("phải dọn vector bài đã xóa, got %v", vectors.deleted)
	}
	if len(chunkRepo.deleted) != 1 || chunkRepo.deleted[0] != 10 {
		t.Fatalf("phải dọn chunk bài đã xóa, got %v", chunkRepo.deleted)
	}
	if len(articleRepo.done) != 0 || len(articleRepo.errs) != 0 {
		t.Fatalf("không done cũng không retry, got done=%v errs=%v", articleRepo.done, articleRepo.errs)
	}
}

// Embedder trả thiếu vector → lỗi contract vĩnh viễn: failed luôn thay vì
// backoff, không upsert/mark gì, không panic.
func TestEmbedPending_ShortVectorsNoPanic(t *testing.T) {
	articleRepo := &fakeEmbedArticleRepo{list: []*domain.Article{embedArticle(11)}}
	chunkRepo := &fakeEmbedChunkRepo{byID: map[int64][]*domain.ChunkWithOwner{
		11: {embedChunk(51, 11, 0, "một"), embedChunk(52, 11, 1, "hai")},
	}}
	vectors := &fakeVectorStore{}
	svc := NewArticleEmbedService(articleRepo, chunkRepo, &fakeEmbedder{dim: 4, short: true}, vectors, fakeUnitOfWork{})

	n, err := svc.EmbedPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 0 || len(vectors.points) != 0 || len(chunkRepo.marked) != 0 {
		t.Fatalf("thiếu vector mà vẫn upsert/mark: n=%d", n)
	}
	if articleRepo.errs[11] != domain.MaxQueueAttempts {
		t.Fatalf("lỗi contract phải failed luôn, got %v", articleRepo.errs)
	}
}

// Lỗi ở bước claim thì trả lỗi ra ngoài để runner log (không nuốt).
func TestEmbedPending_ClaimError(t *testing.T) {
	articleRepo := &fakeEmbedArticleRepo{listErr: errEmbedDown}
	svc := NewArticleEmbedService(articleRepo, &fakeEmbedChunkRepo{}, &fakeEmbedder{dim: 4}, &fakeVectorStore{}, fakeUnitOfWork{})

	if n, err := svc.EmbedPending(context.Background()); err == nil || n != 0 {
		t.Fatalf("n = %d, err = %v; want 0, non-nil", n, err)
	}
}
