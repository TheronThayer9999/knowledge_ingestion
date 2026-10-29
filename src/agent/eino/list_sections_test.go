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
func (f *fakeSectionVectors) DeleteByArticles(_ context.Context, _ []int64) error {
	return nil
}
func (f *fakeSectionVectors) CountByArticle(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}
func (f *fakeSectionVectors) CountByFilter(_ context.Context, _ domain.SearchFilter) (int64, error) {
	return 0, nil
}
func (f *fakeSectionVectors) CountCollection(context.Context) (int64, error) {
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
func (f *fakeSectionVectors) ListSections(_ context.Context, filter domain.SearchFilter) ([]domain.SectionSpan, error) {
	f.got = filter
	out := make([]domain.SectionSpan, 0, len(f.sections))
	for i, s := range f.sections {
		out = append(out, domain.SectionSpan{Name: s, From: i * 10})
	}
	return out, f.err
}
func (f *fakeSectionVectors) ScrollRange(_ context.Context, _ domain.SearchFilter, _, _ int) ([]*domain.ScoredChunk, error) {
	return nil, nil
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

// article_id thiếu/không phải số → note cho LLM tự sửa, không error;
// user trong scope phải khóa đúng owner.
func TestListSectionsToolValidatesArgs(t *testing.T) {
	v := &fakeSectionVectors{sections: []string{"ĐIỀU 1", "ĐIỀU 2"}}
	tool, err := NewListSectionsTool(v)
	if err != nil {
		t.Fatalf("NewListSectionsTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 3, CategoryIDs: []int64{9}})
	for _, args := range []string{`{}`, `{"article_id":"None"}`} {
		raw, err := tool.InvokableRun(ctx, args)
		if err != nil {
			t.Fatalf("args không hợp lệ phải trả note, không error: %v", err)
		}
		var bad listSectionsResult
		if err := json.Unmarshal([]byte(raw), &bad); err != nil {
			t.Fatalf("output phải JSON: %q: %v", raw, err)
		}
		if bad.Note == "" || bad.SectionCount != 0 {
			t.Fatalf("args không hợp lệ phải có note + count 0, got %+v", bad)
		}
	}
	raw, err := tool.InvokableRun(ctx, `{"article_id":"10"}`)
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
	if got.ArticleID != 10 || got.SectionCount != 2 || len(got.Sections) != 2 || got.Sections[0].Name != "ĐIỀU 1" || got.Sections[0].From != 0 || got.Sections[1].From != 10 {
		t.Fatalf("section_count + sections (kèm tọa độ from) sai: %+v", got)
	}
}
