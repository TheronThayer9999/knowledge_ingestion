package services

// Test logic hội tụ của ChunkPending (không test worker/ticker/pool): fake
// repository + fake storage, assert claim → chunk → done/error + nội dung chunk
// lưu. Fakes có mutex vì service chạy pool song song.
// Chạy: go test -race ./src/controller/services/ ./src/common/extractor/

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/domain"

	"github.com/google/uuid"
)

var errBlobDown = errors.New("blob down")

type fakeChunkArticleRepo struct {
	mu      sync.Mutex
	list    []*domain.Article
	listErr error
	done    []int64
	errs    map[int64]int
	// gone mô phỏng bài bị user xóa giữa chừng — IsAlive luôn false.
	gone bool
}

func (f *fakeChunkArticleRepo) IsAlive(ctx context.Context, id int64) (bool, error) {
	return !f.gone, nil
}

func (f *fakeChunkArticleRepo) ClaimChunkPending(ctx context.Context, limit int, lease time.Duration) ([]*domain.Article, error) {
	return f.list, f.listErr
}
func (f *fakeChunkArticleRepo) MarkChunkDone(ctx context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = append(f.done, id)
	return nil
}
func (f *fakeChunkArticleRepo) MarkChunkError(ctx context.Context, id int64, attempts int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errs == nil {
		f.errs = map[int64]int{}
	}
	f.errs[id] = attempts
	return nil
}

func (f *fakeChunkArticleRepo) Create(ctx context.Context, article *domain.Article) error { return nil }
func (f *fakeChunkArticleRepo) Update(ctx context.Context, article *domain.Article) error { return nil }
func (f *fakeChunkArticleRepo) Delete(ctx context.Context, article *domain.Article) error { return nil }
func (f *fakeChunkArticleRepo) GetByID(ctx context.Context, id int64, userID int64) (*domain.Article, error) {
	return nil, nil
}
func (f *fakeChunkArticleRepo) ExistsByURL(ctx context.Context, url string, userID int64) (bool, error) {
	return false, nil
}
func (f *fakeChunkArticleRepo) ExistsByStorageKey(ctx context.Context, storageKey string, userID int64) (bool, error) {
	return false, nil
}
func (f *fakeChunkArticleRepo) ListByCategoryID(ctx context.Context, categoryID int64, userID int64, limit, offset int) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeChunkArticleRepo) ListByCategoryName(ctx context.Context, categoryName string, userID int64, limit, offset int) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeChunkArticleRepo) ListSoftDeleted(ctx context.Context, before time.Time, limit int) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeChunkArticleRepo) ClaimEmbedPending(ctx context.Context, limit int, lease time.Duration) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeChunkArticleRepo) MarkEmbedDone(ctx context.Context, id int64) error { return nil }
func (f *fakeChunkArticleRepo) MarkEmbedError(ctx context.Context, id int64, attempts int) error {
	return nil
}
func (f *fakeChunkArticleRepo) HardDelete(ctx context.Context, id int64) error { return nil }

type fakeChunkRepo struct {
	mu      sync.Mutex
	saved   []*domain.ArticleChunk
	saveErr error
	batches int
	exists  map[int64]bool
	deleted []int64
}

func (f *fakeChunkRepo) CreateBatch(ctx context.Context, chunks []*domain.ArticleChunk) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.batches++
	f.saved = append(f.saved, chunks...)
	return nil
}

func (f *fakeChunkRepo) ExistsByArticleID(ctx context.Context, articleID int64) (bool, error) {
	return f.exists[articleID], nil
}

func (f *fakeChunkRepo) DeleteByArticleID(ctx context.Context, articleID int64) error {
	f.deleted = append(f.deleted, articleID)
	return nil
}

func (f *fakeChunkRepo) ListUnembeddedByArticle(ctx context.Context, articleID int64) ([]*domain.ChunkWithOwner, error) {
	return nil, nil
}

func (f *fakeChunkRepo) MarkEmbedded(ctx context.Context, ids []int64) error { return nil }
func (f *fakeChunkRepo) DeleteStaleChunks(ctx context.Context, before time.Time) (int64, error) {
	return 0, nil
}

type fakeChunkStorage struct {
	blobs map[string]string
	err   error
}

func (f *fakeChunkStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(strings.NewReader(f.blobs[key])), nil
}

func (f *fakeChunkStorage) Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	return nil
}
func (f *fakeChunkStorage) Delete(ctx context.Context, key string) error { return nil }
func (f *fakeChunkStorage) Exists(ctx context.Context, key string) (bool, error) {
	_, ok := f.blobs[key]
	return ok, nil
}
func (f *fakeChunkStorage) PresignedURL(ctx context.Context, key string, contentType string, expiry time.Duration) (string, error) {
	return "", nil
}

func newChunkTest(articleRepo *fakeChunkArticleRepo, chunkRepo *fakeChunkRepo, store *fakeChunkStorage) IArticleChunkService {
	return NewArticleChunkService(articleRepo, chunkRepo, store, extractor.OCRConfig{})
}

func chunkArticle(id int64, key string) *domain.Article {
	return &domain.Article{
		BaseModel:  domain.BaseModel{ID: id},
		StorageKey: key,
		Name:       "bài test",
	}
}

// Bài txt ngắn → claim xong đánh done, đúng 1 chunk index 0, PointID UUIDv7.
func TestChunkPending_ShortTextOneChunk(t *testing.T) {
	articleRepo := &fakeChunkArticleRepo{list: []*domain.Article{chunkArticle(5, "uploads/a.txt")}}
	chunkRepo := &fakeChunkRepo{exists: map[int64]bool{}}
	store := &fakeChunkStorage{blobs: map[string]string{"uploads/a.txt": "xin chào thế giới"}}
	svc := newChunkTest(articleRepo, chunkRepo, store)

	n, err := svc.ChunkPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 1 {
		t.Fatalf("chunked = %d, want 1", n)
	}
	if len(articleRepo.done) != 1 || articleRepo.done[0] != 5 {
		t.Fatalf("done = %v, want [5]", articleRepo.done)
	}
	if len(chunkRepo.saved) != 1 {
		t.Fatalf("saved = %d chunk, want 1", len(chunkRepo.saved))
	}
	c := chunkRepo.saved[0]
	if c.ArticleID != 5 || c.ChunkIndex != 0 || c.Content != "xin chào thế giới" {
		t.Fatalf("chunk sai: %+v", c)
	}
	if c.PageNum != 0 {
		t.Fatalf("txt gom 1 page Num 0, got %d", c.PageNum)
	}
	pid, err := uuid.Parse(c.PointID)
	if err != nil {
		t.Fatalf("PointID không phải UUID: %q (%v)", c.PointID, err)
	}
	if pid.Version() != 7 {
		t.Fatalf("PointID phải là UUIDv7, got version %d", pid.Version())
	}
}

// Text dài → nhiều chunk, index liên tục 0..n-1, PointID duy nhất.
func TestChunkPending_LongTextManyChunks(t *testing.T) {
	text := strings.Repeat("đoạn văn bản tiếng Việt cần chia nhỏ để embedding. ", 50)
	articleRepo := &fakeChunkArticleRepo{list: []*domain.Article{chunkArticle(6, "uploads/b.txt")}}
	chunkRepo := &fakeChunkRepo{}
	store := &fakeChunkStorage{blobs: map[string]string{"uploads/b.txt": text}}
	svc := newChunkTest(articleRepo, chunkRepo, store)

	n, err := svc.ChunkPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 1 || len(chunkRepo.saved) < 2 {
		t.Fatalf("chunked = %d, saved = %d; want 1 và nhiều chunk", n, len(chunkRepo.saved))
	}
	for i, c := range chunkRepo.saved {
		if c.ChunkIndex != i || c.ArticleID != 6 {
			t.Fatalf("chunk %d sai thứ tự: %+v", i, c)
		}
	}
	seen := map[string]bool{}
	for _, c := range chunkRepo.saved {
		if seen[c.PointID] {
			t.Fatalf("trùng PointID: %q", c.PointID)
		}
		seen[c.PointID] = true
	}
	if len(articleRepo.done) != 1 {
		t.Fatalf("done = %v, want [6]", articleRepo.done)
	}
}

// Download lỗi / file rỗng / file hỏng / OCR tắt → lỗi xác định (blob
// immutable, thử lại cũng vậy) nên failed luôn (attempts=Max), không đốt
// retry. Chỉ bài tốt được done + lưu.
func TestChunkPending_SkipsBadArticles(t *testing.T) {
	articleRepo := &fakeChunkArticleRepo{list: []*domain.Article{
		chunkArticle(7, "uploads/c.txt"),
		chunkArticle(8, "uploads/empty.txt"),
		chunkArticle(9, "uploads/d.pdf"),
		chunkArticle(13, "uploads/h.png"),
	}}
	chunkRepo := &fakeChunkRepo{}
	store := &fakeChunkStorage{blobs: map[string]string{
		"uploads/c.txt":     "nội dung tốt",
		"uploads/empty.txt": "   ",
		"uploads/d.pdf":     "%PDF-1.4 rác",
		"uploads/h.png":     "fake-png",
	}}
	svc := newChunkTest(articleRepo, chunkRepo, store)

	n, err := svc.ChunkPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 1 {
		t.Fatalf("chunked = %d, want 1 (chỉ bài tốt)", n)
	}
	if len(chunkRepo.saved) != 1 || chunkRepo.saved[0].ArticleID != 7 {
		t.Fatalf("chỉ được lưu chunk bài 7, got %+v", chunkRepo.saved)
	}
	if len(articleRepo.done) != 1 || articleRepo.done[0] != 7 {
		t.Fatalf("chỉ done bài 7, got %v", articleRepo.done)
	}
	if articleRepo.errs[8] != utils.MaxQueueAttempts ||
		articleRepo.errs[9] != utils.MaxQueueAttempts ||
		articleRepo.errs[13] != utils.MaxQueueAttempts {
		t.Fatalf("lỗi xác định phải failed luôn (attempts=%d), got %v", utils.MaxQueueAttempts, articleRepo.errs)
	}
}

// Bài đã có chunk (claim trùng do lease race) → bỏ qua, đánh done, không tải.
func TestChunkPending_AlreadyChunked(t *testing.T) {
	articleRepo := &fakeChunkArticleRepo{list: []*domain.Article{chunkArticle(11, "uploads/f.txt")}}
	chunkRepo := &fakeChunkRepo{exists: map[int64]bool{11: true}}
	svc := newChunkTest(articleRepo, chunkRepo, &fakeChunkStorage{})

	n, err := svc.ChunkPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 1 || len(chunkRepo.saved) != 0 {
		t.Fatalf("bài có chunk rồi thì skip: n=%d saved=%d", n, len(chunkRepo.saved))
	}
	if len(articleRepo.done) != 1 {
		t.Fatalf("done = %v, want [11]", articleRepo.done)
	}
}

// Kho chết toàn cục (Download luôn lỗi) → 0 bài, MarkChunkError, không crash.
func TestChunkPending_StorageDown(t *testing.T) {
	articleRepo := &fakeChunkArticleRepo{list: []*domain.Article{chunkArticle(10, "uploads/e.txt")}}
	chunkRepo := &fakeChunkRepo{}
	store := &fakeChunkStorage{err: errBlobDown}
	svc := newChunkTest(articleRepo, chunkRepo, store)

	n, err := svc.ChunkPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 0 || len(chunkRepo.saved) != 0 {
		t.Fatalf("kho chết mà vẫn lưu: n=%d saved=%d", n, len(chunkRepo.saved))
	}
	if articleRepo.errs[10] != 1 {
		t.Fatalf("phải MarkChunkError bài 10, got %v", articleRepo.errs)
	}
}

// Bài bị user xóa giữa chừng (claim xong mới xóa) → chunk vừa tạo bị dọn
// ngay, không để orphan; done vẫn đánh (ngoài thật là no-op trên row đã mất).
func TestChunkPending_DeletedMidway(t *testing.T) {
	articleRepo := &fakeChunkArticleRepo{list: []*domain.Article{chunkArticle(12, "uploads/g.txt")}, gone: true}
	chunkRepo := &fakeChunkRepo{exists: map[int64]bool{}}
	store := &fakeChunkStorage{blobs: map[string]string{"uploads/g.txt": "nội dung"}}
	svc := newChunkTest(articleRepo, chunkRepo, store)

	n, err := svc.ChunkPending(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 1 {
		t.Fatalf("chunked = %d, want 1", n)
	}
	if len(chunkRepo.deleted) != 1 || chunkRepo.deleted[0] != 12 {
		t.Fatalf("phải dọn chunk bài đã xóa, got %v", chunkRepo.deleted)
	}
	if len(articleRepo.errs) != 0 {
		t.Fatalf("xóa giữa chừng không phải lỗi retry, got %v", articleRepo.errs)
	}
}

// Lỗi ở bước claim thì trả lỗi ra ngoài để runner log (không nuốt).
func TestChunkPending_ClaimError(t *testing.T) {
	articleRepo := &fakeChunkArticleRepo{listErr: errBlobDown}
	svc := newChunkTest(articleRepo, &fakeChunkRepo{}, &fakeChunkStorage{})

	if n, err := svc.ChunkPending(context.Background()); err == nil || n != 0 {
		t.Fatalf("n = %d, err = %v; want 0, non-nil", n, err)
	}
}
