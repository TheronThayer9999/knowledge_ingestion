package usecase

import (
	"context"
	"net/http"
	"strings"

	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/agent/question"
	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

type searchService struct {
	hybrid      *retrieval.HybridSearcher
	classifier  *question.LLMClassifier
	currentUser port.IUserResolver
}

func NewSearchService(
	embedder domain.Embedder,
	vectors domain.IVectorStore,
	articleRepo domain.IArticleRepository,
	llm domain.ILLM,
	currentUser port.IUserResolver,
) port.ISearcher {
	return &searchService{
		hybrid:      retrieval.NewHybridSearcher(embedder, vectors, articleRepo),
		classifier:  question.NewLLMClassifier(llm),
		currentUser: currentUser,
	}
}

// Search chạy đúng 2 vòng agentic (§8 AGENT_LLM_INSTRUCTIONS.md). Vòng 1:
// phân loại câu hỏi bằng LLM (có dto.Model) rồi router chọn tool + params —
// không model thì default lookup, không tốn lượt LLM. Vòng 2: gọi tool
// knowledge_search (pipeline hybrid) theo params, trừ chitchat skip tool.
// Lỗi hạ tầng trả 500 (không phải lỗi client); query rỗng/whitespace thì 400.
func (s *searchService) Search(ctx context.Context, dto *dtos.SearchRequest) dtos.Result[*dtos.SearchResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	if strings.TrimSpace(dto.Query) == "" {
		return dtos.Fail[*dtos.SearchResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "query rỗng"))
	}
	// Vòng 1: classify + route.
	qtype := question.QuestionLookup
	if strings.TrimSpace(dto.Model) != "" {
		qtype = s.classifier.Classify(ctx, dto.Model, dto.Query)
	}
	route := question.Route(qtype)
	// DTO caller tự chỉ định thì thắng router — router chỉ là default theo loại.
	limit := dto.Limit
	if limit <= 0 {
		limit = route.Limit
	}
	if limit <= 0 {
		limit = constants.SEARCH_DEFAULT_LIMIT
	}
	if limit > constants.SEARCH_MAX_LIMIT {
		limit = constants.SEARCH_MAX_LIMIT
	}
	// Vòng 2: chitchat skip tool — đỡ tốn 1 lượt embed + Qdrant vô ích.
	if route.SkipSearch && dto.Limit <= 0 {
		return dtos.Ok(&dtos.SearchResponse{
			Hits:         []*dtos.SearchHitResponse{},
			Total:        0,
			QuestionType: qtype.String(),
		})
	}
	hits, err := s.hybrid.SearchHybrid(ctx, retrieval.HybridParams{
		UserID:         userID,
		Query:          dto.Query,
		CategoryID:     dto.CategoryID,
		Limit:          limit,
		ScoreThreshold: dto.ScoreThreshold,
	})
	if err != nil {
		return dtos.Fail[*dtos.SearchResponse](err)
	}
	res := &dtos.SearchResponse{Hits: make([]*dtos.SearchHitResponse, 0, len(hits)), QuestionType: qtype.String()}
	for _, h := range hits {
		res.Hits = append(res.Hits, dtos.ToSearchHitResponse(h))
	}
	res.Total = len(res.Hits)
	return dtos.Ok(res)
}
