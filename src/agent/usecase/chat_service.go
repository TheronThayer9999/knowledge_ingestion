package usecase

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/agent/question"
	"knowledge_ingestion/src/agent/synthesis"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

type chatService struct {
	search     port.ISearcher
	classifier *question.LLMClassifier
	synth      synthesis.IChatSynthesizer
}

func NewChatService(search port.ISearcher, llm domain.ILLM, prompts synthesis.Prompts, classify question.Prompts) port.IChatter {
	return &chatService{
		search:     search,
		classifier: question.NewLLMClassifier(llm, classify),
		synth:      synthesis.NewChatSynthesizer(llm, prompts),
	}
}

// snippetLen là độ dài trích đoạn mỗi cite — đủ để frontend hiện preview,
// không bê nguyên chunk dài vào response.
const snippetLen = 200

// snippet cắt text theo rune (không cắt giữa ký tự Việt) cho preview cite.
func snippet(text string) string {
	if utf8.RuneCountInString(text) <= snippetLen {
		return text
	}
	return string([]rune(text)[:snippetLen]) + "…"
}

func (s *chatService) Chat(ctx context.Context, dto *dtos.ChatRequest) dtos.Result[*dtos.ChatResponse] {
	if strings.TrimSpace(dto.Message) == "" {
		return dtos.Fail[*dtos.ChatResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "message rỗng"))
	}
	if strings.TrimSpace(dto.Model) == "" {
		return dtos.Fail[*dtos.ChatResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "model bắt buộc (chat cần LLM để tổng hợp)"))
	}

	// Vòng 1: LLM + câu hỏi + prompt system → loại câu hỏi → quyết định
	// tool nào (knowledge_search hay skip như chitchat). Model bắt buộc ở
	// chat nên luôn có lượt classify này; search bên dưới dùng lại kết quả
	// qua DTO, không tốn thêm lượt LLM.
	qtype := s.classifier.Classify(ctx, dto.Model, dto.Message)
	route := question.Route(qtype)

	// Vòng 2a: gọi tool đã chọn — skip thì hits rỗng, đỡ tốn embed + Qdrant.
	var hits []*dtos.SearchHitResponse
	if route.SkipSearch && dto.Limit <= 0 {
		hits = []*dtos.SearchHitResponse{}
	} else {
		searchRes := s.search.Search(ctx, &dtos.SearchRequest{
			Query:        dto.Message,
			CategoryID:   dto.CategoryID,
			Limit:        dto.Limit,
			Model:        dto.Model,
			QuestionType: qtype.String(),
		})
		if searchRes.Err != nil {
			return dtos.Fail[*dtos.ChatResponse](searchRes.Err)
		}
		hits = searchRes.Data.Hits
	}

	// Vòng 2b: mọi việc LLM (dựng prompt + gọi model) nằm trong synthesizer.
	answer, err := s.synth.Synthesize(ctx, dto.Model, dto.Message, qtype.String(), hits, dto.History)
	if err != nil {
		return dtos.Fail[*dtos.ChatResponse](err)
	}

	cites := make([]*dtos.ChatCite, 0, len(hits))
	for i, h := range hits {
		cites = append(cites, &dtos.ChatCite{
			N:          i + 1,
			ArticleID:  h.ArticleID,
			PageNum:    h.PageNum,
			ChunkIndex: h.ChunkIndex,
			Snippet:    snippet(h.Text),
		})
	}
	return dtos.Ok(&dtos.ChatResponse{Answer: answer, QuestionType: qtype.String(), Cites: cites})
}
