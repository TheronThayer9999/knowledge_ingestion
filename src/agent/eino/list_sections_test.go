package eino

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"knowledge_ingestion/src/domain"
)

// fakeSectionVectors là vector store giả cho tool — chỉ ListSections có dữ
// liệu, còn lại trả nil để đủ hợp đồng IVectorStore.
type fakeSectionVectors struct {
	sections []string
	got      domain.SearchFilter
	err      error
}

func (f *fakeSectionVectors) UpsertPoints(_ context.Context, _ []*domain.VectorPoint) error {
	return nil
}
func (f *fakeSectionVectors) DeleteByArticle(_ context.Context, _ int64) error { return nil }
func (f *fakeSectionVectors) CountByArticle(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}
func (f *fakeSectionVectors) Search(_ context.Context, _ []float32, _ domain.SearchFilter, _ int, _ float32) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (f *fakeSectionVectors) SearchText(_ context.Context, _ string, _ domain.SearchFilter, _ int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (f *fakeSectionVectors) IsPermanentError(_ error) bool { return false }
func (f *fakeSectionVectors) ScrollNeighbors(_ context.Context, _ domain.SearchFilter, _, _ int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (f *fakeSectionVectors) ListSections(_ context.Context, filter domain.SearchFilter) ([]string, error) {
	f.got = filter
	return f.sections, f.err
}

func TestNewListSectionsToolRequiresStore(t *testing.T) {
	if _, err := NewListSectionsTool(nil); err == nil {
		t.Fatal("store nil phải lỗi để fail-fast lúc startup")
	}
}

func TestListSectionsToolRequiresScope(t *testing.T) {
	tool, err := NewListSectionsTool(&fakeSectionVectors{})
	if err != nil {
		t.Fatalf("NewListSectionsTool: %v", err)
	}
	if _, err := tool.InvokableRun(context.Background(), `{"article_id":10}`); err == nil {
		t.Fatal("thiếu scope phải lỗi")
	} else if !strings.Contains(err.Error(), "scope") {
		t.Fatalf("lỗi phải nhắc scope, got %q", err)
	}
}

// article_id thiếu/không có → lỗi; user trong scope phải khóa đúng owner.
func TestListSectionsToolValidatesArgs(t *testing.T) {
	v := &fakeSectionVectors{sections: []string{"ĐIỀU 1", "ĐIỀU 2"}}
	tool, err := NewListSectionsTool(v)
	if err != nil {
		t.Fatalf("NewListSectionsTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 3, CategoryIDs: []int64{9}})
	if _, err := tool.InvokableRun(ctx, `{}`); err == nil {
		t.Fatal("thiếu article_id phải lỗi")
	}
	raw, err := tool.InvokableRun(ctx, `{"article_id":10}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if v.got.UserID != 3 || v.got.ArticleID != 10 || len(v.got.CategoryIDs) != 1 || v.got.CategoryIDs[0] != 9 {
		t.Fatalf("filter phải khóa user + article + category scope, got %+v", v.got)
	}
	var got listSectionsResult
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải là JSON parse được, got %q: %v", raw, err)
	}
	if got.ArticleID != 10 || got.SectionCount != 2 || len(got.Sections) != 2 || got.Sections[0] != "ĐIỀU 1" {
		t.Fatalf("section_count + sections sai: %+v", got)
	}
}
