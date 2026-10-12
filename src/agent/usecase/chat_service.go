package usecase

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/agent/question"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
)

type chatService struct {
	agent       *eino.ChatAgent
	classifier  *question.LLMClassifier
	currentUser port.IUserResolver
}

// NewChatService dựng chat agentic thuần: 1 lượt agent chạy toàn bộ vòng
// [nhận định → kế hoạch → gọi tool → quan sát → điều chỉnh] — chọn tool,
// tổng hợp đều do agent trong loop, Go chỉ còn validate, phạm vi tin cậy,
// dán nhãn question_type và dựng cites. Không Route/switch trong Go.
func NewChatService(agent *eino.ChatAgent, classifier *question.LLMClassifier, currentUser port.IUserResolver) port.IChatter {
	return &chatService{agent: agent, classifier: classifier, currentUser: currentUser}
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
		return dtos.Fail[*dtos.ChatResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "model bắt buộc (chat cần LLM để chạy agent)"))
	}

	// Phạm vi tin cậy theo ctx cho tool đọc lúc thực thi: user từ token,
	// category ép từ DTO, model theo request — không tin LLM.
	userID, _ := s.currentUser.UserID(ctx)
	ctx = eino.WithModel(ctx, dto.Model)
	ctx = eino.WithScope(ctx, eino.Scope{UserID: userID, CategoryIDs: dto.CategoryIDs})

	res, err := s.agent.Run(ctx, dto.History, dto.Message)
	if err != nil {
		return dtos.Fail[*dtos.ChatResponse](err)
	}

	cites := make([]*dtos.ChatCite, 0, len(res.Hits))
	for i, h := range res.Hits {
		cites = append(cites, &dtos.ChatCite{
			N:          i + 1,
			ArticleID:  h.ArticleID,
			PageNum:    h.PageNum,
			ChunkIndex: h.ChunkIndex,
			Snippet:    snippet(h.Text),
		})
	}
	return dtos.Ok(&dtos.ChatResponse{Answer: res.Answer, QuestionType: s.label(ctx, dto.Model, dto.Message, len(cites) > 0), Cites: cites})
}

// label dán nhãn question_type cho response — agent trong loop không bị ép
// gọi classify, nhãn suy ra sau run: không đọc tài liệu thì chitchat (khỏi
// tốn lượt LLM), có đọc thì classify 1 lần (lỗi → lookup, không chặn trả lời).
func (s *chatService) label(ctx context.Context, model, message string, searched bool) string {
	if !searched {
		return question.QuestionChitchat.String()
	}
	if s.classifier == nil {
		return question.QuestionLookup.String()
	}
	return s.classifier.Classify(ctx, model, message).String()
}
