package services

// Test logic hội tụ của PurgeDeleted (không test worker/ticker): fake
// repository + fake storage, assert cả THỨ TỰ gọi S3-trước-DB-sau và kịch bản
// retry kỳ N/N+1 trong comment của PurgeDeleted. Chạy: go test -race ./src/controller/services/

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

func newPurgeTest(log *orderLog, repo *fakeArticleRepo, store *fakeStorage) IArticlePurgeService {
	return NewArticlePurgeService(repo, store)
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

// Thứ tự bắt buộc: S3 trước, DB sau — đúng invariant trong comment
// PurgeDeleted (row là con trỏ duy nhất tới blob).
func TestPurgeDeleted_S3BeforeDB(t *testing.T) {
	log := &orderLog{}
	repo := &fakeArticleRepo{log: log, list: []*domain.Article{
		{BaseModel: domain.BaseModel{ID: 5}, StorageKey: "uploads/a.png"},
	}}
	store := &fakeStorage{log: log}
	svc := newPurgeTest(log, repo, store)

	purged, err := svc.PurgeDeleted(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged = %d, want 1", purged)
	}
	equalCalls(t, log.calls, []string{"list", "s3:uploads/a.png", "db:5"})
}

// Kịch bản trong comment: DB chết đúng giữa chừng (kỳ N) rồi sống lại
// (kỳ N+1) — row vẫn bị dọn, S3 delete lần 2 vẫn success nhờ idempotent.
func TestPurgeDeleted_DBDownThenRetryConverges(t *testing.T) {
	log := &orderLog{}
	repo := &fakeArticleRepo{log: log, hardDeleteFails: 1, list: []*domain.Article{
		{BaseModel: domain.BaseModel{ID: 5}, StorageKey: "uploads/a.png"},
	}}
	store := &fakeStorage{log: log}
	svc := newPurgeTest(log, repo, store)

	// Kỳ N: S3 xong, DB lỗi — không báo lỗi ra ngoài, row để kỳ sau.
	if purged, err := svc.PurgeDeleted(context.Background()); err != nil || purged != 0 {
		t.Fatalf("kỳ N: purged = %d, err = %v; want 0, nil", purged, err)
	}
	// Kỳ N+1: DB sống lại — dọn xong.
	if purged, err := svc.PurgeDeleted(context.Background()); err != nil || purged != 1 {
		t.Fatalf("kỳ N+1: purged = %d, err = %v; want 1, nil", purged, err)
	}
	equalCalls(t, log.calls, []string{
		"list", "s3:uploads/a.png", "db:5",
		"list", "s3:uploads/a.png", "db:5",
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
	svc := newPurgeTest(log, repo, store)

	if purged, err := svc.PurgeDeleted(context.Background()); err != nil || purged != 0 {
		t.Fatalf("purged = %d, err = %v; want 0, nil", purged, err)
	}
	equalCalls(t, log.calls, []string{"list", "s3:uploads/b.png"})
	if len(repo.hardDeletes) != 0 {
		t.Fatalf("S3 lỗi mà vẫn xóa DB: %v", repo.hardDeletes)
	}
}

// Bài loại URL không có blob: bỏ qua S3, xóa hẳn row luôn.
func TestPurgeDeleted_URLArticleSkipsS3(t *testing.T) {
	log := &orderLog{}
	repo := &fakeArticleRepo{log: log, list: []*domain.Article{
		{BaseModel: domain.BaseModel{ID: 9}, URL: "https://example.com/x"},
	}}
	store := &fakeStorage{log: log}
	svc := newPurgeTest(log, repo, store)

	if purged, err := svc.PurgeDeleted(context.Background()); err != nil || purged != 1 {
		t.Fatalf("purged = %d, err = %v; want 1, nil", purged, err)
	}
	equalCalls(t, log.calls, []string{"list", "db:9"})
}

// Lỗi ở bước quét thì trả lỗi ra ngoài để runner log (không nuốt).
func TestPurgeDeleted_ListError(t *testing.T) {
	log := &orderLog{}
	repo := &fakeArticleRepo{log: log, listErr: errDBDown}
	store := &fakeStorage{log: log}
	svc := newPurgeTest(log, repo, store)

	if purged, err := svc.PurgeDeleted(context.Background()); err == nil || purged != 0 {
		t.Fatalf("purged = %d, err = %v; want 0, non-nil", purged, err)
	}
	if len(store.deletes) != 0 || len(repo.hardDeletes) != 0 {
		t.Fatalf("quét lỗi mà vẫn đụng S3/DB")
	}
}
