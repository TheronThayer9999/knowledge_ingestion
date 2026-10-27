package domain

import (
	"context"
	"time"
)

// Role của message trong phiên chat — trùng tên với `oneof=user assistant` của
// ChatHistoryItem ở dưới, để lịch sử nạp từ DB thẳng vào agent được mà không
// cần dịch trung gian.
const (
	MessageRoleUser      = "user"
	MessageRoleAssistant = "assistant"
)

// Conversation là 1 phiên chat — đơn vị **sở hữu** lịch sử, phạm vi tra cứu và
// lựa chọn provider/model (§6.7-a RAG_RESEARCH: scope + summary phải sống cùng
// session, không thể là tham số mỗi request).
//
// Phiên chỉ soft delete (BaseModel.DeletedAt): messages ở lại để không có
// đường xoá nhầm mất nội dung, purge cứng để sau.
type Conversation struct {
	BaseModel
	// UserID là owner — mọi query scope theo owner, không tin id client gửi
	// lên; phiên của người khác trả ErrNotFound (404), không 403 để không lộ
	// thông tin "phiên này tồn tại".
	UserID int64 `json:"user_id"`
	// Title rỗng ở lượt tạo đầu là hợp lệ: AppendTurn điền bằng câu hỏi đầu
	// tiên (cắt TitleAutoClipRune ký tự) — kiểu sidebar Qwen, vào chat là có
	// tên, không bắt client gọi thêm PATCH.
	Title string `json:"title" gorm:"size:120"`
	// Provider/Model/Thinking là lựa chọn của phiên (S4). Chat request không
	// gửi lại thì dùng bản đã lưu ở đây.
	Provider string `json:"provider" gorm:"size:64"`
	Model    string `json:"model" gorm:"size:128"`
	Thinking bool   `json:"thinking"`
	// ScopeCategoryIDs là jsonb mảng `category_ids` của phiên — CHỈ là gợi ý
	// phạm vi, tool lúc thực thi vẫn tự intersect với quyền thật của user
	// theo ctx (§6.7 cảnh báo: lưu hint rồi tin nó là quyền là mở đường vượt
	// phạm vi).
	//
	// default:'[]' BẮT BUỘC cùng lý do với ArticlePage.Seq: AutoMigrate thêm
	// cột NOT NULL vào bảng đã có dữ liệu mà không có default thì fail.
	ScopeCategoryIDs string `json:"scope_category_ids" gorm:"type:jsonb;not null;default:'[]'"`
	// LastMessageAt là mốc xếp sidebar (mới nhất lên đầu) — NOT NULL ngay từ
	// lúc tạo (service ghi time.Now()) để thứ tự luôn là tổng thứ tự, cursor
	// phân trang theo (last_message_at, id) mới đúng.
	LastMessageAt time.Time `json:"last_message_at"`
}

func (Conversation) TableName() string {
	return "conversations"
}

// ChatMessage là 1 lượt trong phiên. `cites` PHẢI lưu (§6.7-a): không lưu thì
// mở lại hội thoại là chip `[^n]` chết, và snippet nguồn đã bay mất theo response.
type ChatMessage struct {
	BaseModel
	ConversationID int64  `json:"conversation_id"`
	Role           string `json:"role" gorm:"size:16"`
	// Content là markdown nguyên văn đã chuẩn hoá trích dẫn (Đợt 22) — lưu bản
	// SAU normalizeCitations để số [^n] reopened khớp đúng với cites đã lưu.
	Content string `json:"content"`
	// Cites là jsonb mảng ChatCite đã đánh số 1..N — không tách bảng riêng vì
	// chẳng có query nào trên từng cite, chỉ có "cả mảng của message".
	Cites string `json:"cites" gorm:"type:jsonb;not null;default:'[]'"`
	// Charts là jsonb mảng ChatChart đã dựng xong (Đợt 26) — lưu để mở lại phiên
	// biểu đồ vẫn vẽ y như lượt vừa hỏi, không phải gọi tool xin vẽ lại.
	// Default '[]' vì AutoMigrate thêm cột NOT NULL vào bảng đã có dữ liệu mà
	// không có default thì fail (cùng lý do với Cites).
	Charts string `json:"charts" gorm:"type:jsonb;not null;default:'[]'"`
	// EffectiveCategoryIDs ghi lại phạm vi HIỆU LỰC của lượt này (S4): đổi scope
	// giữa hội thoại là chuyện thật, để sau này UI chỉ ra được "lượt này tra
	// trong phạm vi nào" thay vì suy từ scope hiện tại của phiên.
	EffectiveCategoryIDs string `json:"effective_category_ids" gorm:"type:jsonb;not null;default:'[]'"`
	Provider             string `json:"provider" gorm:"size:64"`
	Model                string `json:"model" gorm:"size:128"`
	Thinking             bool   `json:"thinking"`
	// FK conversation_id chưa đặt ON DELETE CASCADE ở đợt này vì phiên chỉ soft
	// delete (không có đường xoá cứng nào chạy) — đừng hiểu nhầm là thiếu sót:
	// khi có purge cứng thì thêm FK trong migrate.go cùng lúc.
}

func (ChatMessage) TableName() string {
	return "messages"
}

// TitleAutoClipRune là trần độ dài title tự sinh từ câu hỏi đầu tiên — cắt theo
// rune (tiếng Việt), đủ dài để đọc ra chuyện gì trong sidebar.
const TitleAutoClipRune = 60

// IConversationRepository là contract cho bảng conversations.
type IConversationRepository interface {
	Create(ctx context.Context, conv *Conversation) error
	// GetByID luôn kèm userID — không có đường nào đọc phiên của người khác.
	GetByID(ctx context.Context, id int64, userID int64) (*Conversation, error)
	// List newest-first theo (last_message_at, id); beforeID = 0 là trang đầu,
	// khác 0 là keyset sau phiên con trỏ (hàm tự tra mốc của nó).
	List(ctx context.Context, userID int64, beforeID int64, limit int) ([]*Conversation, error)
	UpdateTitle(ctx context.Context, id int64, userID int64, title string) error
	// SetTitleIfEmpty chỉ ghi khi title đang rỗng — lượt thứ 2 không được phép
	// đổi tên phiên đã có tên.
	SetTitleIfEmpty(ctx context.Context, id int64, title string) error
	// TouchMessageTime dời mốc last_message_at để sidebar đẩy phiên lên đầu.
	TouchMessageTime(ctx context.Context, id int64, at time.Time) error
	// Delete soft delete theo đúng owner.
	Delete(ctx context.Context, id int64, userID int64) (int64, error)
	// TouchScope cập nhật phạm vi đã lưu của phiên khi client đổi scope trong
	// request chat (S4: phiên phải nhớ phạm vi nó đang tra).
	TouchScope(ctx context.Context, id int64, scopeCategoryIDs string) error
}

// IChatMessageRepository là contract cho bảng messages.
type IChatMessageRepository interface {
	// Append ghi nhiều message 1 lệnh; gọi trong InTx thì chung transaction
	// với các op khác (dbConn đọc tx từ ctx).
	Append(ctx context.Context, msgs []*ChatMessage) error
	// ListRecent lấy `limit` message MỚI NHẤT và trả về theo thứ tự CŨ → MỚI
	// để đưa thẳng vào history của agent (role user/assistant xen kẽ đúng nhịp).
	ListRecent(ctx context.Context, conversationID int64, limit int) ([]*ChatMessage, error)
	// ListPage phân trang ngược cho UI: `beforeID`=0 là trang mới nhất, trả
	// newest-first theo id.
	ListPage(ctx context.Context, conversationID int64, beforeID int64, limit int) ([]*ChatMessage, error)
	// CountByConversationIDs đếm message cho đúng một trang phiên đang xem —
	// 1 câu GROUP BY thay vì cột đếm phản dư dễ lệch theo thời gian.
	CountByConversationIDs(ctx context.Context, ids []int64) (map[int64]int64, error)
}
