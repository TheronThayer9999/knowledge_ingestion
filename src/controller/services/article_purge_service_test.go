package services

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"knowledge_ingestion/src/domain"
)

// TestPurgeDeleted khoá thứ tự dọn của janitor, thứ mà codebase rất coi
// trọng: xoá việc ngoài (blob S3, vector Qdrant) TRƯỚC rồi mới xoá row
// Postgres. Xoá ngược lại thì khi DB chết đúng giữa chừng, blob đã mất nhưng
// row còn, và không còn con trỏ nào để kỳ sau tìm lại — mồ côi vĩnh viễn.

type purgeStubArticleRepo struct {
	articles []*domain.Article
	deleted  [][]int64
}

func (r *purgeStubArticleRepo) ListSoftDeleted(_ context.Context, _ time.Time, _ int) ([]*domain.Article, error) {
	return r.articles, nil
}

func (r *purgeStubArticleRepo) HardDeleteMany(_ context.Context, ids []int64) error {
	r.deleted = append(r.deleted, append([]int64(nil), ids...))
	return nil
}

// purgeStubPageRepo đếm lần gọi + id nhận vào cho hàm xoá hàng loạt.
type purgeStubPageRepo struct {
	deleteManyCalls [][]int64
}

func (r *purgeStubPageRepo) DeleteByArticles(_ context.Context, ids []int64) error {
	r.deleteManyCalls = append(r.deleteManyCalls, append([]int64(nil), ids...))
	return nil
}

func (r *purgeStubPageRepo) DeleteOrphan(context.Context) (int64, error)    { return 0, nil }
func (r *purgeStubPageRepo) DeleteByArticleID(context.Context, int64) error { return nil }
func (r *purgeStubPageRepo) CreateBatch(context.Context, []*domain.ArticlePage) error {
	return nil
}
func (r *purgeStubPageRepo) Progress(context.Context, int64) (*domain.PageProgress, error) {
	return &domain.PageProgress{DoneSeq: -1}, nil
}
func (r *purgeStubPageRepo) ListByArticle(context.Context, int64) ([]*domain.ArticlePage, error) {
	return nil, nil
}
func (r *purgeStubPageRepo) ListPagesRange(context.Context, int64, int, int) ([]*domain.ArticlePage, error) {
	return nil, nil
}

// purgeStubSections đếm lần xóa article_sections theo lô (phase 1b).
type purgeStubSections struct {
	domain.IArticleSectionRepository
	deleteManyCalls [][]int64
}

func (r *purgeStubSections) DeleteByArticles(_ context.Context, ids []int64) error {
	r.deleteManyCalls = append(r.deleteManyCalls, append([]int64(nil), ids...))
	return nil
}

// purgeStubVectors ghi lại bài nào đã xoá vector, và fail đúng 1 id.
type purgeStubVectors struct {
	deleted []int64
	failFor int64
}

func (v *purgeStubVectors) DeleteByArticle(_ context.Context, articleID int64) error {
	if articleID == v.failFor {
		return errors.New("qdrant chết")
	}
	v.deleted = append(v.deleted, articleID)
	return nil
}

func (v *purgeStubVectors) DeleteByArticles(context.Context, []int64) error { return nil }
func (v *purgeStubVectors) UpsertPoints(context.Context, []*domain.VectorPoint) error {
	return nil
}
func (v *purgeStubVectors) CountByArticle(context.Context, int64) (int64, error) {
	return 0, nil
}
func (v *purgeStubVectors) Search(context.Context, []float32, domain.SearchFilter, int, float32) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v *purgeStubVectors) SearchText(context.Context, string, domain.SearchFilter, int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v *purgeStubVectors) ScrollNeighbors(context.Context, domain.SearchFilter, int, int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v *purgeStubVectors) ScrollRange(context.Context, domain.SearchFilter, int, int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v *purgeStubVectors) ListSections(context.Context, domain.SearchFilter) ([]domain.SectionSpan, error) {
	return nil, nil
}
func (v *purgeStubVectors) IsPermanentError(error) bool { return false }

// purgeStubStorage fail đúng 1 key.
type purgeStubStorage struct {
	failFor string
}

func (s *purgeStubStorage) Delete(_ context.Context, key string) error {
	if key == s.failFor {
		return errors.New("s3 chết")
	}
	return nil
}

func (s *purgeStubStorage) Download(context.Context, string) (io.ReadCloser, error) {
	return nil, nil
}
func (s *purgeStubStorage) Upload(context.Context, string, io.Reader, int64, string) error {
	return nil
}
func (s *purgeStubStorage) Exists(context.Context, string) (bool, error) { return true, nil }
func (s *purgeStubStorage) PresignedURL(context.Context, string, string, time.Duration) (string, error) {
	return "", nil
}

func TestPurgeDeleted(t *testing.T) {
	tests := []struct {
		name         string
		articles     []*domain.Article
		failBlobKey  string
		failVectorID int64
		wantPurged   int
	}{
		{
			name: "dọn hết khi không có lỗi",
			articles: []*domain.Article{
				{BaseModel: domain.BaseModel{ID: 1}, StorageKey: "uploads/a.pdf"},
				{BaseModel: domain.BaseModel{ID: 2}, StorageKey: "uploads/b.pdf"},
			},
			wantPurged: 2,
		},
		{
			name: "blob hỏng — bài đó chưa purge, bài khác vẫn chạy",
			articles: []*domain.Article{
				{BaseModel: domain.BaseModel{ID: 1}, StorageKey: "uploads/hong.pdf"},
				{BaseModel: domain.BaseModel{ID: 2}, StorageKey: "uploads/ok.pdf"},
			},
			failBlobKey: "uploads/hong.pdf",
			wantPurged:  1,
		},
		{
			name: "qdrant chết — không purge bài nào, chờ kỳ sau",
			articles: []*domain.Article{
				{BaseModel: domain.BaseModel{ID: 1}, StorageKey: "uploads/a.pdf"},
			},
			failVectorID: 1,
			wantPurged:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			artRepo := &purgeStubArticleRepo{articles: tc.articles}
			pageRepo := &purgeStubPageRepo{}
			sectionRepo := &purgeStubSections{}
			vectors := &purgeStubVectors{failFor: tc.failVectorID}
			st := &purgeStubStorage{failFor: tc.failBlobKey}

			svc := NewArticlePurgeService(artRepo, pageRepo, sectionRepo, vectors, st, PurgeOptions{})
			n, err := svc.PurgeDeleted(context.Background())
			if err != nil {
				t.Fatalf("PurgeDeleted lỗi: %v", err)
			}
			if n != tc.wantPurged {
				t.Errorf("purged = %d, want %d", n, tc.wantPurged)
			}
			// Chỉ bài nào đã dọn xong việc ngoài mới bị xoá hẳn row.
			total := 0
			for _, ids := range artRepo.deleted {
				total += len(ids)
			}
			if total != tc.wantPurged {
				t.Errorf("HardDeleteMany xoá %d row, want %d", total, tc.wantPurged)
			}
			// Trang trung gian xoá trước article row — cùng lý do. Không có bài
			// nào dọn xong thì service thoát sớm, không gọi hàm này.
			wantPageCalls := 0
			if tc.wantPurged > 0 {
				wantPageCalls = 1
			}
			if len(pageRepo.deleteManyCalls) != wantPageCalls {
				t.Errorf("DeleteByArticles gọi %d lần, want %d (%v)",
					len(pageRepo.deleteManyCalls), wantPageCalls, pageRepo.deleteManyCalls)
			} else if wantPageCalls == 1 && len(pageRepo.deleteManyCalls[0]) != tc.wantPurged {
				t.Errorf("DeleteByArticles nhận %d id, want %d",
					len(pageRepo.deleteManyCalls[0]), tc.wantPurged)
			}
			// Summary đã compile cũng phải được dọn cùng nhịp (phase 1b).
			if len(sectionRepo.deleteManyCalls) != wantPageCalls {
				t.Errorf("section DeleteByArticles gọi %d lần, want %d",
					len(sectionRepo.deleteManyCalls), wantPageCalls)
			}
		})
	}
}

var (
	_ domain.ArticleJanitor            = (*purgeStubArticleRepo)(nil)
	_ domain.IArticlePageRepository    = (*purgeStubPageRepo)(nil)
	_ domain.IArticleSectionRepository = (*purgeStubSections)(nil)
	_ domain.IVectorStore              = (*purgeStubVectors)(nil)
)
