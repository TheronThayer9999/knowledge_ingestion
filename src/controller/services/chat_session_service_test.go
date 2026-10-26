package services

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
)

// TestAppendTurnOrder khoá ĐÚNG THỨ TỰ một lượt ghi xuống: 2 message trước, rồi
// dời mốc phiên, điền title, chốt phạm vi. Đảo bước 1 ra sau là có trạng thái
// "phiên đã touch nhưng chưa có message" — sidebar nhảy lên đầu mà mở ra trống.
func TestAppendTurnOrder(t *testing.T) {
	trace := &sessionTrace{}
	convRepo := &sessionStubConversations{trace: trace}
	msgRepo := &sessionStubMessages{trace: trace}
	svc := NewChatSessionStore(convRepo, msgRepo, sessionStubUoW{}, sessionStubCurrentUser{id: 7})

	err := svc.AppendTurn(context.Background(), &port.ChatTurn{
		ConversationID: 5,
		UserID:         7,
		Question:       "Phụ cấp công tác là bao nhiêu?",
		Answer:         "Mức phạt là 0.05%/ngày[^1].",
		Cites:          []*dtos.ChatCite{{N: 1, ArticleID: 8, PageNum: 4, Snippet: "Điều 13..."}},
		CategoryIDs:    []int64{3, 9},
		Provider:       "openrouter",
		Model:          "google/gemma-4-31b-it:free",
		Thinking:       true,
	})
	if err != nil {
		t.Fatalf("AppendTurn lỗi: %v", err)
	}

	want := []string{"messages.Append(2)", "conversations.TouchMessageTime(5)", "conversations.SetTitleIfEmpty(5)", "conversations.TouchScope(5)"}
	if len(trace.steps) != len(want) {
		t.Fatalf("trace = %v, want %v", trace.steps, want)
	}
	for i := range want {
		if trace.steps[i] != want[i] {
			t.Errorf("bước %d = %q, want %q", i, trace.steps[i], want[i])
		}
	}

	// Hai row phải là user→assistant với ĐÚNG nội dung, cites chỉ nằm ở row
	// assistant (câu hỏi thì chưa có nguồn nào).
	if len(msgRepo.appended) != 2 {
		t.Fatalf("số message ghi = %d, want 2", len(msgRepo.appended))
	}
	user, assistant := msgRepo.appended[0], msgRepo.appended[1]
	if user.Role != domain.MessageRoleUser || user.Content != "Phụ cấp công tác là bao nhiêu?" {
		t.Errorf("row user = %+v", user)
	}
	if user.Cites != "[]" {
		t.Errorf("câu hỏi không được mang cites, got %q", user.Cites)
	}
	if assistant.Role != domain.MessageRoleAssistant || !strings.Contains(assistant.Cites, "\"article_id\":8") {
		t.Errorf("row assistant = role=%q cites=%q", assistant.Role, assistant.Cites)
	}
	if assistant.Thinking != true || assistant.Provider != "openrouter" {
		t.Errorf("lựa chọn của lượt phải đi theo message: %+v", assistant)
	}
	// jsonb mảng id phải là "[3,9]" — để mở lại phiên đọc được đúng phạm vi.
	if assistant.EffectiveCategoryIDs != "[3,9]" {
		t.Errorf("effective scope = %q, want [3,9]", assistant.EffectiveCategoryIDs)
	}
}

// TestAppendTurnStopsWhenMessagesFail chứng chỉ cần 1 bước lỗi là AppendTurn trả
// error NGAY, không chạy tiếp touch/title — InTx thật sẽ rollback, còn stub thì
// chứng minh không có bước nào chạy sau lỗi.
func TestAppendTurnStopsWhenMessagesFail(t *testing.T) {
	trace := &sessionTrace{}
	msgRepo := &sessionStubMessages{trace: trace, appendErr: stderrors.New("db chết")}
	svc := NewChatSessionStore(&sessionStubConversations{trace: trace}, msgRepo, sessionStubUoW{}, sessionStubCurrentUser{id: 7})

	err := svc.AppendTurn(context.Background(), &port.ChatTurn{ConversationID: 5, Question: "hỏi", Answer: "đáp"})
	if err == nil {
		t.Fatal("Append lỗi mà AppendTurn vẫn nil — sẽ commit nửa lượt")
	}
	if len(trace.steps) != 1 {
		t.Errorf("trace = %v, want chỉ bước Append lỗi", trace.steps)
	}
}

// TestLoadTakesHistoryFromDBnotClient là nghiệm thu sống còn của S1: lịch sử
// đến từ message đã lưu, theo thứ tự CŨ→MỚI, không phải do client kể lại.
func TestLoadTakesHistoryFromDB(t *testing.T) {
	trace := &sessionTrace{}
	msgRepo := &sessionStubMessages{trace: trace, recent: []*domain.ChatMessage{
		{Role: domain.MessageRoleUser, Content: "câu 1"},
		{Role: domain.MessageRoleAssistant, Content: "đáp 1"},
	}}
	convRepo := &sessionStubConversations{trace: trace, conv: &domain.Conversation{
		ScopeCategoryIDs: "[3,9]", Provider: "openrouter", Model: "m-a", Thinking: true,
	}}
	svc := NewChatSessionStore(convRepo, msgRepo, sessionStubUoW{}, sessionStubCurrentUser{id: 7})

	session, err := svc.Load(context.Background(), 5, 7)
	if err != nil {
		t.Fatalf("Load lỗi: %v", err)
	}
	if len(session.History) != 2 || session.History[0].Content != "câu 1" || session.History[1].Role != "assistant" {
		t.Errorf("history = %+v", session.History)
	}
	if msgRepo.recentLimit != chatHistoryLimit {
		t.Errorf("cửa sổ lịch sử = %d, want %d", msgRepo.recentLimit, chatHistoryLimit)
	}
	if session.ConversationID != 5 || len(session.CategoryIDs) != 2 || !session.Thinking {
		t.Errorf("session = %+v — scope/provider phải về từ phiên", session)
	}
}

// TestLoadForeignConversationIsNotFound: đọc phiên của người khác phải là
// domain.ErrNotFound (chat_service dịch ra 404), không phải session rỗng — im
// lặng trả rỗng là mất lịch sử mà client không biết lý do.
func TestLoadForeignConversationIsNotFound(t *testing.T) {
	trace := &sessionTrace{}
	convRepo := &sessionStubConversations{trace: trace, notFound: true}
	svc := NewChatSessionStore(convRepo, &sessionStubMessages{trace: trace}, sessionStubUoW{}, sessionStubCurrentUser{id: 7})

	_, err := svc.Load(context.Background(), 5, 7)
	if !stderrors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
}

// TestRenameRejectsBlankTitleButKeepsRepoQuiet: title chỉ khoảng trắng là 400 và
// KHÔNG được chạm repo — để có phiên tên rỗng do tay người dùng.
func TestRenameRejectsBlankTitle(t *testing.T) {
	trace := &sessionTrace{}
	convRepo := &sessionStubConversations{trace: trace, conv: &domain.Conversation{Title: "có tên rồi"}}
	svc := NewConversationService(convRepo, &sessionStubMessages{trace: trace}, sessionStubUoW{}, sessionStubCurrentUser{id: 7})

	blank := "   "
	res := svc.Rename(context.Background(), 5, &dtos.RenameConversationRequest{Title: &blank})
	if res.Err == nil {
		t.Fatal("title rỗng phải bị chặn")
	}
	if e := errors.From(res.Err); e == nil || e.GetHttpCode() != http.StatusBadRequest {
		t.Errorf("err = %v, want 400", res.Err)
	}
	for _, step := range convRepo.trace.steps {
		if strings.Contains(step, "UpdateTitle") {
			t.Errorf("title rỗng mà vẫn ghi (%v)", convRepo.trace.steps)
		}
	}
}

// TestGetNotFoundMapsTo404 khoá khuôn "không có/không phải của tôi" → 404 như
// nhau, không 403 (403 lộ thông tin phiên đó có tồn tại).
func TestGetNotFoundMapsTo404(t *testing.T) {
	trace := &sessionTrace{}
	convRepo := &sessionStubConversations{trace: trace, notFound: true}
	svc := NewConversationService(convRepo, &sessionStubMessages{trace: trace}, sessionStubUoW{}, sessionStubCurrentUser{id: 7})

	res := svc.Get(context.Background(), 5)
	if res.Err == nil {
		t.Fatal("muốn 404 mà trả được")
	}
	e := errors.From(res.Err)
	if e == nil || e.GetHttpCode() != http.StatusNotFound {
		t.Errorf("err = %v, want 404", res.Err)
	}
}

// TestMessagesReturnsOldestFirst: repo cắt cửa sổ newest-first (để dùng index +
// LIMIT), client cần oldest-first để append thẳng vào DOM.
func TestMessagesReturnsOldestFirst(t *testing.T) {
	trace := &sessionTrace{}
	msgRepo := &sessionStubMessages{trace: trace, page: []*domain.ChatMessage{
		{Content: "đáp 2"}, {Content: "hỏi 2"}, {Content: "đáp 1"}, {Content: "hỏi 1"},
	}}
	svc := NewConversationService(&sessionStubConversations{trace: trace, conv: &domain.Conversation{}},
		msgRepo, sessionStubUoW{}, sessionStubCurrentUser{id: 7})

	res := svc.Messages(context.Background(), 5, &dtos.ListMessagesRequest{Limit: 4})
	if res.Err != nil {
		t.Fatalf("Messages lỗi: %v", res.Err)
	}
	got := make([]string, 0, len(res.Data))
	for _, msg := range res.Data {
		got = append(got, msg.Content)
	}
	want := []string{"hỏi 1", "đáp 1", "hỏi 2", "đáp 2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("thứ tự = %v, want %v", got, want)
	}
	if msgRepo.pageLimit != 4 {
		t.Errorf("limit truyền xuống = %d, want 4", msgRepo.pageLimit)
	}
}

// TestMessagesDefaultLimit khoá mặc định phòng thân: client không gửi limit thì
// không được dịch thành "không giới hạn" (kéo cả hội thoại lên bộ nhớ).
func TestMessagesDefaultLimit(t *testing.T) {
	trace := &sessionTrace{}
	msgRepo := &sessionStubMessages{trace: trace}
	svc := NewConversationService(&sessionStubConversations{trace: trace, conv: &domain.Conversation{}},
		msgRepo, sessionStubUoW{}, sessionStubCurrentUser{id: 7})

	if res := svc.Messages(context.Background(), 5, &dtos.ListMessagesRequest{}); res.Err != nil {
		t.Fatalf("Messages lỗi: %v", res.Err)
	}
	if msgRepo.pageLimit != defaultMessageListLimit {
		t.Errorf("limit mặc định = %d, want %d", msgRepo.pageLimit, defaultMessageListLimit)
	}
}

// ---- stub ----

// sessionTrace là dãy bước dùng chung cho cả 2 repo — tách 2 dãy thì test
// không chứng minh được THỨ TỰ ghi của một lượt. Bước được ghi kèm tham số
// (số row, id phiên) để test khoá luôn "2 message trong MỘT lần Append".
type sessionTrace struct{ steps []string }

func (t *sessionTrace) add(step string, args ...any) {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, fmt.Sprint(a))
	}
	if len(parts) > 0 {
		step += "(" + strings.Join(parts, ",") + ")"
	}
	t.steps = append(t.steps, step)
}

// sessionStubConversations hiện thân domain.IConversationRepository.
type sessionStubConversations struct {
	trace     *sessionTrace
	conv      *domain.Conversation
	notFound  bool
	lastLimit int
}

func (c *sessionStubConversations) Create(_ context.Context, conv *domain.Conversation) error {
	c.trace.add("conversations.Create")
	c.conv = conv
	return nil
}

func (c *sessionStubConversations) GetByID(context.Context, int64, int64) (*domain.Conversation, error) {
	c.trace.add("conversations.GetByID")
	if c.notFound {
		return nil, domain.ErrNotFound
	}
	conv := c.conv
	if conv == nil {
		conv = &domain.Conversation{}
	}
	conv.ID = 5
	return conv, nil
}

func (c *sessionStubConversations) List(_ context.Context, _ int64, _ int64, limit int) ([]*domain.Conversation, error) {
	c.trace.add("conversations.List")
	c.lastLimit = limit
	return []*domain.Conversation{{Title: "một phiên"}}, nil
}

func (c *sessionStubConversations) UpdateTitle(_ context.Context, _ int64, _ int64, title string) error {
	c.trace.add("conversations.UpdateTitle")
	c.conv.Title = title
	return nil
}

func (c *sessionStubConversations) SetTitleIfEmpty(_ context.Context, id int64, _ string) error {
	c.trace.add("conversations.SetTitleIfEmpty", id)
	return nil
}

func (c *sessionStubConversations) TouchMessageTime(_ context.Context, id int64, _ time.Time) error {
	c.trace.add("conversations.TouchMessageTime", id)
	return nil
}

func (c *sessionStubConversations) TouchScope(_ context.Context, id int64, _ string) error {
	c.trace.add("conversations.TouchScope", id)
	return nil
}

func (c *sessionStubConversations) Delete(context.Context, int64, int64) (int64, error) {
	c.trace.add("conversations.Delete")
	return 1, nil
}

// sessionStubMessages hiện thân domain.IChatMessageRepository.
type sessionStubMessages struct {
	trace       *sessionTrace
	appendErr   error
	recent      []*domain.ChatMessage
	page        []*domain.ChatMessage
	count       map[int64]int64
	appended    []*domain.ChatMessage
	recentLimit int
	pageLimit   int
}

func (m *sessionStubMessages) Append(_ context.Context, msgs []*domain.ChatMessage) error {
	m.trace.add("messages.Append", len(msgs))
	m.appended = append(m.appended, msgs...)
	if m.appendErr != nil {
		// Bước vẫn nằm trong trace để test thấy "đã cố ghi rồi dừng", không phải
		// "chưa ghi gì".
		return m.appendErr
	}
	return nil
}

func (m *sessionStubMessages) ListRecent(_ context.Context, _ int64, limit int) ([]*domain.ChatMessage, error) {
	m.trace.add("messages.ListRecent")
	m.recentLimit = limit
	return m.recent, nil
}

func (m *sessionStubMessages) ListPage(_ context.Context, _ int64, _ int64, limit int) ([]*domain.ChatMessage, error) {
	m.trace.add("messages.ListPage")
	m.pageLimit = limit
	return m.page, nil
}

func (m *sessionStubMessages) CountByConversationIDs(context.Context, []int64) (map[int64]int64, error) {
	m.trace.add("messages.CountByConversationIDs")
	if m.count != nil {
		return m.count, nil
	}
	return map[int64]int64{}, nil
}

// sessionStubUoW chạy fn thẳng — stub không có rollback thật, nên test chỉ chứng
// minh được "lỗi thì dừng ngay, không chạy tiếp bước sau".
type sessionStubUoW struct{}

func (sessionStubUoW) InTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type sessionStubCurrentUser struct{ id int64 }

func (u sessionStubCurrentUser) UserID(context.Context) (int64, bool) { return u.id, true }

var (
	_ domain.IConversationRepository = (*sessionStubConversations)(nil)
	_ domain.IChatMessageRepository  = (*sessionStubMessages)(nil)
	_ domain.IUnitOfWork             = sessionStubUoW{}
	_ middlewares.ICurrentUser       = sessionStubCurrentUser{}
	_ port.IChatSessions             = NewChatSessionStore(nil, nil, nil, nil)
	_ IConversationService           = NewConversationService(nil, nil, nil, nil)
)
