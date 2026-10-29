package eino

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"knowledge_ingestion/src/domain"
)

// fakeArticleVectors là vector store giả cho read_article — ScrollRange trả
// đúng lát [from, from+limit), CountByFilter trả tổng theo scope (scopedTotal),
// CountByArticle giữ tổng toàn cục để test không dùng nhầm bản unscoped.
type fakeArticleVectors struct {
	points              []*domain.ScoredChunk
	total               int64 // CountByArticle — chỉ để phát hiện dùng nhầm
	scopedTotal         int64 // CountByFilter — tổng đúng theo filter
	gotFrom             int
	gotLimit            int
	gotFilter           domain.SearchFilter
	gotCountFilter      domain.SearchFilter
	countByArticleCalls int
}

func (f *fakeArticleVectors) UpsertPoints(_ context.Context, _ []*domain.VectorPoint) error {
	return nil
}
func (f *fakeArticleVectors) DeleteByArticle(_ context.Context, _ int64) error { return nil }
func (f *fakeArticleVectors) DeleteByArticles(_ context.Context, _ []int64) error {
	return nil
}
func (f *fakeArticleVectors) CountByArticle(_ context.Context, _ int64) (int64, error) {
	f.countByArticleCalls++
	return f.total, nil
}
func (f *fakeArticleVectors) CountByFilter(_ context.Context, filter domain.SearchFilter) (int64, error) {
	f.gotCountFilter = filter
	return f.scopedTotal, nil
}
func (f *fakeArticleVectors) CountCollection(context.Context) (int64, error) {
	return f.total, nil
}
func (f *fakeArticleVectors) Search(_ context.Context, _ []float32, _ domain.SearchFilter, _ int, _ float32) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (f *fakeArticleVectors) SearchText(_ context.Context, _ string, _ domain.SearchFilter, _ int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (f *fakeArticleVectors) IsPermanentError(_ error) bool { return false }
func (f *fakeArticleVectors) ScrollNeighbors(_ context.Context, _ domain.SearchFilter, _, _ int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (f *fakeArticleVectors) ListSections(_ context.Context, _ domain.SearchFilter) ([]domain.SectionSpan, error) {
	return nil, nil
}
func (f *fakeArticleVectors) ScrollRange(_ context.Context, filter domain.SearchFilter, from, limit int) ([]*domain.ScoredChunk, error) {
	f.gotFrom, f.gotLimit = from, limit
	f.gotFilter = filter
	return f.points, nil
}

func TestNewReadArticleToolRequiresStore(t *testing.T) {
	if _, err := NewReadArticleTool(nil); err == nil {
		t.Fatal("store nil phải lỗi để fail-fast lúc startup")
	}
}

// Thiếu scope → lỗi; article_id không phải số → trang rỗng next_from 0,
// không gọi store, không error (không abort run 500).
func TestReadArticleToolValidatesArgs(t *testing.T) {
	v := &fakeArticleVectors{}
	tool, err := NewReadArticleTool(v)
	if err != nil {
		t.Fatalf("NewReadArticleTool: %v", err)
	}
	if _, err := tool.InvokableRun(context.Background(), `{"article_id":10}`); err == nil {
		t.Fatal("thiếu scope phải lỗi")
	}
	ctx := WithScope(context.Background(), Scope{UserID: 3, CategoryIDs: []int64{9}})
	raw, err := tool.InvokableRun(ctx, `{"article_id":"None"}`)
	if err != nil {
		t.Fatalf("id không hợp lệ phải trả trang rỗng, không error: %v", err)
	}
	var empty readArticlePage
	if err := json.Unmarshal([]byte(raw), &empty); err != nil {
		t.Fatalf("output phải JSON: %v", err)
	}
	if empty.NextFrom != 0 || len(empty.Items) != 0 {
		t.Fatalf("id không hợp lệ phải trang rỗng + next_from 0, got %+v", empty)
	}
	if v.gotLimit != 0 {
		t.Fatalf("id sai không được gọi store, got from=%d limit=%d", v.gotFrom, v.gotLimit)
	}
}

// Trang cuối (short page) → next_from 0; items kèm n khớp collector, section
// gom vào map danh sách mục đã đi qua.
func TestReadArticleToolPaging(t *testing.T) {
	v := &fakeArticleVectors{
		points: []*domain.ScoredChunk{
			{PointID: "c5", ArticleID: 10, ChunkIndex: 5, PageNum: 2, Section: "ĐIỀU 2", Text: "nửa sau"},
			{PointID: "c4", ArticleID: 10, ChunkIndex: 4, PageNum: 2, Section: "ĐIỀU 2", Text: "nửa đầu"},
		},
		total:       6,
		scopedTotal: 6,
	}
	tool, err := NewReadArticleTool(v)
	if err != nil {
		t.Fatalf("NewReadArticleTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 3, CategoryIDs: []int64{9}, Collector: &HitCollector{}})
	raw, err := tool.InvokableRun(ctx, `{"article_id":10,"from":4,"limit":8}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var page readArticlePage
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatalf("output phải JSON: %v", err)
	}
	if v.gotFrom != 4 || v.gotLimit != 8 {
		t.Fatalf("phải truyền from/limit xuống store, got %d/%d", v.gotFrom, v.gotLimit)
	}
	// Filter phải mang đủ scope — tháo CategoryIDs là lỗ hổng GAP-3 quay lại.
	if v.gotFilter.UserID != 3 || v.gotFilter.ArticleID != 10 ||
		len(v.gotFilter.CategoryIDs) != 1 || v.gotFilter.CategoryIDs[0] != 9 {
		t.Fatalf("filter thiếu scope: %+v", v.gotFilter)
	}
	if v.countByArticleCalls != 0 {
		t.Fatalf("read_article không được dùng CountByArticle (unscoped), gọi %d lần", v.countByArticleCalls)
	}
	if v.gotCountFilter.UserID != 3 || v.gotCountFilter.ArticleID != 10 ||
		len(v.gotCountFilter.CategoryIDs) != 1 || v.gotCountFilter.CategoryIDs[0] != 9 {
		t.Fatalf("CountByFilter phải nhận cùng scope với ScrollRange: %+v", v.gotCountFilter)
	}
	if page.TotalChunk != 6 || page.NextFrom != 0 {
		t.Fatalf("trang cuối (2 < 8) phải next_from 0, total 6: %+v", page)
	}
	if len(page.Items) != 2 || page.Items[0].ChunkIndex != 4 || page.Items[1].ChunkIndex != 5 {
		t.Fatalf("items phải theo thứ tự chunk: %+v", page.Items)
	}
	if page.Items[0].N != 1 || page.Items[1].N != 2 {
		t.Fatalf("n phải khớp collector cho cites: %+v", page.Items)
	}
	if !strings.Contains(page.Sections, "ĐIỀU 2") {
		t.Fatalf("sections phải gom mục đã đi qua, got %q", page.Sections)
	}
}
