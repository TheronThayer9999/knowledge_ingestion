package dtos

// ProviderForm là bộ field CHUNG của mọi form provider: provider chung (admin
// sửa trong popup "Nhà cung cấp AI") và kết nối RIÊNG của user (BYOK). Hai
// đường lưu ở hai bảng khác nhau nhưng gửi đúng một bộ field này, nên luật
// gộp ("bỏ trống = giữ giá trị cũ") và luật kiểm tra chỉ cần viết một lần.
//
// Không phải struct wire: nó là khuôn trung gian để service đổi DTO của
// mình sang bộ field chung rồi áp lên dòng tương ứng.
type ProviderForm struct {
	// Label là tên hiển thị.
	Label string
	// Type là giao thức client: "ollama" hoặc "openai" (nhận thêm
	// "openai-compatible" — server chuẩn hoá).
	Type string
	// BaseURL là địa chỉ provider — vd https://api.groq.com/openai/v1.
	BaseURL string
	// Model là model mặc định.
	Model string
	// Models là danh sách model đã tải. nil = không gửi (giữ danh sách cũ),
	// mảng rỗng = gửi rõ ý xoá hết.
	Models []string
}

// Form đổi đơn form admin sang bộ field chung.
func (r SaveLLMProviderRequest) Form() ProviderForm {
	return ProviderForm{
		Label:   r.Label,
		Type:    r.Type,
		BaseURL: r.BaseURL,
		Model:   r.Model,
		Models:  r.Models,
	}
}

// Form đổi đơn form kết nối riêng sang bộ field chung. User KHÔNG tự gõ base
// URL hay giao thức — chỉ chọn provider trong danh mục, server tra định nghĩa
// rồi điền vào đây (xem service UserProviderService).
func (r SaveUserProviderRequest) Form() ProviderForm {
	return ProviderForm{
		Label:  r.Label,
		Model:  r.Model,
		Models: r.Models,
	}
}
