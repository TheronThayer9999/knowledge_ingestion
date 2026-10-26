package services

import (
	"context"
	stderrors "errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
)

// chatHistoryLimit là số message gần nhất nạp vào làm lịch sử cho một lượt chat
// — khớp HISTORY_LIMIT frontend đang cắt, để đổi hành vi không thành đổi hợp đồng
// API cũ (client vẫn gửi history cho đường không có conversation_id).
const chatHistoryLimit = 12

const (
	defaultConversationListLimit = 20
	defaultMessageListLimit      = 50
)

// IConversationService là nghiệp vụ phiên chat cho API CRUD/list messages.
//
// Mọi op đều scope theo user_id lấy từ ctx (token), không có tham số user_id nào
// từ client — phiên của người khác là 404, không phải 403, để không lộ việc phiên
// đó tồn tại.
type IConversationService interface {
	Create(ctx context.Context, req *dtos.CreateConversationRequest) dtos.Result[*dtos.ConversationResponse]
	List(ctx context.Context, req *dtos.ListConversationsRequest) dtos.Result[[]*dtos.ConversationResponse]
	Get(ctx context.Context, id int64) dtos.Result[*dtos.ConversationResponse]
	Rename(ctx context.Context, id int64, req *dtos.RenameConversationRequest) dtos.Result[*dtos.ConversationResponse]
	Delete(ctx context.Context, id int64) dtos.Result[*dtos.ConversationResponse]
	// Messages phân trang ngược, trả về oldest-first trong cửa sổ để frontend
	// append thẳng vào DOM theo đúng thứ tự đọc.
	Messages(ctx context.Context, id int64, req *dtos.ListMessagesRequest) dtos.Result[[]*dtos.ChatMessageResponse]
}

type chatSessionService struct {
	convRepo    domain.IConversationRepository
	msgRepo     domain.IChatMessageRepository
	uow         domain.IUnitOfWork
	currentUser middlewares.ICurrentUser
}

func NewConversationService(
	convRepo domain.IConversationRepository,
	msgRepo domain.IChatMessageRepository,
	uow domain.IUnitOfWork,
	currentUser middlewares.ICurrentUser,
) IConversationService {
	return &chatSessionService{convRepo: convRepo, msgRepo: msgRepo, uow: uow, currentUser: currentUser}
}

// NewChatSessionStore là vai hẹp của CÙNG nghiệp vụ trên, dành cho agent: chỉ
// Load + AppendTurn, agent không thấy (và không cần) CRUD. Tách constructor
// theo đúng khuôn NewArticlePageRepository/NewArticlePageIssues — constructor
// phía trên trả interface lớn nên không bind thẳng vào vai hẹp được.
func NewChatSessionStore(
	convRepo domain.IConversationRepository,
	msgRepo domain.IChatMessageRepository,
	uow domain.IUnitOfWork,
	currentUser middlewares.ICurrentUser,
) port.IChatSessions {
	return &chatSessionService{convRepo: convRepo, msgRepo: msgRepo, uow: uow, currentUser: currentUser}
}

func (s *chatSessionService) Create(ctx context.Context, req *dtos.CreateConversationRequest) dtos.Result[*dtos.ConversationResponse] {
	userID, _ := s.currentUser.UserID(ctx)
	conv, err := dtos.ToConversationModel(userID, req)
	if err != nil {
		return dtos.Fail[*dtos.ConversationResponse](err)
	}
	if err := s.convRepo.Create(ctx, conv); err != nil {
		return dtos.Fail[*dtos.ConversationResponse](err)
	}
	resp, err := dtos.ToConversationResponse(conv)
	if err != nil {
		return dtos.Fail[*dtos.ConversationResponse](err)
	}
	return dtos.Ok(resp)
}

func (s *chatSessionService) List(ctx context.Context, req *dtos.ListConversationsRequest) dtos.Result[[]*dtos.ConversationResponse] {
	userID, _ := s.currentUser.UserID(ctx)
	limit := req.Limit
	if limit <= 0 {
		limit = defaultConversationListLimit
	}
	convs, err := s.convRepo.List(ctx, userID, req.Before, limit)
	if err != nil {
		return dtos.Fail[[]*dtos.ConversationResponse](err)
	}
	counts, err := s.countFor(ctx, convs)
	if err != nil {
		return dtos.Fail[[]*dtos.ConversationResponse](err)
	}
	out := make([]*dtos.ConversationResponse, 0, len(convs))
	for _, conv := range convs {
		resp, err := dtos.ToConversationResponse(conv)
		if err != nil {
			return dtos.Fail[[]*dtos.ConversationResponse](err)
		}
		resp.MessageCount = int(counts[conv.ID])
		out = append(out, resp)
	}
	return dtos.Ok(out)
}

func (s *chatSessionService) Get(ctx context.Context, id int64) dtos.Result[*dtos.ConversationResponse] {
	conv, err := s.getOwned(ctx, id)
	if err != nil {
		return dtos.Fail[*dtos.ConversationResponse](conversationErr(err))
	}
	resp, err := dtos.ToConversationResponse(conv)
	if err != nil {
		return dtos.Fail[*dtos.ConversationResponse](err)
	}
	counts, err := s.msgRepo.CountByConversationIDs(ctx, []int64{conv.ID})
	if err != nil {
		return dtos.Fail[*dtos.ConversationResponse](err)
	}
	resp.MessageCount = int(counts[conv.ID])
	return dtos.Ok(resp)
}

func (s *chatSessionService) Rename(ctx context.Context, id int64, req *dtos.RenameConversationRequest) dtos.Result[*dtos.ConversationResponse] {
	title := strings.TrimSpace(*req.Title)
	if title == "" {
		return dtos.Fail[*dtos.ConversationResponse](errConversationTitleEmpty)
	}
	conv, err := s.getOwned(ctx, id)
	if err != nil {
		return dtos.Fail[*dtos.ConversationResponse](conversationErr(err))
	}
	if err := s.convRepo.UpdateTitle(ctx, conv.ID, conv.UserID, title); err != nil {
		return dtos.Fail[*dtos.ConversationResponse](err)
	}
	conv.Title = title
	resp, err := dtos.ToConversationResponse(conv)
	if err != nil {
		return dtos.Fail[*dtos.ConversationResponse](err)
	}
	counts, err := s.msgRepo.CountByConversationIDs(ctx, []int64{conv.ID})
	if err != nil {
		return dtos.Fail[*dtos.ConversationResponse](err)
	}
	resp.MessageCount = int(counts[conv.ID])
	return dtos.Ok(resp)
}

// Delete soft delete phiên: row messages Ở LẠI (đừng hiểu là leak — chưa có
// purge cứng nên không có đường nào xoá mất nội dung), sidebar mất phiên.
// Trả chính phiên vừa xoá để client xác nhận đã đúng object.
func (s *chatSessionService) Delete(ctx context.Context, id int64) dtos.Result[*dtos.ConversationResponse] {
	conv, err := s.getOwned(ctx, id)
	if err != nil {
		return dtos.Fail[*dtos.ConversationResponse](conversationErr(err))
	}
	rows, err := s.convRepo.Delete(ctx, conv.ID, conv.UserID)
	if err != nil {
		return dtos.Fail[*dtos.ConversationResponse](err)
	}
	if rows == 0 {
		// Đua giữa getOwned và Delete (phiên bị xoá ở request khác đúng khoảnh
		// khắc đó) — vẫn là 404, không phải 500.
		return dtos.Fail[*dtos.ConversationResponse](errConversationNotFound())
	}
	resp, err := dtos.ToConversationResponse(conv)
	if err != nil {
		return dtos.Fail[*dtos.ConversationResponse](err)
	}
	return dtos.Ok(resp)
}

func (s *chatSessionService) Messages(ctx context.Context, id int64, req *dtos.ListMessagesRequest) dtos.Result[[]*dtos.ChatMessageResponse] {
	conv, err := s.getOwned(ctx, id)
	if err != nil {
		return dtos.Fail[[]*dtos.ChatMessageResponse](conversationErr(err))
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultMessageListLimit
	}
	msgs, err := s.msgRepo.ListPage(ctx, conv.ID, req.Before, limit)
	if err != nil {
		return dtos.Fail[[]*dtos.ChatMessageResponse](err)
	}
	// Repo trả newest-first (để cắt cửa sổ bằng index), client cần oldest-first.
	slices.Reverse(msgs)
	out := make([]*dtos.ChatMessageResponse, 0, len(msgs))
	for _, msg := range msgs {
		resp, err := dtos.ToMessageResponse(msg)
		if err != nil {
			return dtos.Fail[[]*dtos.ChatMessageResponse](err)
		}
		out = append(out, resp)
	}
	return dtos.Ok(out)
}

// Load là port cho agent (S1): phiên của chính user + lịch sử đã cắt cửa sổ.
//
// Request chat dùng history ở đây THAY CHO history client gửi lên khi có
// conversation_id — nguồn duy nhất, nếu không là hai sự thật cùng lúc (§6.7).
func (s *chatSessionService) Load(ctx context.Context, conversationID, userID int64) (*port.ChatSession, error) {
	conv, err := s.convRepo.GetByID(ctx, conversationID, userID)
	if err != nil {
		return nil, err
	}
	msgs, err := s.msgRepo.ListRecent(ctx, conv.ID, chatHistoryLimit)
	if err != nil {
		return nil, err
	}
	history := make([]dtos.ChatHistoryItem, 0, len(msgs))
	for _, msg := range msgs {
		history = append(history, dtos.ChatHistoryItem{Role: msg.Role, Content: msg.Content})
	}
	ids, err := dtos.DecodeIDList(conv.ScopeCategoryIDs)
	if err != nil {
		return nil, err
	}
	return &port.ChatSession{
		ConversationID: conv.ID,
		History:        history,
		CategoryIDs:    ids,
		Provider:       conv.Provider,
		Model:          conv.Model,
		Thinking:       conv.Thinking,
	}, nil
}

// AppendTurn ghi một lượt thành công: 2 message + dời mốc phiên + điền title
// (khi đang rỗng) + ghi phạm vi hiệu lực, TẤT CẢ trong một transaction.
//
// Atomic là bắt buộc chứ không phải choice: thiếu 1 trong 2 message thì lịch sử
// lệch nhịp user/assistant (model hiểu sai câu nối tiếp), thiếu touch thì phiên
// nằm sai chỗ trên sidebar.
//
// Không kiểm user_id ở đây: chatService đã Load(ctx, id, userID) trước lượt
// chạy, tức quyền sở hữu đã chốt trong cùng request này.
func (s *chatSessionService) AppendTurn(ctx context.Context, turn *port.ChatTurn) error {
	cites, err := dtos.EncodeCites(turn.Cites)
	if err != nil {
		return err
	}
	scope, err := dtos.EncodeIDList(turn.CategoryIDs)
	if err != nil {
		return err
	}
	now := time.Now()
	msgs := []*domain.ChatMessage{
		newChatMessage(turn, domain.MessageRoleUser, turn.Question, "[]", scope),
		newChatMessage(turn, domain.MessageRoleAssistant, turn.Answer, cites, scope),
	}
	return s.uow.InTx(ctx, func(txCtx context.Context) error {
		if err := s.msgRepo.Append(txCtx, msgs); err != nil {
			return err
		}
		if err := s.convRepo.TouchMessageTime(txCtx, turn.ConversationID, now); err != nil {
			return err
		}
		if title := dtos.ClipTitle(turn.Question); title != "" {
			if err := s.convRepo.SetTitleIfEmpty(txCtx, turn.ConversationID, title); err != nil {
				return err
			}
		}
		return s.convRepo.TouchScope(txCtx, turn.ConversationID, scope)
	})
}

func newChatMessage(turn *port.ChatTurn, role, content, cites, scope string) *domain.ChatMessage {
	return &domain.ChatMessage{
		ConversationID:       turn.ConversationID,
		Role:                 role,
		Content:              content,
		Cites:                cites,
		EffectiveCategoryIDs: scope,
		Provider:             turn.Provider,
		Model:                turn.Model,
		Thinking:             turn.Thinking,
	}
}

// countFor đếm message cho đúng tập phiên đang hiển thị — 1 câu GROUP BY,
// không N+1.
func (s *chatSessionService) countFor(ctx context.Context, convs []*domain.Conversation) (map[int64]int64, error) {
	ids := make([]int64, 0, len(convs))
	for _, conv := range convs {
		ids = append(ids, conv.ID)
	}
	return s.msgRepo.CountByConversationIDs(ctx, ids)
}

// getOwned đọc phiên theo id + user_id từ ctx; lỗi trả raw domain.ErrNotFound
// để caller decide (conversationErr dịch ra 404).
func (s *chatSessionService) getOwned(ctx context.Context, id int64) (*domain.Conversation, error) {
	userID, _ := s.currentUser.UserID(ctx)
	return s.convRepo.GetByID(ctx, id, userID)
}

func conversationErr(err error) error {
	if stderrors.Is(err, domain.ErrNotFound) {
		return errConversationNotFound()
	}
	return err
}

// errConversationNotFound: 404 chứ không 403 — "không tìm thấy" và "không phải
// của anh" cố ý không phân biệt, để endpoint không tiết lộ id nào đang tồn tại.
func errConversationNotFound() error {
	return errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "phiên chat không tồn tại")
}

var errConversationTitleEmpty = errors.NewCustomHttpError(
	http.StatusBadRequest, errors.BadRequest, "title không được rỗng",
)
