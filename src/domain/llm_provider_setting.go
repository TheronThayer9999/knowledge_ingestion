package domain

import "context"

// LLMProviderSetting là phần admin chỉnh được của MỘT provider LLM. Config
// vẫn là nơi KHAI BÁO provider (type/base_url/label/model mặc định); bảng này
// chỉ ghi đè những gì nhập từ UI và giữ kết quả lần "Tải model" gần nhất —
// config là mặc định, DB thắng khi có giá trị.
//
// Không bao giờ xoá cứng/xoá mềm dòng ở đây: "xoá key" chỉ rỗng hoá cột
// cipher để provider quay về key trong config. Nhờ vậy uniqueIndex(slug) không
// bao giờ đụng dòng đã soft delete, và danh sách model đã tải vẫn còn.
type LLMProviderSetting struct {
	BaseModel
	// Slug là key của llms.providers (đã lowercase) — khoá tự nhiên, 1 dòng
	// cho mỗi provider.
	Slug string `json:"slug" gorm:"size:64;uniqueIndex;not null"`
	// APIKeyCipher là AES-GCM của key do admin nhập, base64. KHÔNG BAO GIỜ
	// trả ra API hay ghi log. Rỗng = dùng key trong config.
	APIKeyCipher string `json:"-" gorm:"type:text"`
	// Model là model mặc định admin chọn trong popup; rỗng = theo config.
	Model string `json:"model" gorm:"size:128"`
	// Models là jsonb mảng model đọc từ provider (nút "Tải model") — cache
	// hiển thị, không phải danh sách chặn: backend vẫn nhận model ngoài danh
	// sách và provider tự quyết.
	Models string `json:"models" gorm:"type:jsonb;not null;default:'[]'"`
}

func (LLMProviderSetting) TableName() string {
	return "llm_provider_settings"
}

// ILLMProviderSettingRepository là contract cho bảng llm_provider_settings.
// Không có Delete: xem ghi chú ở model (xoá key = rỗng hoá cipher).
type ILLMProviderSettingRepository interface {
	List(ctx context.Context) ([]*LLMProviderSetting, error)
	// Get trả domain.ErrNotFound khi provider chưa có cài đặt nào.
	Get(ctx context.Context, slug string) (*LLMProviderSetting, error)
	// Save là upsert theo slug — gọi 1 lần cho mỗi thao tác của admin.
	Save(ctx context.Context, setting *LLMProviderSetting) error
}
