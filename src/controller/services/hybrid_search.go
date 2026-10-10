package services

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/domain"
)

// HybridParams input thuần cho hybrid search — HTTP map từ DTO, agent tool
// (Eino Tool) map từ args, cùng gọi 1 pipeline nên 2 mặt ăn cùng ranking,
// khỏi fork logic search riêng cho agent.
type HybridParams struct {
	// UserID owner từ token — bắt buộc, service không tin client.
	UserID int64
	Query  string
	// CategoryID > 0 thì khoanh chủ đề, ArticleID > 0 thì khoanh 1 bài.
	CategoryID int64
	ArticleID  int64
	Limit      int
	// ScoreThreshold lọc hit dense yếu trước khi fuse (thang cosine). Hit text
	// không có score nên không chịu ngưỡng này — vào fuse rồi tính.
	ScoreThreshold float32
}

// HybridSearcher pipeline hybrid dense + full-text — tách method nhỏ để agent
// tool tái dùng từng bước (chỉ cần dense, chỉ fuse, chỉ lọc done...) thay vì
// bắt gọi nguyên pipeline. Không dính DTO/envelope HTTP: trả domain để HTTP
// map response, agent tool map tool result.
type HybridSearcher struct {
	embedder    domain.Embedder
	vectors     domain.IVectorStore
	articleRepo domain.IArticleRepository
}

func NewHybridSearcher(embedder domain.Embedder, vectors domain.IVectorStore, articleRepo domain.IArticleRepository) *HybridSearcher {
	return &HybridSearcher{embedder: embedder, vectors: vectors, articleRepo: articleRepo}
}

// SearchHybrid pipeline đầy đủ: embed query → dense + text → RRF fuse → cắt
// limit → chặn bài chưa done. Tuần tự chứ không song song: embed đã tốn
// giây, 2 query Qdrant chỉ ms nên song song không đáng độ phức tạp.
func (h *HybridSearcher) SearchHybrid(ctx context.Context, p HybridParams) ([]*domain.ScoredChunk, error) {
	if strings.TrimSpace(p.Query) == "" {
		return nil, fmt.Errorf("hybrid search: query rỗng")
	}
	limit := p.Limit
	if limit <= 0 {
		limit = constants.SEARCH_DEFAULT_LIMIT
	}
	filter := domain.SearchFilter{UserID: p.UserID, CategoryID: p.CategoryID, ArticleID: p.ArticleID}
	vec, err := h.EmbedQuery(ctx, p.Query)
	if err != nil {
		return nil, err
	}
	dense, err := h.DenseSearch(ctx, vec, filter, limit, p.ScoreThreshold)
	if err != nil {
		return nil, err
	}
	// Text là nửa bổ trợ — lỗi (collection cũ thiếu text index) thì degraded
	// dense-only, không fail cả query. Warn to để lộ chứ không nuốt thầm lặng.
	text := h.TextSearch(ctx, p.Query, filter, limit)
	fused := FuseRRF(dense, text)
	if len(fused) > limit {
		fused = fused[:limit]
	}
	return h.FilterDone(ctx, p.UserID, fused)
}

// EmbedQuery embed 1 câu hỏi thành vector — agent tool cần vector để tự query
// Qdrant thì gọi bước này thay vì gọi cả pipeline.
func (h *HybridSearcher) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vec, err := h.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("hybrid search: embed query: %w", err)
	}
	return vec, nil
}

// DenseSearch nửa semantic: top chunk gần vector nhất trong phạm vi filter.
// Threshold > 0 lọc hit yếu để không trả lời bừa.
func (h *HybridSearcher) DenseSearch(ctx context.Context, vec []float32, filter domain.SearchFilter, limit int, threshold float32) ([]*domain.ScoredChunk, error) {
	return h.vectors.Search(ctx, vec, filter, limit, threshold)
}

// TextSearch nửa keyword: chunk khớp từ khóa trong payload text. Lỗi thì trả
// nil + warn (degraded dense-only) — caller không phải check error.
func (h *HybridSearcher) TextSearch(ctx context.Context, query string, filter domain.SearchFilter, limit int) []*domain.ScoredChunk {
	hits, err := h.vectors.SearchText(ctx, query, filter, limit)
	if err != nil {
		logs.Warnw("hybrid: text search lỗi, chạy dense-only (collection cũ thiếu text index?)", "error", err)
		return nil
	}
	return hits
}

// RRFFusionK hằng số k của Reciprocal Rank Fusion (Cormack et al.) — 60 là
// chuẩn cộng đồng, đủ nén khoảng cách rank mà không san bằng hết.
const RRFFusionK = 60.0

// FuseRRF trộn N danh sách rank thành 1 (Reciprocal Rank Fusion):
// score = Σ 1/(k+rank). Không dùng score gốc vì thang cosine (dense) và thang
// text không so được nhau — rank mới là đơn vị chung. Chunk chỉ có 1 phía vẫn
// giữ (điểm 1 phía, rank thấp hơn chunk 2 phía cùng hạng). Pure function —
// test không cần Qdrant.
func FuseRRF(lists ...[]*domain.ScoredChunk) []*domain.ScoredChunk {
	scores := make(map[string]float64)
	rep := make(map[string]*domain.ScoredChunk)
	for _, list := range lists {
		for rank, h := range list {
			if h == nil {
				continue
			}
			scores[h.PointID] += 1.0 / (RRFFusionK + float64(rank+1))
			if _, ok := rep[h.PointID]; !ok {
				rep[h.PointID] = h
			}
		}
	}
	out := make([]*domain.ScoredChunk, 0, len(scores))
	for id, s := range scores {
		h := rep[id]
		out = append(out, &domain.ScoredChunk{
			PointID:    h.PointID,
			Score:      float32(s),
			ArticleID:  h.ArticleID,
			ChunkIndex: h.ChunkIndex,
			PageNum:    h.PageNum,
			Text:       h.Text,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].PointID < out[j].PointID
		}
		return out[i].Score > out[j].Score
	})
	return out
}

// FilterDone chặn hit của bài chưa embed done — vector lên Qdrant theo đợt nên
// bài đang embed dở có ngữ cảnh thiếu, trả cho agent là trả lời bừa. Agent
// tool gọi pipeline thủ công (không qua SearchHybrid) thì gọi bước này trước
// khi trả ngữ cảnh.
func (h *HybridSearcher) FilterDone(ctx context.Context, userID int64, hits []*domain.ScoredChunk) ([]*domain.ScoredChunk, error) {
	if len(hits) == 0 {
		return hits, nil
	}
	ids := make([]int64, 0, len(hits))
	seen := make(map[int64]bool, len(hits))
	for _, h := range hits {
		if !seen[h.ArticleID] {
			seen[h.ArticleID] = true
			ids = append(ids, h.ArticleID)
		}
	}
	done, err := h.articleRepo.ListDoneIDs(ctx, userID, ids)
	if err != nil {
		return nil, fmt.Errorf("hybrid search: lọc bài done: %w", err)
	}
	allowed := make(map[int64]bool, len(done))
	for _, id := range done {
		allowed[id] = true
	}
	out := make([]*domain.ScoredChunk, 0, len(hits))
	for _, h := range hits {
		if allowed[h.ArticleID] {
			out = append(out, h)
		}
	}
	return out, nil
}
