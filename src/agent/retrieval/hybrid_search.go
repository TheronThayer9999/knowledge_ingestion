package retrieval

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
	// CategoryIDs rỗng là mọi chủ đề, có thì khoanh nhiều chủ đề cùng lúc;
	// ArticleID > 0 thì khoanh 1 bài.
	CategoryIDs []int64
	ArticleID   int64
	Limit       int
	// ScoreThreshold lọc hit dense yếu trước khi fuse (thang cosine). Hit text
	// không có score nên không chịu ngưỡng này — vào fuse rồi tính.
	ScoreThreshold float32
}

// HybridSearcher pipeline hybrid dense + full-text — một entry duy nhất
// SearchHybrid cho tool gọi; FuseRRF giữ riêng vì là pure function có test
// thẳng. Không tách method con cho từng bước (YAGNI): tool mới cần bước lẻ
// thì tách sau, hiện tại mỗi wrapper một nơi gọi chỉ làm sâu luồng.
// Không dính DTO/envelope HTTP: trả domain để HTTP map response, agent tool
// map tool result.
// DoneLister là phần duy nhất của repository mà retrieval cần — thu hẹp
// dependency để test chỉ stub 1 method thay vì cả IArticleRepository.
type DoneLister interface {
	ListDoneIDs(ctx context.Context, userID int64, ids []int64) ([]int64, error)
}

type HybridSearcher struct {
	embedder    domain.Embedder
	vectors     domain.IVectorStore
	articleRepo DoneLister
}

func NewHybridSearcher(embedder domain.Embedder, vectors domain.IVectorStore, articleRepo DoneLister) *HybridSearcher {
	return &HybridSearcher{embedder: embedder, vectors: vectors, articleRepo: articleRepo}
}

// SearchHybrid pipeline đầy đủ trong một thân: embed query → dense + text →
// RRF fuse → cắt limit → chặn bài chưa done. Tuần tự chứ không song song:
// embed đã tốn giây, 2 query Qdrant chỉ ms nên song song không đáng độ phức
// tạp. Text là nửa bổ trợ — lỗi (collection cũ thiếu text index) thì degraded
// dense-only + warn, không fail cả query.
func (h *HybridSearcher) SearchHybrid(ctx context.Context, p HybridParams) ([]*domain.ScoredChunk, error) {
	if strings.TrimSpace(p.Query) == "" {
		return nil, fmt.Errorf("hybrid search: query rỗng")
	}
	// Limit chốt 1 chỗ duy nhất cho mọi đường gọi (DTO, gợi ý LLM, default):
	// rỗng thì default hệ thống, vượt thì kẹp trần.
	limit := p.Limit
	if limit <= 0 {
		limit = constants.SEARCH_DEFAULT_LIMIT
	}
	if limit > constants.SEARCH_MAX_LIMIT {
		limit = constants.SEARCH_MAX_LIMIT
	}
	filter := domain.SearchFilter{UserID: p.UserID, CategoryIDs: p.CategoryIDs, ArticleID: p.ArticleID}
	vec, err := h.embedder.Embed(ctx, p.Query)
	if err != nil {
		return nil, fmt.Errorf("hybrid search: embed query: %w", err)
	}
	dense, err := h.vectors.Search(ctx, vec, filter, limit, p.ScoreThreshold)
	if err != nil {
		return nil, err
	}
	var text = make([]*domain.ScoredChunk, 0)
	if hits, err := h.vectors.SearchText(ctx, p.Query, filter, limit); err != nil {
		logs.Warnw("hybrid: text search lỗi, chạy dense-only (collection cũ thiếu text index?)", "error", err)
	} else {
		text = hits
	}
	fused := FuseRRF(dense, text)
	if len(fused) > limit {
		fused = fused[:limit]
	}
	if len(fused) == 0 {
		return fused, nil
	}
	ids := make([]int64, 0, len(fused))
	seen := make(map[int64]bool, len(fused))
	for _, h := range fused {
		if !seen[h.ArticleID] {
			seen[h.ArticleID] = true
			ids = append(ids, h.ArticleID)
		}
	}
	done, err := h.articleRepo.ListDoneIDs(ctx, p.UserID, ids)
	if err != nil {
		return nil, fmt.Errorf("hybrid search: lọc bài done: %w", err)
	}
	allowed := make(map[int64]bool, len(done))
	for _, id := range done {
		allowed[id] = true
	}
	out := make([]*domain.ScoredChunk, 0, len(fused))
	for _, h := range fused {
		if allowed[h.ArticleID] {
			out = append(out, h)
		}
	}
	return out, nil
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
