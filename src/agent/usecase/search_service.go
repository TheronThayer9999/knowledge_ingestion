package usecase

import (
	"context"
	"net/http"
	"strings"

	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/agent/question"
	"knowledge_ingestion/src/agent/tools"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

type searchService struct {
	tools       *tools.Registry
	classifier  *question.LLMClassifier
	currentUser port.IUserResolver
}

func NewSearchService(
	toolsReg *tools.Registry,
	classifier *question.LLMClassifier,
	currentUser port.IUserResolver,
) port.ISearcher {
	return &searchService{
		tools:       toolsReg,
		classifier:  classifier,
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
	// Vòng 1: caller (HTTP) kèm sẵn loại hợp lệ thì dùng default router, khỏi
	// tốn lượt LLM — còn lại classify slug rồi Route default.
	qtype, ok := question.Parse(dto.QuestionType)
	if !ok {
		qtype = question.QuestionLookup
		if strings.TrimSpace(dto.Model) != "" {
			qtype = s.classifier.Classify(ctx, dto.Model, dto.Query)
		}
	}
	route := question.Route(qtype)
	// Vòng 2: thực thi duy nhất qua tools.Registry — skip/limit/chặn max nằm
	// trong registry, không fork mỗi service một kiểu.
	hits, err := s.tools.Execute(ctx, route, tools.Args{
		UserID:         userID,
		Query:          dto.Query,
		CategoryIDs:    dto.CategoryIDs,
		Limit:          dto.Limit,
		ScoreThreshold: dto.ScoreThreshold,
	})
	if err != nil {
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
