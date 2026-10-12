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
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/controller/dtos"
)

type chatService struct {
	providers   *eino.ProviderAgents
	classifier  *question.LLMClassifier
	currentUser port.IUserResolver
}

// NewChatService dựng chat agentic thuần: 1 lượt agent chạy toàn bộ vòng
// [nhận định → kế hoạch → gọi tool → quan sát → điều chỉnh] — chọn tool,
// tổng hợp đều do agent trong loop, Go chỉ còn validate, chọn provider theo
// request, phạm vi tin cậy, dán nhãn question_type và dựng cites. Không
// Route/switch trong Go.
func NewChatService(providers *eino.ProviderAgents, classifier *question.LLMClassifier, currentUser port.IUserResolver) port.IChatter {
	return &chatService{providers: providers, classifier: classifier, currentUser: currentUser}
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

// followUpMarkers là từ hồi chiếu báo hiệu câu nối tiếp không tự đứng được
// ("con số đó", "vậy thì...", "trường hợp kia"...). Model đôi khi đáp thẳng
// từ câu trả lời cũ mà không search (PD từng 0 cites, nhãn chitchat sai) nên
// Go chặn 1 lần: lượt nối tiếp mà agent không gọi tool thì chạy lại với chỉ
// thị ép tìm kiếm. Chỉ 1 retry để không bùng chi phí.
var followUpMarkers = []string{
	"vừa", "đó", "này", "kia", "nó ", "còn", "vậy", "trường hợp",
	"con số", "số đó", "điều đó", "khoản đó", "trên", "kia thì",
}

// isFollowUp nhận diện câu nối tiếp cần căn cứ mới: có lịch sử và message
// chứa từ hồi chiếu. Chào/cảm ơn thuần túy không dính marker nên lọt qua,
// không tốn retry oan.
func isFollowUp(message string, history []dtos.ChatHistoryItem) bool {
	if len(history) == 0 {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(message))
	for _, m := range followUpMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// followUpRetryNote là chỉ thị ép tìm kiếm gắn vào message khi retry —
// nhắc lại quy tắc prompt §11 bằng lời ngắn để model không đáp thẳng nữa.
const followUpRetryNote = "\n\n(Hệ thống: đây là câu nối tiếp — bắt buộc viết lại thành câu độc lập đầy đủ theo lịch sử rồi gọi tool tìm kiếm, cấm đáp thẳng từ câu trả lời cũ.)"

func (s *chatService) Chat(ctx context.Context, dto *dtos.ChatRequest) dtos.Result[*dtos.ChatResponse] {
	if strings.TrimSpace(dto.Message) == "" {
		return dtos.Fail[*dtos.ChatResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "message rỗng"))
	}
	// Provider/model theo request, rỗng thì mặc định theo cấu hình — chọn
	// sai provider (chưa cấu hình key) thì 400 rõ lý do, không 500.
	agent, model, err := s.providers.Resolve(dto.Provider, dto.Model)
	if err != nil {
		return dtos.Fail[*dtos.ChatResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, err.Error()))
	}

	// Phạm vi tin cậy theo ctx cho tool đọc lúc thực thi: user từ token,
	// category ép từ DTO — không tin LLM.
	userID, _ := s.currentUser.UserID(ctx)
	ctx = eino.WithScope(ctx, eino.Scope{UserID: userID, CategoryIDs: dto.CategoryIDs})

	res, err := agent.Run(ctx, model, dto.History, dto.Message)
	if err != nil {
		return dtos.Fail[*dtos.ChatResponse](err)
	}
	// Chốt chặn follow-up: lượt nối tiếp mà agent không đọc tài liệu (0 hit)
	// thì chạy lại 1 lần với chỉ thị ép search. Collector cùng request nên
	// hit retry gom tiếp, không mất cites lượt đầu (ở đây là không có).
	if len(res.Hits) == 0 && isFollowUp(dto.Message, dto.History) {
		logs.Warnw("chat: follow-up 0 hit, retry ép search", "message", dto.Message)
		if res2, err2 := agent.Run(ctx, model, dto.History, dto.Message+followUpRetryNote); err2 != nil {
			logs.Warnw("chat: retry follow-up lỗi, giữ kết quả đầu", "error", err2)
		} else {
			res = res2
		}
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
	qt := s.label(ctx, model, dto.Message, res.Searched)
	return dtos.Ok(&dtos.ChatResponse{Answer: res.Answer, QuestionType: qt, Cites: cites})
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
