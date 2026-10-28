package domain

import "context"

// LLMProviderSetting là phần admin chỉnh được của MỘT provider LLM. Config
// vẫn là nơi KHAI BÁO provider (type/base_url/label/model mặc định); bảng này
// ghi đè những gì nhập từ UI và, từ Đợt 28 nấc 3, còn tự KHAI BÁO được một
// provider mới — thêm Groq/OpenRouter mà không sửa file config, không restart.
// Quy tắc chung một câu: config là mặc định, DB thắng khi có giá trị.
//
// Không bao giờ xoá mềm dòng ở đây: "xoá key" chỉ rỗng hoá cột cipher để
// provider quay về key trong config, "xoá provider" là xoá HARD (repository
// Unscoped) để uniqueIndex(slug) không bao giờ đụng dòng đã soft delete và
// admin tạo lại được đúng slug đó.
type LLMProviderSetting struct {
	BaseModel
	// Slug là key của llms.providers (đã lowercase) — khoá tự nhiên, 1 dòng
	// cho mỗi provider. Provider chỉ có ở UI dùng cùng khoá này.
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
	// Label là tên hiển thị admin đặt; rỗng = theo config, rồi theo slug.
	Label string `json:"label" gorm:"size:64"`
	// Type là protocol client đã chuẩn hoá ("ollama" | "openai"). Rỗng = theo
	// config — với provider UI tạo mới thì service bắt buộc có giá trị.
	Type string `json:"type" gorm:"size:32"`
	// BaseURL là địa chỉ provider. Rỗng = theo config.
	BaseURL string `json:"base_url" gorm:"size:512"`
	// Disabled là công tắc admin tắt provider: không dựng agent, không hiện ở
	// menu chat, nhưng giữ nguyên key/model đã lưu để bật lại là chạy.
	Disabled bool `json:"disabled" gorm:"not null;default:false"`
}

func (LLMProviderSetting) TableName() string {
	return "llm_provider_settings"
}

// ILLMProviderSettingRepository là contract cho bảng llm_provider_settings.
// Không có xoá mềm: "xoá key" = rỗng hoá cipher (provider quay về config),
// còn Delete là xoá HARD cho provider admin tạo từ UI — xem ghi chú ở model.
type ILLMProviderSettingRepository interface {
	List(ctx context.Context) ([]*LLMProviderSetting, error)
	// Get trả domain.ErrNotFound khi provider chưa có cài đặt nào.
	Get(ctx context.Context, slug string) (*LLMProviderSetting, error)
	// Save là upsert theo slug — gọi 1 lần cho mỗi thao tác của admin.
	Save(ctx context.Context, setting *LLMProviderSetting) error
	// Delete bỏ hẳn dòng cài đặt (kể cả của provider trong config: xoá cài đặt
	// là provider quay về đúng file config). Không trả lỗi khi slug lạ.
	Delete(ctx context.Context, slug string) error
}
