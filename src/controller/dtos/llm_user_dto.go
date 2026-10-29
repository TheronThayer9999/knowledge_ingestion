package dtos

// SaveUserProviderRequest là form user nhập cho KẾT NỐI RIÊNG của mình
// (BYOK): chọn nhà cung cấp trong danh mục hệ thống rồi dán API key của họ.
// Key đi một chiều vào server (mã hoá rồi lưu DB) — không có endpoint nào đọc
// ngược key ra, kể cả với chính user đó.
//
// User KHÔNG tự gõ base URL: server tự tra định nghĩa của provider đã chọn
// (giao thức + base URL) trong danh mục. Nhờ vậy không có đường nào để user
// trỏ server sang host tuỳ ý, và đường dẫn luôn đúng sẵn — không phải đoán
// mò xem provider thật nằm ở /v1 hay /api/v1.
//
// Trường bỏ trống khi SỬA nghĩa là "giữ nguyên giá trị cũ" (nút "Tải model"
// đã lưu danh sách model không bị mất khi user chỉ đổi key). Khi TẠO MỚI thì
// nhà cung cấp + API key là bắt buộc — không có gì để "giữ".
//
// Vì vậy binding KHÔNG đánh dấu required: sửa một ô (chỉ đổi nhãn) là body
// thiếu provider, chặn ở tầng bind sẽ chặn cả luật "bỏ trống thì giữ" mà
// service cài sẵn. Ràng buộc "đủ thông tin" được kiểm tra trên DÒNG ĐÃ GỘP
// (service validateUserProvider) — tạo mới thiếu nhà cung cấp/key thì báo 400
// với câu đọc được, sửa thì giữ giá trị cũ.
type SaveUserProviderRequest struct {
	// Label là tên hiện thị trong menu chọn model ("Groq của tôi"). Bỏ trống
	// thì lấy nhãn của provider trong danh mục.
	Label string `json:"label" binding:"omitempty,max=64" example:"Groq của tôi"`
	// Provider là slug của nhà cung cấp trong danh mục hệ thống (xem
	// GET /llm/my/providers/catalog) — server tra định nghĩa của nó.
	Provider string `json:"provider" binding:"omitempty,max=64" example:"groq"`
	// APIKey là key của provider. Bắt buộc với giao thức openai-compatible;
	// để trống khi sửa thì giữ key đang có.
	APIKey string `json:"api_key" binding:"omitempty,max=512" example:"gsk_..."`
	// Model là model mặc định của kết nối.
	Model string `json:"model" binding:"omitempty,max=128" example:"llama-3.3-70b-versatile"`
	// Models là danh sách model đã tải từ provider (nút "Tải model").
	Models []string `json:"models" binding:"omitempty,max=300"`
}

// LLMProviderCatalogItem là một provider trong danh mục mà người dùng có thể
// gắn key của chính họ. KHÔNG có base_url: đó là cấu hình nội bộ của hệ
// thống, không cần lộ ra cho user — và user cũng không tự gõ được, nên không
// có đường nào để trỏ server sang host tuỳ ý.
type LLMProviderCatalogItem struct {
	// Slug là khoá của provider trong danh mục — user gửi lại khi lưu.
	Slug string `json:"slug" example:"groq"`
	// Label là tên hiển thị ("Groq").
	Label string `json:"label" example:"Groq"`
	// Type là giao thức client ("ollama" | "openai") — user biết trước có cần
	// nhập API key hay không (ollama local chạy không key).
	Type string `json:"type" example:"openai"`
}
