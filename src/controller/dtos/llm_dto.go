package dtos

// LLMProviderOption là một backend LLM đã khai báo trong config, phản chiếu ra
// UI để người dùng chọn provider/model khi chat. KHÔNG có api_key trong struct
// này — key chỉ nằm ở config phía server, không bao giờ xuống trình duyệt.
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
