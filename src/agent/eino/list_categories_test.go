package eino

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"knowledge_ingestion/src/domain"
)

// fakeCategoryRepo là repo giả cho tool — chỉ GetAll có dữ liệu, còn lại
// trả nil để đủ hợp đồng ICategoryRepositoryImpl.
type fakeCategoryRepo struct {
	cats []*domain.Category
	err  error
}

func (f *fakeCategoryRepo) Create(_ context.Context, _ *domain.Category) error {
	return nil
}

func (f *fakeCategoryRepo) Update(_ context.Context, _ *domain.Category) error {
	return nil
}

func (f *fakeCategoryRepo) Delete(_ context.Context, _ *domain.Category) error {
	return nil
}

func (f *fakeCategoryRepo) GetById(_ context.Context, _ int64, _ int64) (*domain.Category, error) {
	return nil, nil
}

func (f *fakeCategoryRepo) GetByName(_ context.Context, _ string, _ int64) (*domain.Category, error) {
	return nil, nil
}

func (f *fakeCategoryRepo) GetAll(_ context.Context, _ int64) ([]*domain.Category, error) {
	return f.cats, f.err
}

func TestNewListCategoriesToolRequiresRepo(t *testing.T) {
	if _, err := NewListCategoriesTool(nil); err == nil {
		t.Fatal("repo nil phải lỗi để fail-fast lúc startup")
	}
}

func TestListCategoriesToolRequiresScope(t *testing.T) {
	tool, err := NewListCategoriesTool(&fakeCategoryRepo{})
	if err != nil {
		t.Fatalf("NewListCategoriesTool: %v", err)
	}
	// Không WithScope trong ctx — tool phải từ chối chứ không chạy với
	// phạm vi rỗng (kẻo lộ chủ đề của user khác).
	if _, err := tool.InvokableRun(context.Background(), "{}"); err == nil {
		t.Fatal("thiếu scope phải lỗi")
	} else if !strings.Contains(err.Error(), "scope") {
		t.Fatalf("lỗi phải nhắc scope, got %q", err)
	}
}

func TestListCategoriesToolReturnsOwnerCategories(t *testing.T) {
	tool, err := NewListCategoriesTool(&fakeCategoryRepo{cats: []*domain.Category{
		{BaseModel: domain.BaseModel{ID: 3}, UserID: 7, Name: "Thuế", Description: "thuế TNCN"},
		nil, // entry nil phải bỏ qua, không vỡ output
		{BaseModel: domain.BaseModel{ID: 5}, UserID: 7, Name: "Bảo hiểm"},
	}})
	if err != nil {
		t.Fatalf("NewListCategoriesTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 7})
	raw, err := tool.InvokableRun(ctx, "{}")
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var got []listCategoriesItem
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải là JSON parse được, got %q: %v", raw, err)
	}
	if len(got) != 2 || got[0].ID != 3 || got[0].Name != "Thuế" || got[1].ID != 5 {
		t.Fatalf("phải trả đúng 2 chủ đề của owner theo thứ tự repo, got %+v", got)
	}
}
