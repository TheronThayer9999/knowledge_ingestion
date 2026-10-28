package port

import (
	"context"

	"knowledge_ingestion/src/controller/dtos"
)

// ISearcher là năng lực search mà agent cung cấp và chat tiêu thụ. API gọi
// thẳng interface này.
type ISearcher interface {
	Search(ctx context.Context, dto *dtos.SearchRequest) dtos.Result[*dtos.SearchResponse]
}

// IChatter là năng lực chat agent cung cấp. API gọi thẳng interface này.
type IChatter interface {
	Chat(ctx context.Context, dto *dtos.ChatRequest) dtos.Result[*dtos.ChatResponse]
}

// IUserResolver lấy user_id của người gọi từ ctx — agent chỉ cần method này,
// không cần biết middleware auth.
type IUserResolver interface {
	UserID(ctx context.Context) (int64, bool)
}

// ILLMOptions là bảng chọn provider/model mà UI cần (Đợt 28). Agent là nơi
// biết provider nào đã dựng được agent (available) nên năng lực này nằm ở
// agent; role do controller đọc từ token và truyền vào để quyết định field
// admin-only (base_url/has_key) — không nhận role từ client.
type ILLMOptions interface {
	Options(ctx context.Context, role int) dtos.Result[*dtos.LLMOptionsResponse]
}

// ChatSession là những gì một phiên chat cần biết trước khi chạy một lượt:
// lịch sử (đã cắt cửa sổ) + phạm vi/provider/model đã lưu của phiên.
type ChatSession struct {
	ConversationID int64
	History        []dtos.ChatHistoryItem
	CategoryIDs    []int64
	Provider       string
	Model          string
	Thinking       bool
}

// ChatTurn là một lượt thành công cần ghi xuống: câu hỏi + câu trả lời đã chuẩn
// hoá (sau normalizeCitations) + cites + charts + phạm vi hiệu lực của lượt này.
type ChatTurn struct {
	ConversationID int64
	UserID         int64
	Question       string
	Answer         string
	Cites          []*dtos.ChatCite
	// Charts là biểu đồ backend đã dựng (Đợt 26) — lưu cùng message để mở lại
	// phiên vẫn thấy biểu đồ, không phải chạy lại tool.
	Charts      []*dtos.ChatChart
	CategoryIDs []int64
	Provider    string
	Model       string
	Thinking    bool
}

// IChatSessions là cổng lưu hội thoại — agent KHAI BÁO cái nó cần (Load +
// AppendTurn), services là người implement, loader nối 2 đầu. Đúng khuôn
// IUserResolver: agent không import repository/gorm, không lật tầng.
type IChatSessions interface {
	// Load đọc phiên của chính userID (không phải chủ → domain.ErrNotFound).
	Load(ctx context.Context, conversationID, userID int64) (*ChatSession, error)
	// AppendTurn ghi 1 lượt (2 message + touch phiên) trong cùng transaction.
	AppendTurn(ctx context.Context, turn *ChatTurn) error
}
