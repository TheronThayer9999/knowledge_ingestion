package services

import (
	"context"
	"net/http"
	"strings"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
)

// ISearchService truy hồi tri thức cho agent RAG — HTTP và gRPC cùng gọi,
// trả đoạn text gần câu hỏi nhất trong phạm vi quyền của người gọi.
type ISearchService interface {
	// Search embed câu hỏi rồi tìm top chunk: filter user_id (owner từ
	// token) + category nếu có, chỉ giữ hit thuộc bài đã embed done để
	// agent không trả lời trên ngữ cảnh đang embed dở.
	Search(ctx context.Context, dto *dtos.SearchRequest) dtos.Result[*dtos.SearchResponse]
}

type searchService struct {
	hybrid      *HybridSearcher
	currentUser middlewares.ICurrentUser
}

func NewSearchService(embedder domain.Embedder, vectors domain.IVectorStore, articleRepo domain.IArticleRepository, currentUser middlewares.ICurrentUser) ISearchService {
	return &searchService{hybrid: NewHybridSearcher(embedder, vectors, articleRepo), currentUser: currentUser}
}

// Search chuẩn hóa limit rồi gọi pipeline hybrid (embed → dense + text → RRF
// fuse → chặn bài chưa done, xem HybridSearcher). Lỗi hạ tầng trả 500 (không
// phải lỗi client); query rỗng/whitespace thì 400.
func (s *searchService) Search(ctx context.Context, dto *dtos.SearchRequest) dtos.Result[*dtos.SearchResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	if strings.TrimSpace(dto.Query) == "" {
		return dtos.Fail[*dtos.SearchResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "query rỗng"))
	}
	limit := dto.Limit
	if limit <= 0 {
		limit = constants.SEARCH_DEFAULT_LIMIT
	}
	if limit > constants.SEARCH_MAX_LIMIT {
		limit = constants.SEARCH_MAX_LIMIT
	}
	hits, err := s.hybrid.SearchHybrid(ctx, HybridParams{
		UserID:         userID,
		Query:          dto.Query,
		CategoryID:     dto.CategoryID,
		Limit:          limit,
		ScoreThreshold: dto.ScoreThreshold,
	})
	if err != nil {
		return dtos.Fail[*dtos.SearchResponse](err)
	}
	res := &dtos.SearchResponse{Hits: make([]*dtos.SearchHitResponse, 0, len(hits))}
	for _, h := range hits {
		res.Hits = append(res.Hits, dtos.ToSearchHitResponse(h))
	}
	res.Total = len(res.Hits)
	return dtos.Ok(res)
}
