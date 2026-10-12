package eino

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"knowledge_ingestion/src/domain"
)

// fakeInfoArticleRepo là repo giả cho tool — chỉ GetByID/ListByCategoryID có
// dữ liệu, còn lại trả nil để đủ hợp đồng IArticleRepository.
type fakeInfoArticleRepo struct {
	byID map[int64]*domain.Article
	byCat map[int64][]*domain.Article
}

func (f *fakeInfoArticleRepo) Create(_ context.Context, _ *domain.Article) error { return nil }
func (f *fakeInfoArticleRepo) Update(_ context.Context, _ *domain.Article) error { return nil }
func (f *fakeInfoArticleRepo) Delete(_ context.Context, _ *domain.Article) error { return nil }
func (f *fakeInfoArticleRepo) GetByID(_ context.Context, id int64, _ int64) (*domain.Article, error) {
	return f.byID[id], nil
}
func (f *fakeInfoArticleRepo) IsAlive(_ context.Context, _ int64) (bool, error) { return true, nil }
func (f *fakeInfoArticleRepo) ExistsByURL(_ context.Context, _ string, _ int64) (bool, error) {
	return false, nil
}
func (f *fakeInfoArticleRepo) ExistsByStorageKey(_ context.Context, _ string, _ int64) (bool, error) {
	return false, nil
}
func (f *fakeInfoArticleRepo) ListByCategoryID(_ context.Context, categoryID int64, _ int64, _, _ int) ([]*domain.Article, error) {
	return f.byCat[categoryID], nil
}
func (f *fakeInfoArticleRepo) ListByCategoryName(_ context.Context, _ string, _ int64, _, _ int) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeInfoArticleRepo) ListSoftDeleted(_ context.Context, _ time.Time, _ int) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeInfoArticleRepo) ClaimChunkPending(_ context.Context, _ int, _ time.Duration) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeInfoArticleRepo) MarkChunkDone(_ context.Context, _ int64) error { return nil }
func (f *fakeInfoArticleRepo) MarkChunkError(_ context.Context, _ int64, _ int) error {
	return nil
}
func (f *fakeInfoArticleRepo) ClaimEmbedPending(_ context.Context, _ int, _ time.Duration) ([]*domain.Article, error) {
	return nil, nil
}
func (f *fakeInfoArticleRepo) MarkEmbedDone(_ context.Context, _ int64) error { return nil }
func (f *fakeInfoArticleRepo) MarkEmbedError(_ context.Context, _ int64, _ int) error {
	return nil
}
func (f *fakeInfoArticleRepo) HardDelete(_ context.Context, _ int64) error { return nil }
func (f *fakeInfoArticleRepo) IDsByCategory(_ context.Context, _ int64, _ int64) ([]int64, error) {
	return nil, nil
}
func (f *fakeInfoArticleRepo) ListDoneIDs(_ context.Context, _ int64, ids []int64) ([]int64, error) {
	return ids, nil
}
func (f *fakeInfoArticleRepo) ResetQueue(_ context.Context, _ []int64, _ int64) (int64, error) {
	return 0, nil
}

func TestNewArticleInfoToolRequiresRepo(t *testing.T) {
	if _, err := NewArticleInfoTool(nil); err == nil {
		t.Fatal("repo nil phải lỗi để fail-fast lúc startup")
	}
}

func TestArticleInfoToolRequiresScope(t *testing.T) {
	tool, err := NewArticleInfoTool(&fakeInfoArticleRepo{})
	if err != nil {
		t.Fatalf("NewArticleInfoTool: %v", err)
	}
	if _, err := tool.InvokableRun(context.Background(), `{"article_id":10}`); err == nil {
		t.Fatal("thiếu scope phải lỗi")
	} else if !strings.Contains(err.Error(), "scope") {
		t.Fatalf("lỗi phải nhắc scope, got %q", err)
	}
}

// Xem 1 bài theo ID — metadata số trang/phiên bản/hiệu lực phải ra đủ.
func TestArticleInfoToolSingleArticle(t *testing.T) {
	tool, err := NewArticleInfoTool(&fakeInfoArticleRepo{byID: map[int64]*domain.Article{
		10: {BaseModel: domain.BaseModel{ID: 10}, Name: "quy_dinh_noi_bo.pdf",
			ContentType: "application/pdf", PageCount: 12, Version: "2024-Q3", EffectiveDate: "01/10/2024"},
	}})
	if err != nil {
		t.Fatalf("NewArticleInfoTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 3})
	raw, err := tool.InvokableRun(ctx, `{"article_id":10}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var got []articleInfoItem
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải là JSON parse được, got %q: %v", raw, err)
	}
	if len(got) != 1 || got[0].PageCount != 12 || got[0].Version != "2024-Q3" || got[0].EffectiveDate != "01/10/2024" {
		t.Fatalf("metadata thiếu: %+v", got)
	}
}

// Không ID, không category, scope cũng không khoanh → trả rỗng chứ không quét
// bừa, và không lỗi để agent tự xoay thay vì abort cả run thành 500.
func TestArticleInfoToolNeedsScope(t *testing.T) {
	tool, err := NewArticleInfoTool(&fakeInfoArticleRepo{})
	if err != nil {
		t.Fatalf("NewArticleInfoTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 3})
	raw, err := tool.InvokableRun(ctx, `{}`)
	if err != nil {
		t.Fatalf("không rõ phạm vi phải trả rỗng, không lỗi: %v", err)
	}
	var got []articleInfoItem
	if err := json.Unmarshal([]byte(raw), &got); err != nil || len(got) != 0 {
		t.Fatalf("phải trả danh sách rỗng, got %q: %v", raw, err)
	}
}
