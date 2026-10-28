package dtos

// LLMProviderOption là một backend LLM — khai trong configs/config.json hoặc do
// admin tạo/sửa từ UI (Đợt 28 nấc 3), phản chiếu ra menu chat và danh sách kết
// nối ở Settings. KHÔNG có api_key trong struct này — key chỉ nằm ở config hoặc
// cột cipher trong DB, không bao giờ xuống trình duyệt.
type LLMProviderOption struct {
	Slug string `json:"slug" example:"9router"`
	// Label là tên hiển thị; backend đã fallback về slug khi config để trống.
	Label string `json:"label" example:"9Router"`
	// Type là protocol client ("ollama" | "openai") — admin đối chiếu khi xem
	// danh sách kết nối.
	Type string `json:"type" example:"openai"`
	// DefaultModel là model dùng khi request không ép model.
	DefaultModel string `json:"default_model" example:"kc/nvidia/nemotron-3.5-super-vl:free"`
	// Models là danh sách model gợi ý cho menu chọn (luôn có mảng, có thể rỗng
	// khi provider chưa khai model nào).
	Models []string `json:"models"`
	// Available cho biết loader đã dựng được agent cho provider này (key/cấu
	// hình hợp lệ) — FE chỉ đưa provider available=true vào menu chat.
	Available bool `json:"available" example:"true"`
	// Thinking cho biết provider có client suy luận sâu riêng.
	Thinking bool `json:"thinking" example:"false"`
	// IsDefault là provider mặc định của cấu hình.
	IsDefault bool `json:"is_default" example:"true"`
	// BaseURL và HasKey chỉ trả cho admin (xem danh sách kết nối ở Settings);
	// user thường không thấy, càng không thấy key thật.
	BaseURL string `json:"base_url,omitempty" example:"http://localhost:20128/v1"`
	HasKey  *bool  `json:"has_key,omitempty" example:"true"`
	// KeySource (admin) cho biết key đang dùng đến từ đâu: "config" hay
	// "database" — chỉ là nguồn, KHÔNG phải key. Rỗng = chưa có key (provider
	// kiểu ollama local vẫn chạy được không key).
	KeySource string `json:"key_source,omitempty" example:"database"`
	// ConfigModel (admin) là model mặc định trong configs/config.json, để popup
	// phân biệt "mặc định của config" với model admin đã chọn trong DB.
	ConfigModel string `json:"config_model,omitempty" example:"llama3.1:8b"`
	// FromConfig (admin) cho biết provider có khai báo trong configs/config.json
	// hay do UI tạo — popup đổi nhãn nút xoá: provider của file thì "xoá cài
	// đặt là quay về file", provider UI tạo thì "xoá là mất hẳn".
	FromConfig bool `json:"from_config,omitempty" example:"true"`
	// Disabled (admin) = provider bị tắt trong UI: vẫn hiện trong popup để bật
	// lại, nhưng không hiện ở menu chat và không dựng agent.
	Disabled bool `json:"disabled,omitempty" example:"false"`
}

// LLMOptionsResponse là toàn bộ lựa chọn model cho UI chat + Settings: mặc
// định của cấu hình + danh sách provider sắp theo slug (kết quả tất định dù
// config là map).
type LLMOptionsResponse struct {
	DefaultProvider string `json:"default_provider" example:"9router"`
	DefaultModel    string `json:"default_model" example:"kc/nvidia/nemotron-3.5-super-vl:free"`
	// Providers luôn là mảng (kể cả rỗng) để FE khỏi phải xử lý null.
	Providers []*LLMProviderOption `json:"providers"`
}

// SaveLLMKeyRequest là key admin nhập trong popup provider. Key đi 1 chiều vào
// server (mã hoá rồi lưu DB) — không có endpoint nào đọc ngược key ra, kể cả
// cho admin. Slug lấy từ path nên không có trong body.
type SaveLLMKeyRequest struct {
	APIKey string `json:"api_key" binding:"required,min=1,max=512" example:"sk-..."`
}

// SelectLLMModelRequest chọn model mặc định cho provider. Không chặn model
// ngoài danh sách đã tải: provider mới là nơi từ chối model sai, và admin có
// thể gõ model chưa kịp "Tải model".
type SelectLLMModelRequest struct {
	Model string `json:"model" binding:"required,min=1,max=128" example:"llama3.1:8b"`
}

// LLMModelsResponse là kết quả "Tải model" từ chính provider — danh sách đã
// lưu vào DB nên lần mở popup sau vẫn thấy, không cần gọi lại.
type LLMModelsResponse struct {
	Slug   string   `json:"slug" example:"9router"`
	Models []string `json:"models"`
}

// SaveLLMProviderRequest là ĐỊNH NGHĨA provider admin nhập trong popup (Đợt 28
// nấc 3): tạo mới hoàn toàn hoặc sửa một provider đang có. Mọi field để trống
// nghĩa là "giữ như cũ / theo config" — riêng provider chưa có ở đâu thì service
// bắt buộc type + base_url (không có hai cái đó thì không dựng được client).
//
// Slug nằm ở path, không có trong body. APIKey là field duy nhất đi một chiều
// vào server: non-empty thì mã hoá lưu DB, empty thì giữ key đang có (muốn xoá
// key dùng endpoint DELETE .../key). Validation base_url làm trong service chứ
// không binding:"url" vì `http://localhost:11434` là địa chỉ hợp lệ ở đây.
type SaveLLMProviderRequest struct {
	Label    string   `json:"label" binding:"omitempty,max=64" example:"Groq"`
	Type     string   `json:"type" binding:"omitempty,max=32" example:"openai-compatible"`
	BaseURL  string   `json:"base_url" binding:"omitempty,max=512" example:"https://api.groq.com/openai/v1"`
	Model    string   `json:"model" binding:"omitempty,max=128" example:"llama-3.3-70b-versatile"`
	Models   []string `json:"models" binding:"omitempty,max=300" example:"[\"llama-3.3-70b-versatile\"]"`
	Disabled *bool    `json:"disabled" example:"false"`
	APIKey   string   `json:"api_key" binding:"omitempty,max=512"`
}
