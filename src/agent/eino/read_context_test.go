package eino

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"knowledge_ingestion/src/domain"
)

// fakeNeighborVectors là vector store giả cho tool — chỉ ScrollNeighbors có
// dữ liệu, còn lại trả nil để đủ hợp đồng IVectorStore.
type fakeNeighborVectors struct {
	points []*domain.ScoredChunk
	got    domain.SearchFilter
	err    error
}

func (f *fakeNeighborVectors) UpsertPoints(_ context.Context, _ []*domain.VectorPoint) error {
	return nil
}
func (f *fakeNeighborVectors) DeleteByArticle(_ context.Context, _ int64) error { return nil }
func (f *fakeNeighborVectors) DeleteByArticles(_ context.Context, _ []int64) error {
	return nil
}
func (f *fakeNeighborVectors) CountByArticle(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}
func (f *fakeNeighborVectors) CountByFilter(_ context.Context, _ domain.SearchFilter) (int64, error) {
	return 0, nil
}
func (f *fakeNeighborVectors) Search(_ context.Context, _ []float32, _ domain.SearchFilter, _ int, _ float32) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (f *fakeNeighborVectors) SearchText(_ context.Context, _ string, _ domain.SearchFilter, _ int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (f *fakeNeighborVectors) IsPermanentError(err error) bool { return false }
func (f *fakeNeighborVectors) ScrollNeighbors(_ context.Context, filter domain.SearchFilter, _, _ int) ([]*domain.ScoredChunk, error) {
	f.got = filter
	return f.points, f.err
}
func (f *fakeNeighborVectors) ListSections(_ context.Context, _ domain.SearchFilter) ([]domain.SectionSpan, error) {
	return nil, nil
}
func (f *fakeNeighborVectors) ScrollRange(_ context.Context, _ domain.SearchFilter, _, _ int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}

func TestNewReadContextToolRequiresStore(t *testing.T) {
	if _, err := NewReadContextTool(nil); err == nil {
		t.Fatal("store nil phải lỗi để fail-fast lúc startup")
	}
}

func TestReadContextToolRequiresScope(t *testing.T) {
	tool, err := NewReadContextTool(&fakeNeighborVectors{})
	if err != nil {
		t.Fatalf("NewReadContextTool: %v", err)
	}
	if _, err := tool.InvokableRun(context.Background(), `{"article_id":10,"chunk_index":3}`); err == nil {
		t.Fatal("thiếu scope phải lỗi")
	} else if !strings.Contains(err.Error(), "scope") {
		t.Fatalf("lỗi phải nhắc scope, got %q", err)
	}
}

// article_id thiếu/không phải số → trả rỗng không gọi store (LLM tự xoay);
// user trong scope phải khóa đúng owner.
func TestReadContextToolValidatesArgs(t *testing.T) {
	v := &fakeNeighborVectors{points: []*domain.ScoredChunk{
		{PointID: "p", ArticleID: 10, ChunkIndex: 3, Text: "x"},
	}}
	tool, err := NewReadContextTool(v)
	if err != nil {
		t.Fatalf("NewReadContextTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 3, CategoryIDs: []int64{9}})
	// model free gửi "None"/thiếu id → không lỗi (không abort run 500), rỗng.
	for _, args := range []string{`{"chunk_index":3}`, `{"article_id":"None","chunk_index":3}`} {
		raw, err := tool.InvokableRun(ctx, args)
		if err != nil {
			t.Fatalf("args không hợp lệ phải trả rỗng, không error: %v", err)
		}
		if strings.TrimSpace(raw) != "[]" {
			t.Fatalf("args không hợp lệ phải trả [], got %q", raw)
		}
	}
	if v.got.ArticleID != 0 {
		t.Fatalf("args sai không được gọi store, got %+v", v.got)
	}
	raw, err := tool.InvokableRun(ctx, `{"article_id":10,"chunk_index":3,"window":99}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if v.got.UserID != 3 || v.got.ArticleID != 10 || len(v.got.CategoryIDs) != 1 || v.got.CategoryIDs[0] != 9 {
		t.Fatalf("filter phải khóa user + article + category scope, got %+v", v.got)
	}
	var got []readContextHit
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải là JSON parse được, got %q: %v", raw, err)
	}
}

// Kết quả giữ thứ tự ChunkIndex, n khớp collector để cites [^n] đúng.
func TestReadContextToolOrdersHits(t *testing.T) {
	v := &fakeNeighborVectors{points: []*domain.ScoredChunk{
		{PointID: "p2", ArticleID: 10, ChunkIndex: 4, Section: "ĐIỀU 5", Text: "đuôi"},
		{PointID: "p1", ArticleID: 10, ChunkIndex: 3, Section: "ĐIỀU 5", Text: "đầu"},
	}}
	tool, err := NewReadContextTool(v)
	if err != nil {
		t.Fatalf("NewReadContextTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 3})
	raw, err := tool.InvokableRun(ctx, `{"article_id":10,"chunk_index":3}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var got []readContextHit
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải là JSON parse được: %v", err)
	}
	if len(got) != 2 || got[0].ChunkIndex != 3 || got[1].ChunkIndex != 4 {
		t.Fatalf("phải trả theo ChunkIndex tăng dần, got %+v", got)
	}
	if got[0].N != 1 || got[1].N != 2 || got[0].Section != "ĐIỀU 5" {
		t.Fatalf("n + section sai: %+v", got)
	}
}
