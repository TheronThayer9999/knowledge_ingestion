package services

import (
	"context"
	"errors"
	"testing"

	"knowledge_ingestion/src/domain"
)

var errTestTextDown = errors.New("text index down")

// RRF: chunk có mặt ở cả 2 phía phải đứng trước chunk chỉ 1 phía; chunk text
// duy nhất (từ khóa chính xác mà dense bỏ sót) vẫn được giữ. Thứ tự 1-phía
// theo rank gốc: d1 (rank 0 dense, 1/61) trên t1 (rank 1 text, 1/62).
func TestFuseRRF_BothSidesFirstTextOnlyKept(t *testing.T) {
	dense := []*domain.ScoredChunk{
		{PointID: "d1", Score: 0.9, ArticleID: 1, Text: "dense 1"},
		{PointID: "d2", Score: 0.8, ArticleID: 1, Text: "dense 2"},
	}
	text := []*domain.ScoredChunk{
		{PointID: "d2", ArticleID: 1, Text: "dense 2"},
		{PointID: "t1", ArticleID: 2, Text: "keyword chính xác"},
	}
	got := FuseRRF(dense, text)
	if len(got) != 3 {
		t.Fatalf("expected 3 chunk, got %d", len(got))
	}
	if got[0].PointID != "d2" {
		t.Fatalf("chunk 2 phía phải đứng đầu, got %s", got[0].PointID)
	}
	if got[1].PointID != "d1" || got[2].PointID != "t1" {
		t.Fatalf("thứ tự 1-phía sai, got %s, %s", got[1].PointID, got[2].PointID)
	}
	if got[0].Score <= got[1].Score || got[1].Score <= got[2].Score {
		t.Fatalf("score RRF phải giảm dần, got %v", got)
	}
}

func TestFuseRRF_Empty(t *testing.T) {
	if got := FuseRRF(nil, nil); len(got) != 0 {
		t.Fatalf("expected rỗng, got %d", len(got))
	}
}

// Pipeline hybrid qua fake: dense miss từ khóa nhưng text trúng → hit text
// vẫn về sau fuse; bài chưa done vẫn bị chặn.
func TestSearchHybrid_TextOnlyHitSurvives(t *testing.T) {
	embedder := &fakeSearchEmbedder{vec: []float32{0.1, 0.2, 0.3, 0.4}}
	vectors := &fakeSearchVectors{
		hits:     []*domain.ScoredChunk{},
		textHits: []*domain.ScoredChunk{{PointID: "t1", ArticleID: 8, Text: "số hiệu 123/QĐ"}},
	}
	articles := &fakeSearchArticles{doneIDs: []int64{8}}
	h := NewHybridSearcher(embedder, vectors, searchRepoAdapter{articles})

	got, err := h.SearchHybrid(context.Background(), HybridParams{UserID: 7, Query: "123/QĐ", Limit: 5})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 1 || got[0].PointID != "t1" {
		t.Fatalf("hit text phải về sau fuse, got %+v", got)
	}
}

// Text search lỗi (collection cũ thiếu index) → degraded dense-only, không
// fail cả query.
func TestSearchHybrid_TextErrorDegradesToDense(t *testing.T) {
	embedder := &fakeSearchEmbedder{vec: []float32{0.1, 0.2, 0.3, 0.4}}
	vectors := &fakeSearchVectors{
		hits:    []*domain.ScoredChunk{{PointID: "d1", Score: 0.9, ArticleID: 8, Text: "dense"}},
		textErr: errTestTextDown,
	}
	articles := &fakeSearchArticles{doneIDs: []int64{8}}
	h := NewHybridSearcher(embedder, vectors, searchRepoAdapter{articles})

	got, err := h.SearchHybrid(context.Background(), HybridParams{UserID: 7, Query: "x", Limit: 5})
	if err != nil {
		t.Fatalf("lỗi text phải degraded, không fail: %v", err)
	}
	if len(got) != 1 || got[0].PointID != "d1" {
		t.Fatalf("dense phải còn nguyên, got %+v", got)
	}
}
