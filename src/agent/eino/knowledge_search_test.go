package eino

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/agent/tools"
	"knowledge_ingestion/src/domain"
)

// Fakes nội bộ của eino — usecase/retrieval có bộ fake riêng, không dùng ké
// qua package khác.

type fakeEmbedder struct {
	vec []float32
	err error
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("empty")
	}
	return f.vec, nil
}

func (f *fakeEmbedder) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, nil
}

func (f *fakeEmbedder) Dimension() int                { return 4 }
func (f *fakeEmbedder) ModelName() string             { return "test-model" }
func (f *fakeEmbedder) IsPermanentError(_ error) bool { return false }

type fakeVectors struct {
	hits     []*domain.ScoredChunk
	gotLimit int
	err      error
}

func (f *fakeVectors) UpsertPoints(_ context.Context, _ []*domain.VectorPoint) error {
	return nil
}

func (f *fakeVectors) DeleteByArticle(_ context.Context, _ int64) error { return nil }

func (f *fakeVectors) CountByArticle(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

func (f *fakeVectors) IsPermanentError(_ error) bool { return false }

func (f *fakeVectors) Search(_ context.Context, _ []float32, _ domain.SearchFilter, limit int, _ float32) ([]*domain.ScoredChunk, error) {
	f.gotLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.hits, nil
}

func (f *fakeVectors) SearchText(_ context.Context, _ string, _ domain.SearchFilter, _ int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}

type fakeDone struct {
	ids []int64
}

func (f *fakeDone) ListDoneIDs(_ context.Context, _ int64, ids []int64) ([]int64, error) {
	allowed := make(map[int64]bool, len(f.ids))
	for _, id := range f.ids {
		allowed[id] = true
	}
	var out []int64
	for _, id := range ids {
		if allowed[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func testRegistry(vec []float32, hits []*domain.ScoredChunk, done []int64) (*tools.Registry, *fakeVectors) {
	v := &fakeVectors{hits: hits}
	reg := tools.NewRegistry(retrieval.NewHybridSearcher(
		&fakeEmbedder{vec: vec},
		v,
		&fakeDone{ids: done},
	))
	return reg, v
}

func TestKnowledgeSearchInfo(t *testing.T) {
	reg, _ := testRegistry([]float32{0.1}, nil, nil)
	tl, err := NewKnowledgeSearchTool(reg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	info, err := tl.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != KnowledgeSearchName {
		t.Fatalf("name = %q", info.Name)
	}
	raw, _ := json.Marshal(info.ParamsOneOf)
	_ = raw
	js, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil || js == nil {
		t.Fatalf("ToJSONSchema: %v", err)
	}
	raw, _ = json.Marshal(js)
	if !strings.Contains(string(raw), "query") {
		t.Fatalf("schema thiếu query, got %s", raw)
	}
}

func TestKnowledgeSearchRun(t *testing.T) {
	reg, v := testRegistry([]float32{0.1},
		[]*domain.ScoredChunk{{PointID: "p1", Score: 0.9, ArticleID: 8, ChunkIndex: 2, PageNum: 4, Text: "Điều 13 phạt"}},
		[]int64{8})
	tl, err := NewKnowledgeSearchTool(reg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, col := WithCollector(WithRequest(context.Background(), Request{UserID: 7}))
	out, err := tl.InvokableRun(ctx, `{"query":"mức phạt","limit":5}`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var hits []knowledgeSearchHit
	if err := json.Unmarshal([]byte(out), &hits); err != nil {
		t.Fatalf("output phải là JSON hits, got %q: %v", out, err)
	}
	if len(hits) != 1 || hits[0].N != 1 || hits[0].ArticleID != 8 || hits[0].Text == "" {
		t.Fatalf("hits sai: %s", out)
	}
	if len(col.Hits) != 1 {
		t.Fatalf("collector phải gom 1 hit cho cites, got %d", len(col.Hits))
	}
	if v.gotLimit != 5 {
		t.Fatalf("phải tôn trọng limit LLM, got %d", v.gotLimit)
	}
}

// Category DTO ép thắng gợi ý LLM — LLM không mở rộng phạm vi được.
func TestKnowledgeSearchDTOCategoryWins(t *testing.T) {
	reg, _ := testRegistry([]float32{0.1}, nil, nil)
	tl, err := NewKnowledgeSearchTool(reg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Không assert Qdrant filter ở đây (fake không ghi), chỉ khóa không lỗi và
	// logic ép nằm ở 1 chỗ — test filter thuộc về registry/usecase.
	ctx := WithRequest(context.Background(), Request{UserID: 7, CategoryID: 5})
	if _, err := tl.InvokableRun(ctx, `{"query":"x","category_id":9}`); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestKnowledgeSearchGuards(t *testing.T) {
	reg, _ := testRegistry([]float32{0.1}, nil, nil)
	tl, err := NewKnowledgeSearchTool(reg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := tl.InvokableRun(context.Background(), `{"query":"x"}`); err == nil {
		t.Fatal("thiếu Request (user_id) phải lỗi, không tin LLM")
	}
	ctx := WithRequest(context.Background(), Request{UserID: 7})
	if _, err := tl.InvokableRun(ctx, `{"query":"   "}`); err == nil {
		t.Fatal("query rỗng phải lỗi")
	}
	if _, err := NewKnowledgeSearchTool(nil); err == nil {
		t.Fatal("registry nil phải lỗi lúc dựng")
	}
}

func TestCollectorDedupe(t *testing.T) {
	_, col := WithCollector(context.Background())
	h := &domain.ScoredChunk{PointID: "p1", ArticleID: 8, Text: "a"}
	ns := col.Add([]*domain.ScoredChunk{h, h})
	if len(ns) != 2 || ns[0] != 1 || ns[1] != 1 || len(col.Hits) != 1 {
		t.Fatalf("trùng phải giữ n cũ, got %v hits=%d", ns, len(col.Hits))
	}
}
