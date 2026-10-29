package retrieval

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/domain"
)

// ---- stub ----

type stubEmbedder struct{ err error }

func (s stubEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{0.1, 0.2}, s.err
}
func (s stubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0.1, 0.2}
	}
	return out, s.err
}
func (s stubEmbedder) Dimension() int              { return 2 }
func (s stubEmbedder) ModelName() string           { return "stub" }
func (s stubEmbedder) IsPermanentError(error) bool { return false }

type stubVectors struct {
	dense     []*domain.ScoredChunk
	text      []*domain.ScoredChunk
	textErr   error
	textLimit int // ghi lại limit nửa keyword xin — khoá chuyện "xin dư"
}

func (v *stubVectors) UpsertPoints(context.Context, []*domain.VectorPoint) error { return nil }
func (v *stubVectors) DeleteByArticle(context.Context, int64) error              { return nil }
func (v *stubVectors) DeleteByArticles(context.Context, []int64) error           { return nil }
func (v *stubVectors) CountByArticle(context.Context, int64) (int64, error)      { return 0, nil }
func (v *stubVectors) CountByFilter(context.Context, domain.SearchFilter) (int64, error) {
	return 0, nil
}
func (v *stubVectors) CountCollection(context.Context) (int64, error) { return 0, nil }
func (v *stubVectors) Search(context.Context, []float32, domain.SearchFilter, int, float32) ([]*domain.ScoredChunk, error) {
	return v.dense, nil
}
func (v *stubVectors) SearchText(_ context.Context, _ string, _ domain.SearchFilter, limit int) ([]*domain.ScoredChunk, error) {
	v.textLimit = limit
	if v.textErr != nil {
		return nil, v.textErr
	}
	return v.text, nil
}
func (v *stubVectors) ScrollNeighbors(context.Context, domain.SearchFilter, int, int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v *stubVectors) ScrollRange(context.Context, domain.SearchFilter, int, int) ([]*domain.ScoredChunk, error) {
	return nil, nil
}
func (v *stubVectors) ListSections(context.Context, domain.SearchFilter) ([]domain.SectionSpan, error) {
	return nil, nil
}
func (v *stubVectors) IsPermanentError(error) bool { return false }

type stubDone struct {
	done []int64
	err  error
}

func (s stubDone) ListDoneIDs(context.Context, int64, []int64) ([]int64, error) {
	return s.done, s.err
}

func hit(pointID string, articleID int64, text string) *domain.ScoredChunk {
	return &domain.ScoredChunk{PointID: pointID, ArticleID: articleID, Text: text}
}

func newSearcher(v domain.IVectorStore, e domain.Embedder, repo DoneLister) *HybridSearcher {
	return NewHybridSearcher(e, v, repo)
}

var _ domain.IVectorStore = (*stubVectors)(nil)
var _ domain.Embedder = stubEmbedder{}
var _ DoneLister = stubDone{}

// ---- tests ----

// Nửa keyword phải được xếp hạng theo độ phủ từ TRƯỚC khi fuse. Ở đây Scroll
// trả chunk khớp-lở-một ("t_bad") lên đầu theo thứ tự nội bộ; chunk phủ đủ query
// ("t_good") nằm cuối mẫu. Không xếp hạng lại thì t_bad ăn rank 1 và chèn vào
// top, t_good biến mất — đó chính xác là lỗi ranking baseline đã đo.
func TestSearchHybridRanksKeywordLegBeforeFusing(t *testing.T) {
	vec := &stubVectors{
		dense: []*domain.ScoredChunk{hit("d1", 1, ""), hit("d2", 1, "")},
		text: []*domain.ScoredChunk{
			hit("t_bad", 1, "có"),
			hit("t_mid", 1, "hợp đồng"),
			hit("t_good", 1, "hợp đồng dân sự bồi thường thiệt hại"),
		},
	}
	got, err := newSearcher(vec, stubEmbedder{}, stubDone{done: []int64{1}}).
		SearchHybrid(context.Background(), HybridParams{
			UserID: 7, Query: "hợp đồng dân sự bồi thường thiệt hại", Limit: 2,
		})
	if err != nil {
		t.Fatalf("SearchHybrid: %v", err)
	}
	if ids := pointIDs(got); !reflect.DeepEqual(ids, []string{"d1", "t_good"}) {
		t.Errorf("top 2 = %v, want [d1 t_good] (chunk phủ đủ từ phải thắng mẫu ngẫu nhiên)", ids)
	}
}

// Kho sample của nửa keyword phải là SEARCH_TEXT_FETCH, không phải fetch của
// dense — xin đúng limit thì chỉ lấy được một mẫu ngẫu nhiên cỡ limit.
func TestSearchHybridOverFetchesKeywordLeg(t *testing.T) {
	vec := &stubVectors{}
	if _, err := newSearcher(vec, stubEmbedder{}, stubDone{}).
		SearchHybrid(context.Background(), HybridParams{UserID: 7, Query: "hợp đồng", Limit: 2}); err != nil {
		t.Fatalf("SearchHybrid: %v", err)
	}
	if vec.textLimit != constants.SEARCH_TEXT_FETCH {
		t.Errorf("nửa keyword xin limit = %d, want %d", vec.textLimit, constants.SEARCH_TEXT_FETCH)
	}
}

// Lỗi text leg (collection cũ chưa có chỉ mục text) chỉ degraded, không được
// fail cả query.
func TestSearchHybridDegradesToDenseWhenTextFails(t *testing.T) {
	vec := &stubVectors{
		dense:   []*domain.ScoredChunk{hit("d1", 1, ""), hit("d2", 1, "")},
		textErr: errors.New("no index for field text"),
	}
	got, err := newSearcher(vec, stubEmbedder{}, stubDone{done: []int64{1}}).
		SearchHybrid(context.Background(), HybridParams{UserID: 7, Query: "hợp đồng", Limit: 2})
	if err != nil {
		t.Fatalf("SearchHybrid phải sống khi text lỗi: %v", err)
	}
	if ids := pointIDs(got); !reflect.DeepEqual(ids, []string{"d1", "d2"}) {
		t.Errorf("dense-only = %v, want [d1 d2]", ids)
	}
}

// Bài chưa index xong bị bỏ khỏi kết quả — chunk dang dở đưa vào context là
// model trả lời dựa trên tài liệu nửa vời.
func TestSearchHybridDropsUnfinishedArticles(t *testing.T) {
	vec := &stubVectors{
		dense: []*domain.ScoredChunk{hit("d1", 9, ""), hit("d2", 1, "")},
	}
	got, err := newSearcher(vec, stubEmbedder{}, stubDone{done: []int64{1}}).
		SearchHybrid(context.Background(), HybridParams{UserID: 7, Query: "hợp đồng", Limit: 2})
	if err != nil {
		t.Fatalf("SearchHybrid: %v", err)
	}
	if ids := pointIDs(got); !reflect.DeepEqual(ids, []string{"d2"}) {
		t.Errorf("sau lọc done = %v, want [d2]", ids)
	}
}

// Chunk xuất hiện ở CẢ HAI phía phải đứng trên chunk một phía — luật cơ bản
// của Reciprocal Rank Fusion mà pipeline dựa vào.
func TestFuseRRFRanksAgreeingChunksFirst(t *testing.T) {
	dense := []*domain.ScoredChunk{hit("only_dense", 1, ""), hit("both", 1, "")}
	text := []*domain.ScoredChunk{hit("only_text", 1, ""), hit("both", 1, "")}
	got := FuseRRF(dense, text)
	if len(got) != 3 {
		t.Fatalf("fuse = %d hit, want 3 (gộp theo PointID)", len(got))
	}
	if got[0].PointID != "both" {
		t.Errorf("hạng nhất = %q, want both", got[0].PointID)
	}
	if got[0].Score <= got[1].Score {
		t.Errorf("điểm agree (%v) phải lớn hơn single (%v)", got[0].Score, got[1].Score)
	}
	// Rank 1 mỗi phía cùng điểm → tie-break theo PointID để thứ tự ổn định.
	if got[1].PointID != "only_dense" || got[2].PointID != "only_text" {
		t.Errorf("đuôi = [%q %q], want [only_dense only_text]", got[1].PointID, got[2].PointID)
	}
}

func TestSearchHybridRejectsEmptyQuery(t *testing.T) {
	if _, err := newSearcher(&stubVectors{}, stubEmbedder{}, stubDone{}).
		SearchHybrid(context.Background(), HybridParams{UserID: 7, Query: "   "}); err == nil {
		t.Fatal("query rỗng phải báo lỗi, không được hỏi Qdrant")
	}
}

func pointIDs(hits []*domain.ScoredChunk) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.PointID)
	}
	return out
}
