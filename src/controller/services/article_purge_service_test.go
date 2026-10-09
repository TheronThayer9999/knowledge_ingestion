package services

// Test logic hội tụ của PurgeDeleted (không test worker/ticker): fake
// repository + fake storage/vector, assert cả THỨ TỰ gọi blob → vector →
// chunk → row và kịch bản retry kỳ N/N+1 trong comment của PurgeDeleted.
// Chạy: go test -race ./src/controller/services/

import (
	"context"
	"errors"
	"fmt"
	"io"
	"knowledge_ingestion/src/domain"
	"testing"
	"time"
)

var errDBDown = errors.New("db down")
var errS3Down = errors.New("s3 down")

// orderLog ghi thứ tự gọi xuyên 2 fake — cả 2 cùng append vào 1 slice chung
// để test assert được "S3 trước, DB sau".
type orderLog struct {
	calls []string
}

func (l *orderLog) add(call string) { l.calls = append(l.calls, call) }

type fakeArticleRepo struct {
	log *orderLog
	// list là hàng đợi ListSoftDeleted trả về; listErr lỗi của List.
	list    []*domain.Article
	listErr error
	// hardDeleteFails số lần HardDelete đầu tiên sẽ lỗi (mô phỏng DB chết
	// giữa chừng), các lần sau success.
	hardDeleteFails int
	hardDeletes     []int64
}

func (f *fakeArticleRepo) ListSoftDeleted(ctx context.Context, before time.Time, limit int) ([]*domain.Article, error) {
	f.log.add("list")
	return f.list, f.listErr
}

// Các method queue chunk/embed chỉ phục vụ worker chunk/embed — purge không
// gọi nên stub rỗng.
func (f *fakeArticleRepo) ClaimChunkPending(ctx context.Context, limit int, lease time.Duration) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeArticleRepo) MarkChunkDone(ctx context.Context, id int64) error { return nil }
func (f *fakeArticleRepo) MarkChunkError(ctx context.Context, id int64, attempts int) error {
	return nil
}
func (f *fakeArticleRepo) ClaimEmbedPending(ctx context.Context, limit int, lease time.Duration) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeArticleRepo) MarkEmbedDone(ctx context.Context, id int64) error { return nil }
func (f *fakeArticleRepo) MarkEmbedError(ctx context.Context, id int64, attempts int) error {
	return nil
}

func (f *fakeArticleRepo) HardDelete(ctx context.Context, id int64) error {
	f.log.add(fmt.Sprintf("db:%d", id))
	f.hardDeletes = append(f.hardDeletes, id)
	if f.hardDeleteFails > 0 {
		f.hardDeleteFails--
		return errDBDown
	}
	return nil
}

func (f *fakeArticleRepo) Create(ctx context.Context, article *domain.Article) error { return nil }
func (f *fakeArticleRepo) Update(ctx context.Context, article *domain.Article) error { return nil }
func (f *fakeArticleRepo) Delete(ctx context.Context, article *domain.Article) error { return nil }
func (f *fakeArticleRepo) GetByID(ctx context.Context, id int64, userID int64) (*domain.Article, error) {
	return nil, nil
}

// IsAlive không dùng trong purge — stub.
func (f *fakeArticleRepo) IsAlive(ctx context.Context, id int64) (bool, error) {
	return true, nil
}
func (f *fakeArticleRepo) ExistsByURL(ctx context.Context, url string, userID int64) (bool, error) {
	return false, nil
}
func (f *fakeArticleRepo) ExistsByStorageKey(ctx context.Context, storageKey string, userID int64) (bool, error) {
	return false, nil
}
func (f *fakeArticleRepo) ListByCategoryID(ctx context.Context, categoryID int64, userID int64, limit, offset int) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeArticleRepo) ListByCategoryName(ctx context.Context, categoryName string, userID int64, limit, offset int) ([]*domain.Article, error) {
	return nil, nil
}

type fakeStorage struct {
	log *orderLog
	// deleteErr lỗi S3.Delete trả về (nil = success, kể cả key đã mất —
	// đúng tính idempotent của S3 thật).
	deleteErr error
	deletes   []string
}

func (f *fakeStorage) Delete(ctx context.Context, key string) error {
	f.log.add(fmt.Sprintf("s3:%s", key))
	f.deletes = append(f.deletes, key)
	return f.deleteErr
}

func (f *fakeStorage) Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	return nil
}
func (f *fakeStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return nil, nil
}
func (f *fakeStorage) Exists(ctx context.Context, key string) (bool, error) { return true, nil }
func (f *fakeStorage) PresignedURL(ctx context.Context, key string, contentType string, expiry time.Duration) (string, error) {
	return "", nil
}

// fakePurgeChunkRepo ghi thứ tự xóa chunk để assert pipeline dọn
// blob → vector → chunk → row.
type fakePurgeChunkRepo struct {
	log *orderLog
	// deleteErr lỗi DeleteByArticleID trả về (mô phỏng DB chết lúc dọn chunk).
	deleteErr error
	deletes   []int64
}

func (f *fakePurgeChunkRepo) CreateBatch(ctx context.Context, chunks []*domain.ArticleChunk) error {
	return nil
}
func (f *fakePurgeChunkRepo) ExistsByArticleID(ctx context.Context, articleID int64) (bool, error) {
	return false, nil
}
func (f *fakePurgeChunkRepo) DeleteByArticleID(ctx context.Context, articleID int64) error {
	f.log.add(fmt.Sprintf("chunks:%d", articleID))
	f.deletes = append(f.deletes, articleID)
	return f.deleteErr
}
func (f *fakePurgeChunkRepo) ListUnembeddedByArticle(ctx context.Context, articleID int64) ([]*domain.ChunkWithOwner, error) {
	return nil, nil
}
func (f *fakePurgeChunkRepo) MarkEmbedded(ctx context.Context, ids []int64) error { return nil }
func (f *fakePurgeChunkRepo) DeleteStaleChunks(ctx context.Context, before time.Time) (int64, error) {
	f.log.add("trim-old")
	return int64(len(f.deletes)), nil
}

// fakeVectors ghi thứ tự xóa vector Qdrant.
type fakeVectors struct {
	log *orderLog
	// deleteErr lỗi DeleteByArticle trả về (mô phỏng Qdrant chết).
	deleteErr error
	deletes   []int64
}

func (f *fakeVectors) UpsertPoints(ctx context.Context, points []*domain.VectorPoint) error {
	return nil
}
func (f *fakeVectors) DeleteByArticle(ctx context.Context, articleID int64) error {
	f.log.add(fmt.Sprintf("vec:%d", articleID))
	f.deletes = append(f.deletes, articleID)
	return f.deleteErr
}
func (f *fakeVectors) CountByArticle(ctx context.Context, articleID int64) (int64, error) {
	return 0, nil
}
func (f *fakeVectors) IsPermanentError(err error) bool { return false }

func newPurgeTest(log *orderLog, repo *fakeArticleRepo, chunks *fakePurgeChunkRepo, vectors *fakeVectors, store *fakeStorage) IArticlePurgeService {
	return NewArticlePurgeService(repo, chunks, vectors, store, PurgeOptions{ChunkRetention: time.Hour, TrimInterval: time.Hour})
}

func equalCalls(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("số lần gọi sai: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("thứ tự gọi sai tại [%d]: got %v, want %v", i, got, want)
		}
	}
}

// Thứ tự bắt buộc: blob → vector → chunk → row — đúng invariant trong comment
// PurgeDeleted (row là con trỏ duy nhất tới blob; vector/chunk là dẫn xuất).
func TestPurgeDeleted_S3BeforeDB(t *testing.T) {
	log := &orderLog{}
	repo := &fakeArticleRepo{log: log, list: []*domain.Article{
		{BaseModel: domain.BaseModel{ID: 5}, StorageKey: "uploads/a.png"},
	}}
	store := &fakeStorage{log: log}
	svc := newPurgeTest(log, repo, &fakePurgeChunkRepo{log: log}, &fakeVectors{log: log}, store)

	purged, err := svc.PurgeDeleted(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged = %d, want 1", purged)
	}
	equalCalls(t, log.calls, []string{"list", "s3:uploads/a.png", "vec:5", "chunks:5", "db:5"})
}

// Kịch bản trong comment: DB chết đúng giữa chừng (kỳ N) rồi sống lại
// (kỳ N+1) — row vẫn bị dọn, S3 delete lần 2 vẫn success nhờ idempotent.
func TestPurgeDeleted_DBDownThenRetryConverges(t *testing.T) {
	log := &orderLog{}
	repo := &fakeArticleRepo{log: log, hardDeleteFails: 1, list: []*domain.Article{
		{BaseModel: domain.BaseModel{ID: 5}, StorageKey: "uploads/a.png"},
	}}
	store := &fakeStorage{log: log}
	svc := newPurgeTest(log, repo, &fakePurgeChunkRepo{log: log}, &fakeVectors{log: log}, store)

	// Kỳ N: S3 xong, DB lỗi — không báo lỗi ra ngoài, row để kỳ sau.
	if purged, err := svc.PurgeDeleted(context.Background()); err != nil || purged != 0 {
		t.Fatalf("kỳ N: purged = %d, err = %v; want 0, nil", purged, err)
	}
	// Kỳ N+1: DB sống lại — dọn xong.
	if purged, err := svc.PurgeDeleted(context.Background()); err != nil || purged != 1 {
		t.Fatalf("kỳ N+1: purged = %d, err = %v; want 1, nil", purged, err)
	}
	equalCalls(t, log.calls, []string{
		"list", "s3:uploads/a.png", "vec:5", "chunks:5", "db:5",
		"list", "s3:uploads/a.png", "vec:5", "chunks:5", "db:5",
	})
	if len(store.deletes) != 2 || store.deletes[0] != store.deletes[1] {
		t.Fatalf("S3 phải bị gọi lại đúng key cũ, got %v", store.deletes)
	}
}

// S3 lỗi thì KHÔNG được đụng tới DB — row giữ nguyên để kỳ sau thử lại từ đầu.
func TestPurgeDeleted_S3FailsSkipsDB(t *testing.T) {
	log := &orderLog{}
	repo := &fakeArticleRepo{log: log, list: []*domain.Article{
		{BaseModel: domain.BaseModel{ID: 7}, StorageKey: "uploads/b.png"},
	}}
	store := &fakeStorage{log: log, deleteErr: errS3Down}
	svc := newPurgeTest(log, repo, &fakePurgeChunkRepo{log: log}, &fakeVectors{log: log}, store)

	if purged, err := svc.PurgeDeleted(context.Background()); err != nil || purged != 0 {
		t.Fatalf("purged = %d, err = %v; want 0, nil", purged, err)
	}
	equalCalls(t, log.calls, []string{"list", "s3:uploads/b.png"})
	if len(repo.hardDeletes) != 0 {
		t.Fatalf("S3 lỗi mà vẫn xóa DB: %v", repo.hardDeletes)
	}
}

// Qdrant lỗi thì KHÔNG được đụng tới chunk/row — để kỳ sau thử lại từ đầu.
func TestPurgeDeleted_VectorFailsSkipsRest(t *testing.T) {
	log := &orderLog{}
	repo := &fakeArticleRepo{log: log, list: []*domain.Article{
		{BaseModel: domain.BaseModel{ID: 8}, StorageKey: "uploads/c.png"},
	}}
	store := &fakeStorage{log: log}
	chunks := &fakePurgeChunkRepo{log: log}
	svc := newPurgeTest(log, repo, chunks, &fakeVectors{log: log, deleteErr: errS3Down}, store)

	if purged, err := svc.PurgeDeleted(context.Background()); err != nil || purged != 0 {
		t.Fatalf("purged = %d, err = %v; want 0, nil", purged, err)
	}
	equalCalls(t, log.calls, []string{"list", "s3:uploads/c.png", "vec:8"})
	if len(chunks.deletes) != 0 || len(repo.hardDeletes) != 0 {
		t.Fatalf("vector lỗi mà vẫn xóa chunk/row")
	}
}

// Bài loại URL không có blob: bỏ qua S3, dọn vector + chunk + row luôn.
func TestPurgeDeleted_URLArticleSkipsS3(t *testing.T) {
	log := &orderLog{}
	repo := &fakeArticleRepo{log: log, list: []*domain.Article{
		{BaseModel: domain.BaseModel{ID: 9}, URL: "https://example.com/x"},
	}}
	store := &fakeStorage{log: log}
	svc := newPurgeTest(log, repo, &fakePurgeChunkRepo{log: log}, &fakeVectors{log: log}, store)

	if purged, err := svc.PurgeDeleted(context.Background()); err != nil || purged != 1 {
		t.Fatalf("purged = %d, err = %v; want 1, nil", purged, err)
	}
	equalCalls(t, log.calls, []string{"list", "vec:9", "chunks:9", "db:9"})
}

// Lỗi ở bước quét thì trả lỗi ra ngoài để runner log (không nuốt).
func TestPurgeDeleted_ListError(t *testing.T) {
	log := &orderLog{}
	repo := &fakeArticleRepo{log: log, listErr: errDBDown}
	store := &fakeStorage{log: log}
	svc := newPurgeTest(log, repo, &fakePurgeChunkRepo{log: log}, &fakeVectors{log: log}, store)

	if purged, err := svc.PurgeDeleted(context.Background()); err == nil || purged != 0 {
		t.Fatalf("purged = %d, err = %v; want 0, non-nil", purged, err)
	}
	if len(store.deletes) != 0 || len(repo.hardDeletes) != 0 {
		t.Fatalf("quét lỗi mà vẫn đụng S3/DB")
	}
}

// TrimOldChunks gọi repo đúng 1 lần, trả số row dọn được — không đụng S3,
// vector hay article row.
func TestTrimOldChunks_DelegatesToRepo(t *testing.T) {
	log := &orderLog{}
	repo := &fakeArticleRepo{log: log}
	store := &fakeStorage{log: log}
	chunks := &fakePurgeChunkRepo{log: log, deletes: []int64{1, 2}}
	svc := newPurgeTest(log, repo, chunks, &fakeVectors{log: log}, store)

	n, err := svc.TrimOldChunks(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 2 {
		t.Fatalf("trimmed = %d, want 2", n)
	}
	equalCalls(t, log.calls, []string{"trim-old"})
	if len(store.deletes) != 0 || len(repo.hardDeletes) != 0 {
		t.Fatalf("trim chunk mà vẫn đụng S3/row")
	}
}
