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

// ISearchService truy hồi tri thức cho agent RAG — chỉ API gọi, trả đoạn
// text gần câu hỏi nhất trong phạm vi quyền của người gọi.
type ISearchService interface {
	// Search embed câu hỏi rồi tìm top chunk: filter user_id (owner từ
	// token) + category nếu có, chỉ giữ hit thuộc bài đã embed done để
	// agent không trả lời trên ngữ cảnh đang embed dở.
	Search(ctx context.Context, dto *dtos.SearchRequest) dtos.Result[*dtos.SearchResponse]
}

type searchService struct {
	embedder    domain.Embedder
	vectors     domain.IVectorStore
	articleRepo domain.IArticleRepository
	currentUser middlewares.ICurrentUser
}

func NewSearchService(embedder domain.Embedder, vectors domain.IVectorStore, articleRepo domain.IArticleRepository, currentUser middlewares.ICurrentUser) ISearchService {
	return &searchService{embedder: embedder, vectors: vectors, articleRepo: articleRepo, currentUser: currentUser}
}

// Search chuẩn hóa limit rồi đi 3 bước: embed câu hỏi → query Qdrant có
// filter quyền → chặn hit của bài chưa done. Lỗi embed/query trả 500 (lỗi
// hạ tầng, không phải lỗi client); query rỗng/whitespace thì 400.
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
	vec, err := s.embedder.Embed(ctx, dto.Query)
	if err != nil {
		return dtos.Fail[*dtos.SearchResponse](err)
	}
	hits, err := s.vectors.Search(ctx, vec, domain.SearchFilter{
		UserID:     userID,
		CategoryID: dto.CategoryID,
	}, limit, dto.ScoreThreshold)
	if err != nil {
		return dtos.Fail[*dtos.SearchResponse](err)
	}
	if len(hits) == 0 {
		return dtos.Ok(&dtos.SearchResponse{Hits: []*dtos.SearchHitResponse{}, Total: 0})
	}
	// Chặn hit của bài chưa done — vector lên Qdrant theo đợt nên bài đang
	// embed dở có ngữ cảnh thiếu, trả cho agent là trả lời bừa.
	ids := make([]int64, 0, len(hits))
	seen := make(map[int64]bool, len(hits))
	for _, h := range hits {
		if !seen[h.ArticleID] {
			seen[h.ArticleID] = true
			ids = append(ids, h.ArticleID)
		}
	}
	done, err := s.articleRepo.ListDoneIDs(ctx, userID, ids)
	if err != nil {
		return dtos.Fail[*dtos.SearchResponse](err)
	}
	allowed := make(map[int64]bool, len(done))
	for _, id := range done {
		allowed[id] = true
	}
	res := &dtos.SearchResponse{Hits: make([]*dtos.SearchHitResponse, 0, len(hits))}
	for _, h := range hits {
		if !allowed[h.ArticleID] {
			continue
		}
		res.Hits = append(res.Hits, dtos.ToSearchHitResponse(h))
	}
	res.Total = len(res.Hits)
	return dtos.Ok(res)
}
