package usecase

import (
	"context"
	stderrors "errors"
	"net/http"
	"testing"

	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// TestPlanSessionHistoryWins là nghiệm thu sống còn của S1 ("server là nguồn
// duy nhất"): khi request mang conversation_id, lịch sử nạp từ DB và `history`
// client gửi lên BỊ BỎ hoàn toàn — không trộn, không "client nói gì tin nấy".
// Trộn 2 nguồn là mở đường cho client tự nhét câu hỏi giả vào bối cảnh.
func TestPlanSessionHistoryWins(t *testing.T) {
	svc := &chatService{}
	dto := &dtos.ChatRequest{
		Message:        "Vậy thì mức phạt là bao nhiêu?",
		ConversationID: 5,
		History:        []dtos.ChatHistoryItem{{Role: "user", Content: "client tự bịa"}},
	}
	session := &port.ChatSession{
		ConversationID: 5,
		History: []dtos.ChatHistoryItem{
			{Role: "user", Content: "Phụ cấp đi công tác?"},
			{Role: "assistant", Content: "Quy định ở Điều 13[^1]."},
		},
		CategoryIDs: []int64{3, 9},
		Provider:    "openrouter",
		Model:       "google/gemma-4-31b-it:free",
		Thinking:    true,
	}

	in := svc.plan(dto, session)
	if len(in.History) != 2 || in.History[0].Content != "Phụ cấp đi công tác?" {
		t.Errorf("history = %+v, want bản từ DB", in.History)
	}
	// ba lựa chọn còn lại chỉ được kế thừa vì client KHÔNG gửi (C4: phiên nhớ
	// lựa chọn của nó để lượt sau không phải gửi lại).
	if in.Provider != "openrouter" || in.Model != "google/gemma-4-31b-it:free" || !in.Thinking {
		t.Errorf("lựa chọn kế thừa từ phiên = %+v", in)
	}
	if len(in.CategoryIDs) != 2 || in.CategoryIDs[0] != 3 {
		t.Errorf("scope = %v, want [3,9] từ phiên", in.CategoryIDs)
	}
}

// TestPlanRequestFieldBeatsSession khoá chiều ngược lại: field client gửi rõ
// ràng thì phiên KHÔNG được đè lên — chọn nhầm provider rồi mà phiên bắt phải
// dùng provider cũ là không sửa nổi hội thoại.
func TestPlanRequestFieldBeatsSession(t *testing.T) {
	svc := &chatService{}
	dto := &dtos.ChatRequest{
		Message:     "hỏi",
		Provider:    "cloud",
		Model:       "model-moi",
		CategoryIDs: []int64{1},
	}
	session := &port.ChatSession{
		CategoryIDs: []int64{3, 9},
		Provider:    "openrouter",
		Model:       "model-cu",
		Thinking:    true,
	}

	in := svc.plan(dto, session)
	if in.Provider != "cloud" || in.Model != "model-moi" {
		t.Errorf("provider/model = %q/%q, want bản client thắng", in.Provider, in.Model)
	}
	if len(in.CategoryIDs) != 1 || in.CategoryIDs[0] != 1 {
		t.Errorf("scope = %v, want [1] client ghi đè", in.CategoryIDs)
	}
	// thinking là bool nên client không có cách nào TẮT cờ phiên đã bật — false
	// bị coi như "không gửi". S1 chấp nhận (chỉ sai hướng "theo phiên"),
	// muốn sửa thì đổi field sang *bool, và test này sẽ bắt được chỗ phải đổi.
	if !in.Thinking {
		t.Errorf("thinking = false, want kế thừa true từ phiên (bool zero = không gửi)")
	}
}

// TestPlanWithoutSessionKeepsClientHistory chứng minh đường cũ không hỏng: FE
// chưa truyền conversation_id thì history client vẫn được tin như trước.
func TestPlanWithoutSessionKeepsClientHistory(t *testing.T) {
	svc := &chatService{}
	dto := &dtos.ChatRequest{
		Message:  "hỏi",
		History:  []dtos.ChatHistoryItem{{Role: "user", Content: "đáp của client"}},
		Provider: "ollama",
		Thinking: true,
		Limit:    3,
	}

	in := svc.plan(dto, nil)
	if len(in.History) != 1 || in.History[0].Content != "đáp của client" {
		t.Errorf("history = %+v, want nguyên của client", in.History)
	}
	if in.Provider != "ollama" || !in.Thinking {
		t.Errorf("in = %+v", in)
	}
}

// TestResolveSessionNoConversationIDIsLegacyPath: không có id thì cổng lưu
// không được chạm tới — nếu chạm thì mọi request cũ đều thành một lần đọc DB
// thừa, và test fake sẽ vỡ vì stub trả nil.
func TestResolveSessionNoConversationIDIsLegacyPath(t *testing.T) {
	store := &chatSessionStub{}
	svc := &chatService{sessions: store}

	session, err := svc.resolveSession(context.Background(), &dtos.ChatRequest{Message: "hỏi"}, 7)
	if err != nil || session != nil {
		t.Fatalf("session = %+v err = %v, want nil/nil", session, err)
	}
	if store.loadCalls != 0 {
		t.Errorf("Load bị gọi %d lần dù request không có conversation_id", store.loadCalls)
	}
}

// TestResolveSessionNilStoreRunsChatWithoutPersistence: sessions == nil là
// đường test/fake — chat vẫn phải chạy, không panic khi compose gọi persist.
func TestResolveSessionNilStoreRunsChatWithoutPersistence(t *testing.T) {
	svc := &chatService{}
	dto := &dtos.ChatRequest{Message: "hỏi", ConversationID: 5}

	session, err := svc.resolveSession(context.Background(), dto, 7)
	if err != nil || session != nil {
		t.Fatalf("session = %+v err = %v, want nil/nil khi cổng lưu tắt", session, err)
	}

	svc.persist(context.Background(), &port.ChatSession{ConversationID: 5}, turnInput{}, 7, "hỏi", "đáp", nil)
}

// TestResolveSessionPassesUserID khoá điều kiện quyền: repo lọc theo user_id,
// nên user_id phải được truyền xuống — truyền sai là đọc được phiên người khác.
func TestResolveSessionPassesUserID(t *testing.T) {
	store := &chatSessionStub{loaded: &port.ChatSession{ConversationID: 5}}
	svc := &chatService{sessions: store}

	_, err := svc.resolveSession(context.Background(), &dtos.ChatRequest{ConversationID: 5}, 7)
	if err != nil {
		t.Fatalf("resolveSession lỗi: %v", err)
	}
	if store.gotConvID != 5 || store.gotUserID != 7 {
		t.Errorf("Load nhận (%d,%d), want (5,7)", store.gotConvID, store.gotUserID)
	}
}

// TestResolveSessionPropagatesNotFound: phiên của người khác trả về từ repo là
// domain.ErrNotFound, resolveSession phải để lỗi đi nguyên để Chat dịch ra 404
// qua sessionError — tự吞 lỗi thành nil là im lặng mất lịch sử.
func TestResolveSessionPropagatesNotFound(t *testing.T) {
	store := &chatSessionStub{loadErr: domain.ErrNotFound}
	svc := &chatService{sessions: store}

	_, err := svc.resolveSession(context.Background(), &dtos.ChatRequest{ConversationID: 5}, 7)
	if !stderrors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
	if code := errors.From(sessionError(err)); code == nil || code.GetHttpCode() != http.StatusNotFound {
		t.Errorf("sessionError maps = %+v, want 404", code)
	}
	if code := errors.From(sessionError(stderrors.New("qdrant chết"))); code != nil {
		t.Errorf("lỗi hạ tầng bị sessionError sửa thành %+v, want giữ nguyên", code)
	}
}

// TestPersistWritesMergedChoices: lượt ghi xuống phải mang provider/model/
// thinking VÀ lựa chọn thực sự đã dùng (bản hợp nhất), không phải bản client
// gửi — lưu sai thì mở lại phiên là đổi cả model đang chạy.
func TestPersistWritesMergedChoices(t *testing.T) {
	store := &chatSessionStub{}
	svc := &chatService{sessions: store}
	in := turnInput{
		CategoryIDs: []int64{3, 9},
		Provider:    "openrouter",
		Model:       "google/gemma-4-31b-it:free",
		Thinking:    true,
	}
	cites := []*dtos.ChatCite{{N: 1, ArticleID: 8, PageNum: 4}}

	svc.persist(context.Background(), &port.ChatSession{ConversationID: 5}, in, 7, "hỏi", "đáp[^1]", cites)

	turn := store.turn
	if turn == nil {
		t.Fatal("persist không ghi lượt nào")
	}
	if turn.ConversationID != 5 || turn.UserID != 7 || turn.Question != "hỏi" || turn.Answer != "đáp[^1]" {
		t.Errorf("turn = %+v", turn)
	}
	if turn.Provider != "openrouter" || !turn.Thinking || len(turn.CategoryIDs) != 2 {
		t.Errorf("lựa chọn đã hợp nhất không xuống DB: %+v", turn)
	}
	if len(turn.Cites) != 1 || turn.Cites[0].ArticleID != 8 {
		t.Errorf("cites = %+v, phải lưu theo message", turn.Cites)
	}
}

// TestPersistFailureDoesNotBreakAnswer: DB chết sau khi model đã đáp xong thì
// người dùng vẫn nhận được câu trả lời — persist chỉ log, không trả error.
// Hành vi này là đánh đổi có chủ đích (xem comment trong persist).
func TestPersistFailureDoesNotBreakAnswer(t *testing.T) {
	store := &chatSessionStub{appendErr: stderrors.New("db chết")}
	svc := &chatService{sessions: store}

	svc.persist(context.Background(), &port.ChatSession{ConversationID: 5}, turnInput{}, 7, "hỏi", "đáp", nil)
	if store.turn == nil {
		t.Error("AppendTurn không được gọi dù có session")
	}
}

// ---- stub ----

// chatSessionStub hiện thân port.IChatSessions, ghi lại tham số để test asserts.
type chatSessionStub struct {
	loaded    *port.ChatSession
	loadErr   error
	appendErr error
	loadCalls int
	// gotConvID/gotUserID là tham số của lần Load cuối.
	gotConvID int64
	gotUserID int64
	// turn là tham số của lần AppendTurn cuối.
	turn *port.ChatTurn
}

func (s *chatSessionStub) Load(_ context.Context, conversationID, userID int64) (*port.ChatSession, error) {
	s.loadCalls++
	s.gotConvID, s.gotUserID = conversationID, userID
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return s.loaded, nil
}

func (s *chatSessionStub) AppendTurn(_ context.Context, turn *port.ChatTurn) error {
	s.turn = turn
	return s.appendErr
}

var _ port.IChatSessions = (*chatSessionStub)(nil)
