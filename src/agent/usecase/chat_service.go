package usecase

import (
	"context"
	stderrors "errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/agent/question"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

type chatService struct {
	providers   *eino.ProviderAgents
	classifier  *question.LLMClassifier
	currentUser port.IUserResolver
	// sessions là cổng lưu hội thoại (S1) — nil là TẮT tính lưu, để test/fake
	// dựng chatService không phải kéo theo DB.
	sessions port.IChatSessions
}

// NewChatService dựng chat agentic thuần: 1 lượt agent chạy toàn bộ vòng
// [nhận định → kế hoạch → gọi tool → quan sát → điều chỉnh] — chọn tool,
// tổng hợp đều do agent trong loop, Go chỉ còn validate, chọn provider theo
// request, phạm vi tin cậy, dán nhãn question_type và dựng cites. Không
// Route/switch trong Go.
func NewChatService(providers *eino.ProviderAgents, classifier *question.LLMClassifier, currentUser port.IUserResolver, sessions port.IChatSessions) port.IChatter {
	return &chatService{providers: providers, classifier: classifier, currentUser: currentUser, sessions: sessions}
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

// reasoningLimit là trần độ dài reasoning trả về FE — model bật `think` có thể
// nghĩ dài hơn cả câu trả lời, mà nó chỉ để người dùng mở ra đọc chứ không
// phải dữ liệu để xử lý tiếp (history chỉ gửi role/content). Cắt theo rune để
// không xé đôi ký tự Việt.
const reasoningLimit = 8000

func clipReasoning(text string) string {
	if utf8.RuneCountInString(text) <= reasoningLimit {
		return text
	}
	return string([]rune(text)[:reasoningLimit]) + "…"
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

// emptyAnswerRetryNote là chỉ thị khi agent trả answer rỗng (model free đôi
// khi trả content rỗng sau khi gọi tool đủ) — ép trả lại câu đầy đủ.
const emptyAnswerRetryNote = "\n\n(Hệ thống: câu trả lời vừa rồi bị trống — hãy trả lại đầy đủ nội dung, dựa trên các đoạn trích đã tìm được, kèm trích dẫn [^n].)"

func (s *chatService) Chat(ctx context.Context, dto *dtos.ChatRequest) dtos.Result[*dtos.ChatResponse] {
	if strings.TrimSpace(dto.Message) == "" {
		return dtos.Fail[*dtos.ChatResponse](
			errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "message rỗng"),
		)
	}
	// Phạm vi tin cậy theo ctx cho tool đọc lúc thực thi: user từ token,
	// category ép từ DTO — không tin LLM.
	userID, _ := s.currentUser.UserID(ctx)

	// Phiên (S1): có conversation_id thì lịch sử nạp từ DB, KHÔNG tin `history`
	// client gửi lên — hai nguồn cùng lúc là hai sự thật (§6.7). Không có id thì
	// y hành vi cũ: dùng history client, không lưu.
	session, err := s.resolveSession(ctx, dto, userID)
	if err != nil {
		return dtos.Fail[*dtos.ChatResponse](sessionError(err))
	}
	in := s.plan(dto, session)

	// Provider/model theo request, rỗng thì mặc định theo cấu hình — chọn
	// sai provider (chưa cấu hình key) thì 400 rõ lý do, không 500. thinking
	// bật thì đi bản agent đã dựng client có `think` (provider không hỗ trợ
	// thì Resolve tự fallback bản thường). Chốt slug THỰC đã chạy vào in để
	// response + lượt lưu ghi đúng provider, không phải chuỗi client gửi.
	provider := s.providers.EffectiveProvider(in.Provider)
	agent, model, err := s.providers.Resolve(provider, in.Model, in.Thinking)
	if err != nil {
		return dtos.Fail[*dtos.ChatResponse](
			errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, err.Error()),
		)
	}
	in.Provider = provider
	in.Model = model

	ctx = eino.WithScope(ctx, eino.Scope{UserID: userID, CategoryIDs: in.CategoryIDs})

	// Mỗi lượt 1 dòng: khi nhìn log (hoặc dashboard provider) phải phân biệt
	// được call LLM của chat với call của worker compile — cùng model nhưng
	// khác phase, khác lý do.
	// site=chat cho MỌI request model của lượt này (agent loop có thể gọi
	// model nhiều vòng vì tool) — transport in kèm nhãn nên dashboard provider
	// đối chiếu được request nào là chat, request nào là worker compile.
	ctx = utils.WithLLMSite(ctx, constants.LLMSiteChat)
	// Tệp đính kèm chỉ chèn vào ngữ cảnh của LƯỢT này; bản lưu phiên và nhãn
	// câu hỏi vẫn dùng câu hỏi gốc (xem withAttachment).
	message := withAttachment(dto.Message, dto.Attachment)
	res, err := s.run(ctx, agent, model, in, message)
	if err != nil {
		// Lỗi HTTP của provider (model sai, hết quota...) trả mã đọc được, lỗi
		// không kết nối được dịch vụ AI phía dưới (chưa bật Ollama local cho
		// embedding, Qdrant tắt) trả 503 — xem providerError.
		return dtos.Fail[*dtos.ChatResponse](providerError(err, model))
	}
	return dtos.Ok(s.compose(ctx, res, model, dto.Message, in, userID, session))
}

// compose dựng response cuối của một lượt: đánh số cites, chuẩn hoá khuôn [^n]
// trong answer, dán nhãn question_type, graft biểu đồ tool đã dựng, và ghi lượt
// xuống phiên nếu có.
//
// Tách khỏi Chat() vì Chat đã đầy ngân sách (guardrail
// src/quality/function_budget_test.go), không phải vì nó tái sử dụng được.
func (s *chatService) compose(ctx context.Context, res eino.RunResult, model, message string,
	in turnInput, userID int64, session *port.ChatSession) *dtos.ChatResponse {
	cites := buildCites(res.Hits)
	charts := buildCharts(res.Charts)
	// Answer do model viết nên còn lệch khuôn ([*2], số bịa ra, "Article ID 54") —
	// BE chốt lại theo đúng cites vừa dựng ở trên trước khi trả FE.
	answer, droppedMarkers := normalizeCitations(res.Answer, len(cites))
	if droppedMarkers > 0 {
		logs.Warnw("chat: bỏ marker trích dẫn không có trong cites",
			"trace_id", utils.TraceIDFromCtx(ctx),
			"dropped_markers", droppedMarkers, "cites", len(cites))
	}
	s.persist(ctx, session, in, userID, message, answer, cites, charts)
	return &dtos.ChatResponse{
		Answer:       answer,
		QuestionType: s.label(ctx, model, message, res.Searched),
		Provider:     in.Provider,
		Model:        model,
		Cites:        cites,
		Charts:       charts,
		Reasoning:    clipReasoning(res.Reasoning),
	}
}

// turnInput là kế hoạch một lượt sau khi hoà request với phiên đã lưu: lượt nào
// thắng chỗ nào ghi ngay trong plan(), đừng để Chat() phải so if từng field.
type turnInput struct {
	History     []dtos.ChatHistoryItem
	CategoryIDs []int64
	Provider    string
	Model       string
	Thinking    bool
}

// resolveSession đọc phiên của user; trả nil khi request không gắn
// conversation_id (đường cũ) hoặc khi cổng lưu bị tắt (test/fake).
func (s *chatService) resolveSession(ctx context.Context, dto *dtos.ChatRequest, userID int64) (*port.ChatSession, error) {
	if dto.ConversationID <= 0 || s.sessions == nil {
		return nil, nil
	}
	return s.sessions.Load(ctx, dto.ConversationID, userID)
}

// plan hợp nhất request với phiên: request LUÔN thắng từng field rõ ràng, chỉ
// những field client bỏ trống mới lấy của phiên (§6.7 C4/S4: phiên nhớ lựa chọn
// của nó để lượt sau không phải gửi lại). Lịch sử: có phiên là lấy từ DB,
// history client bị bỏ qua — không trộn.
func (s *chatService) plan(dto *dtos.ChatRequest, session *port.ChatSession) turnInput {
	in := turnInput{
		History:     dto.History,
		CategoryIDs: dto.CategoryIDs,
		Provider:    dto.Provider,
		Model:       dto.Model,
		Thinking:    dto.Thinking,
	}
	if session == nil {
		return in
	}
	in.History = session.History
	if len(in.CategoryIDs) == 0 {
		in.CategoryIDs = session.CategoryIDs
	}
	if in.Provider == "" {
		in.Provider = session.Provider
	}
	if in.Model == "" {
		in.Model = session.Model
	}
	if !in.Thinking {
		in.Thinking = session.Thinking
	}
	return in
}

// persist ghi lượt thành công xuống phiên. LƯU HỎNG KHÔNG làm hỏng response:
// người dùng đã trả token và đã có câu trả lời đúng, trả 500 sau khi model chạy
// xong là đánh đổi tệ hơn để nguyên một lượt mất trắng trong DB — nên log ERROR
// thật to (kèm conversation_id để chữa tay nếu cần) rồi đi tiếp.
func (s *chatService) persist(ctx context.Context, session *port.ChatSession, in turnInput,
	userID int64, question, answer string, cites []*dtos.ChatCite, charts []*dtos.ChatChart) {
	// hai điều kiện chặn giống resolveSession: không có phiên (request cũ) hoặc
	// không có cổng lưu (fake/test) thì coi như chế độ lưu đang tắt.
	if session == nil || s.sessions == nil {
		return
	}
	err := s.sessions.AppendTurn(ctx, &port.ChatTurn{
		ConversationID: session.ConversationID,
		UserID:         userID,
		Question:       question,
		Answer:         answer,
		Cites:          cites,
		Charts:         charts,
		CategoryIDs:    in.CategoryIDs,
		Provider:       in.Provider,
		Model:          in.Model,
		Thinking:       in.Thinking,
	})
	if err != nil {
		logs.Error(err, "chat: lưu lượt vào phiên thất bại (response vẫn trả đủ)",
			"trace_id", utils.TraceIDFromCtx(ctx),
			"conversation_id", session.ConversationID, "user_id", userID)
	}
}

// sessionError: phiên không tồn tại HOẶC không phải của user → 404 như nhau;
// lỗi hạ tầng khác để Render lo (500 + log stack).
func sessionError(err error) error {
	if stderrors.Is(err, domain.ErrNotFound) {
		return errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "phiên chat không tồn tại")
	}
	return err
}

// buildCites đánh số trích dẫn 1..N theo đúng thứ tự hit collector gom được.
// normalizeCitations chuẩn hoá marker trong answer theo trần N này, nên hai
// chỗ phải dùng chung một cách đếm — tách khỏi Chat để hàm chính không phình
// quá ngưỡng guardrail (src/quality/function_budget_test.go).
func buildCites(hits []*domain.ScoredChunk) []*dtos.ChatCite {
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
	return cites
}

// buildCharts map biểu đồ tool chart_from_cite đã dựng (nằm trong collector)
// ra DTO cho frontend, đối xứng với buildCites. Chart đến từ collector chứ không
// từ lời model — model không có đường nào nhét số vào spec (Đợt 26 §2). Trả
// mảng rỗng khi lượt không vẽ gì, FE không phải gặp null.
func buildCharts(charts []*eino.CollectedChart) []*dtos.ChatChart {
	out := make([]*dtos.ChatChart, 0, len(charts))
	for _, ch := range charts {
		if ch == nil {
			continue
		}
		out = append(out, &dtos.ChatChart{
			N:       ch.N,
			Kind:    ch.Kind,
			Spec:    ch.Spec,
			Caption: ch.Caption,
		})
	}
	return out
}

// run gọi agent 1 lượt kèm log call + chốt chặn retry — tách khỏi Chat để hàm
// chính không phình quá ngưỡng (guardrail src/quality/function_budget_test.go).
//
// Retry: câu nối tiếp 0 hit thì ép search; agent trả answer rỗng (model free
// thỉnh thoảng trả content rỗng) thì chạy lại với chỉ thị trả lời đầy đủ.
// Collector cùng request nên hit retry gom tiếp, không mất cites lượt đầu —
// chỉ 1 retry mỗi loại để không bùng chi phí.
func (s *chatService) run(ctx context.Context, agent *eino.ChatAgent, model string, in turnInput, message string) (eino.RunResult, error) {
	logs.Infow("chat: gọi agent", "trace_id", utils.TraceIDFromCtx(ctx),
		"model", model, "history", len(in.History),
		"message_chars", len([]rune(message)))
	res, err := agent.Run(ctx, model, in.History, message)
	if err != nil {
		return eino.RunResult{}, err
	}
	retryNote := ""
	switch {
	case strings.TrimSpace(res.Answer) == "":
		// Log chỉ cần biết câu hỏi dài bao nhiêu — nội dung user đã có trong
		// request, lặp lại vào log chỉ làm tràn dòng và phơi luôn chữ của user
		// ở nơi người vận hành không cần đọc.
		logs.Warnw("chat: answer rỗng, retry", "trace_id", utils.TraceIDFromCtx(ctx),
			"message_chars", len([]rune(message)))
		retryNote = emptyAnswerRetryNote
	case len(res.Hits) == 0 && isFollowUp(message, in.History):
		logs.Warnw("chat: follow-up 0 hit, retry ép search", "trace_id", utils.TraceIDFromCtx(ctx),
			"message_chars", len([]rune(message)), "history", len(in.History))
		retryNote = followUpRetryNote
	}
	if retryNote != "" {
		if res2, err2 := agent.Run(ctx, model, in.History, message+retryNote); err2 != nil {
			logs.Warnw("chat: retry lỗi, giữ kết quả đầu", "trace_id", utils.TraceIDFromCtx(ctx),
				"error", err2)
		} else {
			res = res2
		}
	}
	return res, nil
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
	classifyCtx := utils.WithLLMSite(ctx, constants.LLMSiteClassify)
	return s.classifier.Classify(classifyCtx, model, message).String()
}
