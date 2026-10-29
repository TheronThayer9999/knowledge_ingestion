package domain

import (
	"context"
	"strconv"
	"strings"
)

// UserProviderSlugPrefix là tiền tố DÀNH RIÊNG cho provider riêng của user
// (BYOK). Provider do admin tạo trong popup không được dùng tiền tố này —
// nếu không, slug "u_12" của user sẽ trùng slug provider chung và chat không
// biết đâu là của ai.
const UserProviderSlugPrefix = "u_"

// UserProviderMax là số kết nối tối đa mỗi user. Mỗi dòng là một secret
// được mã hoá + một agent riêng trong bộ nhớ đệm, nên chặn user dựng hàng
// trăm dòng rác.
const UserProviderMax = 5

// UserProviderSlug là slug trên wire của provider riêng: tiền tố + id dòng.
// Id là số tự tăng nên duy nhất toàn hệ thống; server luôn tra theo (id,
// user_id) nên user khác đoán id cũng không đọc được dòng đó.
func UserProviderSlug(id int64) string {
	return UserProviderSlugPrefix + strconv.FormatInt(id, 10)
}

// ParseUserProviderSlug đọc id từ slug provider riêng. Trả false nếu slug
// không thuộc dạng đó — caller dùng để biết phải đi đường provider chung.
func ParseUserProviderSlug(slug string) (int64, bool) {
	s := strings.ToLower(strings.TrimSpace(slug))
	if !strings.HasPrefix(s, UserProviderSlugPrefix) {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(s, UserProviderSlugPrefix), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// IsReservedUserProviderSlug nói slug có thuộc không gian dành riêng của
// provider user không — admin tạo provider trùng vùng này thì bị chặn.
func IsReservedUserProviderSlug(slug string) bool {
	return strings.HasPrefix(slugKeyLower(slug), UserProviderSlugPrefix)
}

// slugKeyLower chuẩn hoá slug kiểu registry (lowercase + trim).
func slugKeyLower(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

// LLMUserProvider là kết nối LLM RIÊNG của một tài khoản (BYOK): user tự
// nhập base_url + api key của họ, dùng cho các lượt chat của chính họ.
// Tách hẳn bảng llm_provider_settings (cấu hình chung của hệ thống): admin
// không đọc được key của user, user không sửa được provider chung.
type LLMUserProvider struct {
	BaseModel
	// UserID là chủ sở hữu — MỌI truy vấn đều scope theo cột này.
	UserID int64 `json:"user_id" gorm:"not null;index"`
	// Label là tên hiện thị user đặt ("Groq của tôi").
	Label string `json:"label" gorm:"size:64;not null"`
	// Type là protocol client đã chuẩn hoá ("ollama" | "openai").
	Type string `json:"type" gorm:"size:32;not null"`
	// BaseURL là địa chỉ provider — lấy từ định nghĩa trong danh mục hệ
	// thống, user KHÔNG tự gõ (nên không có đường trỏ server sang host tuỳ ý).
	BaseURL string `json:"base_url" gorm:"size:512;not null"`
	// ProviderSlug là slug của provider hệ thống mà kết nối này gắn vào —
	// để UI biết đang dùng nhà cung cấp nào và chọn lại được khi sửa.
	ProviderSlug string `json:"provider_slug" gorm:"size:64;index"`
	// APIKeyCipher là AES-GCM của key, base64. KHÔNG BAO GIỜ trả ra API hay
	// ghi log — kể cả với chính user đó.
	APIKeyCipher string `json:"-" gorm:"type:text"`
	// Model là model mặc định của kết nối này.
	Model string `json:"model" gorm:"size:128"`
	// Models là jsonb mảng model đọc từ provider (nút "Tải model").
	Models string `json:"models" gorm:"type:jsonb;not null;default:'[]'"`
}

func (LLMUserProvider) TableName() string {
	return "llm_user_providers"
}

// ILLMUserProviderRepository là contract cho bảng llm_user_providers. Mọi
// method nhận userID và luôn lọc theo nó — không có method nào đọc được
// provider của user khác.
type ILLMUserProviderRepository interface {
	// List trả các kết nối của user, cũ nhất trước.
	List(ctx context.Context, userID int64) ([]*LLMUserProvider, error)
	// Get trả domain.ErrNotFound khi không có dòng nào của user đó.
	Get(ctx context.Context, id, userID int64) (*LLMUserProvider, error)
	// Save là upsert theo id — tạo mới thì để trống id.
	Save(ctx context.Context, provider *LLMUserProvider) error
	// Delete xoá dòng của user đó; không có dòng thì không lỗi.
	Delete(ctx context.Context, id, userID int64) error
	// Count đếm số kết nối user đang có (chặn trần UserProviderMax).
	Count(ctx context.Context, userID int64) (int64, error)
}
