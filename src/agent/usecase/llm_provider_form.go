package usecase

import (
	"fmt"
	"strings"

	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
)

// Luật CHUNG cho cả hai đường provider: provider chung (admin sửa ở popup "Nhà
// cung cấp AI") và kết nối RIÊNG của user (BYOK). Trước đây mỗi đường có một
// bản riêng của apply-form và validate — dễ lệch nhau sau mỗi lần sửa, nên
// gom về đây: một hàm gộp, một hàm kiểm tra.
//
// providerFields là "ô đích" của một dòng provider: các field mà form sẽ ghi
// đè. Dùng con trỏ để một hàm ghi được cho cả cài đặt chung (bảng
// llm_provider_settings) lẫn kết nối riêng (bảng llm_user_providers) — hai
// bảng có tên cột giống nhau nhưng kiểu domain khác nhau.
type providerFields struct {
	Label   *string
	Type    *string
	BaseURL *string
	Model   *string
	Models  *string
}

// applyProviderFields ghi form lên dòng provider — CHỈ field nào có giá trị mới
// đổi, nên sửa một ô (đổi tên) không vô tình xoá base_url hay danh sách model
// đã lưu. Pure function (không DB, không registry, không mã hoá key) để test
// trực tiếp; key do caller Seal vì secret không thuộc về hàm này.
func applyProviderFields(form dtos.ProviderForm, dst providerFields) error {
	if v := strings.TrimSpace(form.Label); v != "" {
		*dst.Label = v
	}
	if raw := strings.TrimSpace(form.Type); raw != "" {
		kind := NormalizeProviderType(raw)
		if kind == "" {
			return badLLMRequest(fmt.Sprintf(
				"giao thức %q không hỗ trợ — chỉ có Ollama hoặc OpenAI-compatible", raw))
		}
		*dst.Type = kind
	}
	if v := strings.TrimSpace(form.BaseURL); v != "" {
		*dst.BaseURL = v
	}
	if v := strings.TrimSpace(form.Model); v != "" {
		*dst.Model = v
	}
	if form.Models != nil {
		*dst.Models = encodeModelList(cleanModels(form.Models))
	}
	return nil
}

// validateProviderConnection chặn từ sớm những kết nối không đời nào dựng được
// client, kèm lỗi đọc được. subject là danh từ gọi đối tượng trong câu lỗi
// ("provider" cho provider chung, "kết nối" cho kết nối riêng của user) — luật
// kiểm tra thì MỘT cho cả hai đường.
//
// hasKey là "có key hay không", KHÔNG phải key: đường admin kiểm tra key đã
// giải mã, đường user chỉ biết cột cipher có rỗng hay không (key đi một chiều,
// không đọc ngược ra được).
func validateProviderConnection(subject, name, kind, baseURL string, hasKey bool) error {
	normalized := NormalizeProviderType(kind)
	if normalized == "" {
		return badLLMRequest(fmt.Sprintf(
			"%s %s chưa có loại — chọn Ollama hoặc OpenAI-compatible", subject, name))
	}
	url := strings.TrimSpace(baseURL)
	if url == "" {
		return badLLMRequest(fmt.Sprintf("%s %s chưa có base_url", subject, name))
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return badLLMRequest(fmt.Sprintf(
			"base_url của %s %s phải bắt đầu bằng http:// hoặc https://", subject, name))
	}
	// Ollama local chạy không key; OpenAI-compatible thì mọi request đều cần
	// Authorization nên thà báo ngay ở form còn hơn để chat nhận 401.
	if normalized == config.LLMTypeOpenAI && !hasKey {
		return badLLMRequest(fmt.Sprintf("%s %s cần API key — nhập key rồi lưu lại", subject, name))
	}
	return nil
}

// keySourceOf là luật chung cho "key đang dùng đến từ đâu": ưu tiên DB rồi
// config, rỗng = chưa có key. Cả provider chung (key trong config lẫn trong DB)
// lẫn kết nối riêng của user (chỉ có key trong DB) đều dùng hàm này.
func keySourceOf(hasDBKey, hasConfigKey bool) string {
	if hasDBKey {
		return keySourceDB
	}
	if hasConfigKey {
		return keySourceConfig
	}
	return ""
}
