package dtos

// SaveUserProviderRequest là form user nhập cho KẾT NỐI RIÊNG của mình
// (BYOK): nhãn, giao thức, base URL và API key. Key đi một chiều vào server
// (mã hoá rồi lưu DB) — không có endpoint nào đọc ngược key ra, kể cả với
// chính user đó.
//
// Trường bỏ trống khi SỬA nghĩa là "giữ nguyên giá trị cũ" (nút "Tải model"
// đã lưu danh sách model không bị mất khi user chỉ đổi key). Khi TẠO MỚI thì
// nhãn + giao thức + base URL là bắt buộc — không có gì để "giữ".
//
// Vì vậy binding KHÔNG đánh dấu required: sửa một ô (chỉ đổi nhãn) là body
// thiếu giao thức + base URL, chặn ở tầng bind sẽ chặn cả luật "bỏ trống thì
// giữ" mà service cài sẵn. Ràng buộc "đủ thông tin" được kiểm tra trên DÒNG
// ĐÃ GỘP (service validateUserProvider) — tạo mới thiếu nhãn/base URL thì
// báo 400 với câu đọc được, sửa thì giữ giá trị cũ.
type SaveUserProviderRequest struct {
	// Label là tên hiện thị trong menu chọn model ("Groq của tôi").
	Label string `json:"label" binding:"omitempty,max=64" example:"Groq của tôi"`
	// Type là giao thức client: "openai-compatible" (Groq/OpenRouter/NVIDIA/
	// 9Router...) hoặc "ollama" (local). Server chuẩn hoá về "openai"/"ollama".
	Type string `json:"type" binding:"omitempty,max=32" example:"openai-compatible"`
	// BaseURL là địa chỉ provider — vd https://api.groq.com/openai/v1.
	BaseURL string `json:"base_url" binding:"omitempty,max=512" example:"https://api.groq.com/openai/v1"`
	// APIKey là key của provider. Bắt buộc với giao thức openai-compatible;
	// để trống khi sửa thì giữ key đang có.
	APIKey string `json:"api_key" binding:"omitempty,max=512" example:"gsk_..."`
	// Model là model mặc định của kết nối.
	Model string `json:"model" binding:"omitempty,max=128" example:"llama-3.3-70b-versatile"`
	// Models là danh sách model đã tải từ provider (nút "Tải model").
	Models []string `json:"models" binding:"omitempty,max=300"`
}
