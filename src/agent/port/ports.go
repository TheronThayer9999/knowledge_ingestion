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

// ILLMSettings là phần GHI của cùng màn hình đó (Đợt 28 nấc 2 + 3): admin lưu/xoá
// key, tải danh sách model từ provider, chọn model mặc định, và từ nấc 3 là tạo/
// sửa/xoá luôn ĐỊNH NGHĨA provider (loại, base_url, nhãn) để không phải sửa file
// config. Key lưu DB đã mã hoá và KHÔNG BAO GIỜ trả lại client; config vẫn là mặc
// định khi DB trống. Chặn quyền ở route (RequireRole) — service không nhận role
// từ client.
//
// SaveProvider/DeleteProvider trả cả danh sách provider vì chúng thêm/bớt một
// dòng khỏi danh sách; ba thao tác còn lại chỉ đổi tiêu chí nên trả đúng dòng đó.
type ILLMSettings interface {
	SaveKey(ctx context.Context, slug, apiKey string) dtos.Result[*dtos.LLMProviderOption]
	ClearKey(ctx context.Context, slug string) dtos.Result[*dtos.LLMProviderOption]
	LoadModels(ctx context.Context, slug string) dtos.Result[*dtos.LLMModelsResponse]
	SelectModel(ctx context.Context, slug, model string) dtos.Result[*dtos.LLMProviderOption]
	SaveProvider(ctx context.Context, slug string, req *dtos.SaveLLMProviderRequest) dtos.Result[*dtos.LLMOptionsResponse]
	DeleteProvider(ctx context.Context, slug string) dtos.Result[*dtos.LLMOptionsResponse]
}

// IUserProviders là phần BYOK: mỗi tài khoản tự thêm kết nối LLM riêng (base
// URL + API key) và dùng cho lượt chat của chính họ. Endpoint gọi tới đây chỉ
// cần đăng nhập — KHÔNG cần role admin (khác hẳn /llm/providers/* của admin).
//
// Service tự lấy user_id từ ctx (không tin id client gửi lên) và mọi truy vấn
// DB scope theo user đó, nên không có đường nào đọc được kết nối của người
// khác. Key đi một chiều: nhận vào → mã hoá → lưu, không có endpoint đọc lại.
type IUserProviders interface {
	List(ctx context.Context) dtos.Result[[]*dtos.LLMProviderOption]
	// Catalog là danh mục provider hệ thống mà người gọi có thể gắn key của
	// chính họ — chỉ nhãn + giao thức, không có base URL nội bộ.
	Catalog(ctx context.Context) dtos.Result[[]*dtos.LLMProviderCatalogItem]
	// Save tạo mới (id = 0) hoặc sửa (id > 0) một kết nối của người gọi.
	Save(ctx context.Context, id int64, req *dtos.SaveUserProviderRequest) dtos.Result[*dtos.LLMProviderOption]
	// Delete xoá kết nối của người gọi, trả lại danh sách còn lại.
	Delete(ctx context.Context, id int64) dtos.Result[[]*dtos.LLMProviderOption]
	// LoadModels hỏi chính provider bằng key của người gọi rồi lưu danh sách.
	LoadModels(ctx context.Context, id int64) dtos.Result[*dtos.LLMModelsResponse]
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
