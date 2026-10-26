package dtos

import (
	"encoding/json"
	"strings"

	"knowledge_ingestion/src/domain"
	"time"
)

// CreateConversationRequest mở một phiên chat. Mọi field đều tuỳ chọn: không
// gửi gì cũng được — phiên rỗng tên, không phạm vi, provider/model mặc định
// theo cấu hình lúc chat.
type CreateConversationRequest struct {
	Title       string  `json:"title,omitempty" binding:"omitempty,max=120" example:"Hỏi về phụ cấp công tác"`
	CategoryIDs []int64 `json:"category_ids,omitempty" binding:"omitempty,max=100,dive,gt=0"`
	Provider    string  `json:"provider,omitempty" example:"openrouter"`
	Model       string  `json:"model,omitempty" example:"google/gemma-4-31b-it:free"`
	Thinking    bool    `json:"thinking,omitempty" example:"false"`
}

// RenameConversationRequest đổi tên phiên — chỉ có title, đủ cho S1. Dùng field
// pointer để phân biệt "không gửi" với "gửi chuỗi rỗng" (rule 11).
type RenameConversationRequest struct {
	Title *string `json:"title" binding:"required,min=1,max=120" example:"Phụ cấp công tác"`
}

// ListConversationsRequest hứng query string của list phiên (newest-first).
type ListConversationsRequest struct {
	Limit  int   `form:"limit" binding:"omitempty,gte=0,lte=100" example:"20"`
	Before int64 `form:"before" binding:"omitempty,gt=0" example:"12"`
}

// ListMessagesRequest hứng query string của lịch sử một phiên.
type ListMessagesRequest struct {
	Limit  int   `form:"limit" binding:"omitempty,gte=0,lte=200" example:"50"`
	Before int64 `form:"before" binding:"omitempty,gt=0" example:"108"`
}

// ConversationResponse là 1 phiên chat cho sidebar.
type ConversationResponse struct {
	ID          int64   `json:"id" example:"12"`
	Title       string  `json:"title" example:"Phụ cấp công tác"`
	Provider    string  `json:"provider,omitempty" example:"openrouter"`
	Model       string  `json:"model,omitempty" example:"google/gemma-4-31b-it:free"`
	Thinking    bool    `json:"thinking"`
	CategoryIDs []int64 `json:"category_ids,omitempty"`
	// MessageCount do COUNT(*) theo trang đang xem, không phải cột phản dư trong DB.
	MessageCount  int       `json:"message_count" example:"4"`
	LastMessageAt time.Time `json:"last_message_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// ChatMessageResponse là 1 message đã lưu. `cites` về đúng nguyên mảng lúc trả
// lời — mở lại phiên là chip `[^n]` (Đợt 23) sống lại được, không phải đoán lại.
type ChatMessageResponse struct {
	ID          int64       `json:"id" example:"108"`
	Role        string      `json:"role" example:"assistant"`
	Content     string      `json:"content"`
	Cites       []*ChatCite `json:"cites"`
	CategoryIDs []int64     `json:"effective_category_ids,omitempty"`
	Provider    string      `json:"provider,omitempty" example:"openrouter"`
	Model       string      `json:"model,omitempty" example:"google/gemma-4-31b-it:free"`
	Thinking    bool        `json:"thinking"`
	CreatedAt   time.Time   `json:"created_at"`
}

func ToConversationResponse(conv *domain.Conversation) (*ConversationResponse, error) {
	ids, err := DecodeIDList(conv.ScopeCategoryIDs)
	if err != nil {
		return nil, err
	}
	return &ConversationResponse{
		ID:            conv.ID,
		Title:         conv.Title,
		Provider:      conv.Provider,
		Model:         conv.Model,
		Thinking:      conv.Thinking,
		CategoryIDs:   ids,
		LastMessageAt: conv.LastMessageAt,
		CreatedAt:     conv.CreatedAt,
	}, nil
}

// ToMessageResponse trả kèm error vì decode jsonb là chỗ duy nhất trong mapping
// của project có thể gãy (dữ liệu do chính ta ghi, nhưng cột có thể bị sửa tay
// hoặc để lại giá trị hỏng từ trước) — để caller quyết định log chứ không im
// lặng mất nguồn trích dẫn.
func ToMessageResponse(msg *domain.ChatMessage) (*ChatMessageResponse, error) {
	cites, err := decodeCites(msg.Cites)
	if err != nil {
		return nil, err
	}
	ids, err := DecodeIDList(msg.EffectiveCategoryIDs)
	if err != nil {
		return nil, err
	}
	return &ChatMessageResponse{
		ID:          msg.ID,
		Role:        msg.Role,
		Content:     msg.Content,
		Cites:       cites,
		CategoryIDs: ids,
		Provider:    msg.Provider,
		Model:       msg.Model,
		Thinking:    msg.Thinking,
		CreatedAt:   msg.CreatedAt,
	}, nil
}

// ToConversationModel dựng phiên mới từ request. last_message_at lấy mốc now để
// thứ tự xếp sidebar luôn là tổng thứ tự (cột NOT NULL, không có NULL FIRST/LAST
// lằng nhằng khi chưa có lượt nào).
func ToConversationModel(userID int64, req *CreateConversationRequest) (*domain.Conversation, error) {
	scope, err := EncodeIDList(req.CategoryIDs)
	if err != nil {
		return nil, err
	}
	return &domain.Conversation{
		UserID:           userID,
		Title:            req.Title,
		Provider:         req.Provider,
		Model:            req.Model,
		Thinking:         req.Thinking,
		ScopeCategoryIDs: scope,
		LastMessageAt:    time.Now(),
	}, nil
}

// EncodeIDList mã hoá []int64 thành jsonb string (cùng khuôn cho scope của phiên
// và effective scope của message). nil -> "[]": cột NOT NULL có default nên
// không bao giờ được nhận rỗng.
func EncodeIDList(ids []int64) (string, error) {
	if len(ids) == 0 {
		return "[]", nil
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func DecodeIDList(raw string) ([]int64, error) {
	if raw == "" || raw == "[]" {
		return nil, nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// EncodeCites mã hoá mảng cites đã chuẩn hoá xuống jsonb. Luôn lưu bản SAU
// normalizeCitations để số [^n] trong content khớp với mảng này khi mở lại.
func EncodeCites(cites []*ChatCite) (string, error) {
	if len(cites) == 0 {
		return "[]", nil
	}
	raw, err := json.Marshal(cites)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeCites(raw string) ([]*ChatCite, error) {
	if raw == "" || raw == "[]" {
		return nil, nil
	}
	var cites []*ChatCite
	if err := json.Unmarshal([]byte(raw), &cites); err != nil {
		return nil, err
	}
	return cites, nil
}

// ClipTitle cắt câu hỏi đầu tiên thành title phiên theo RUNE (tiếng Việt không
// bị xé đôi ký tự) và TrimSpace để không có title mở đầu bằng dấu cách.
func ClipTitle(question string) string {
	text := []rune(strings.TrimSpace(question))
	if len(text) <= domain.TitleAutoClipRune {
		return string(text)
	}
	return string(text[:domain.TitleAutoClipRune]) + "…"
}
