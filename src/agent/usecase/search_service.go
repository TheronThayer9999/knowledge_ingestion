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
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

type searchService struct {
	hybrid      *retrieval.HybridSearcher
	classifier  *question.LLMClassifier
	currentUser port.IUserResolver
}

func NewSearchService(
	hybrid *retrieval.HybridSearcher,
	classifier *question.LLMClassifier,
	currentUser port.IUserResolver,
) port.ISearcher {
	return &searchService{
		hybrid:      hybrid,
		classifier:  classifier,
		currentUser: currentUser,
	}
}

// Search chạy đúng 2 vòng agentic (§8 AGENT_LLM_INSTRUCTIONS.md). Vòng 1:
// phân loại câu hỏi bằng LLM (có dto.Model) rồi router chọn params — không
// model thì default lookup, không tốn lượt LLM. Vòng 2: pipeline hybrid,
// trừ chitchat skip luôn (khỏi tốn embed+Qdrant). Dịch vụ AI phía dưới không
// kết nối được (Ollama local chưa chạy, Qdrant tắt) → 503 kèm hướng kiểm tra;
// lỗi hạ tầng khác vẫn 500 như cũ; query rỗng/whitespace thì 400.
func (s *searchService) Search(ctx context.Context, dto *dtos.SearchRequest) dtos.Result[*dtos.SearchResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	if strings.TrimSpace(dto.Query) == "" {
		return dtos.Fail[*dtos.SearchResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "query rỗng"))
	}
	// Vòng 1: caller (HTTP) kèm sẵn loại hợp lệ thì dùng default router, khỏi
	// tốn lượt LLM — còn lại classify slug rồi Route default.
	qtype, ok := question.Parse(dto.QuestionType)
	if !ok {
		qtype = question.QuestionLookup
		if strings.TrimSpace(dto.Model) != "" {
			classifyCtx := utils.WithLLMSite(ctx, constants.LLMSiteClassify)
			qtype = s.classifier.Classify(classifyCtx, dto.Model, dto.Query)
		}
	}
	route := question.Route(qtype)
	// Vòng 2: chitchat không limit ép thì skip trước khi chạm embed/Qdrant;
	// caller ép limit vẫn search (ưu tiên ý caller).
	if route.SkipSearch && dto.Limit <= 0 {
		return dtos.Ok(&dtos.SearchResponse{Hits: []*dtos.SearchHitResponse{}, QuestionType: qtype.String()})
	}
	limit := dto.Limit
	if limit <= 0 {
		limit = route.Limit
	}
	hits, err := s.hybrid.SearchHybrid(ctx, retrieval.HybridParams{
		UserID:         userID,
		Query:          dto.Query,
		CategoryIDs:    dto.CategoryIDs,
		Limit:          limit,
		ScoreThreshold: dto.ScoreThreshold,
	})
	if err != nil {
		// searchService gọi thẳng pipeline nên không đi qua providerError — bắt
		// riêng lỗi không kết nối được dịch vụ để người dùng biết đường mở
		// Ollama/container lên, thay vì nhận 500 mù.
		if dep := dependencyError(err); dep != nil {
			return dtos.Fail[*dtos.SearchResponse](dep)
		}
		return dtos.Fail[*dtos.SearchResponse](err)
	}
	res := &dtos.SearchResponse{Hits: toHitResponses(hits), QuestionType: qtype.String()}
	res.Total = len(res.Hits)
	return dtos.Ok(res)
}

// toHitResponses map domain hits sang DTO — chat (cho synthesizer + cites) và
// search dùng chung, khỏi mỗi nơi một vòng lặp.
func toHitResponses(hits []*domain.ScoredChunk) []*dtos.SearchHitResponse {
	out := make([]*dtos.SearchHitResponse, 0, len(hits))
	for _, h := range hits {
		out = append(out, dtos.ToSearchHitResponse(h))
	}
	return out
}
