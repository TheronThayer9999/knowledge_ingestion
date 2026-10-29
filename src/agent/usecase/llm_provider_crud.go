package usecase

import (
	"context"
	"fmt"
	"strings"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
)

// Đợt 28 nấc 3: provider không còn là độc quyền của configs/config.json. Admin
// thêm/sửa/xoá provider ngay trong popup "Nhà cung cấp AI" — định nghĩa (loại,
// base_url, nhãn) cùng key và model nằm ở bảng llm_provider_settings, catalog hợp
// nhất config ∪ DB lúc boot nên thêm provider là dùng được NGAY, không restart.
// File config vẫn là nguồn cấu hình hàng loạt (temperature, timeout, rpm) và là
// mặc định cho mọi provider khai ở đó.

// SaveProvider tạo mới hoặc sửa ĐỊNH NGHĨA một provider. Cùng slug với entry
// config thì ghi đè từng field có giá trị (bỏ trống = giữ như cũ/theo config);
// slug chưa có ở đâu là tạo provider hoàn toàn mới. Key, nếu gửi kèm, được mã
// hoá lưu luôn trong một lần bấm — khỏi phải mở hai bước.
//
// Trả về TOÀN BỘ danh sách provider (như /llm/options cho admin) vì thêm provider
// làm thay đổi danh sách, FE chỉ việc nạp lại.
func (s *llmSettingsService) SaveProvider(ctx context.Context, slug string,
	req *dtos.SaveLLMProviderRequest) dtos.Result[*dtos.LLMOptionsResponse] {
	// View (không phải Require): slug chưa có ở đâu là trường hợp HỢP LỆ ở đây —
	// admin đang tạo provider mới.
	view, err := s.catalog.View(ctx, slug)
	if err != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](err)
	}
	setting := view.NewSetting()
	if formErr := applyProviderFields(req.Form(), providerFields{
		Label: &setting.Label, Type: &setting.Type, BaseURL: &setting.BaseURL,
		Model: &setting.Model, Models: &setting.Models,
	}); formErr != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](formErr)
	}
	if req.Disabled != nil {
		setting.Disabled = *req.Disabled
	}
	if key := strings.TrimSpace(req.APIKey); key != "" {
		cipher, sealErr := s.catalog.Seal(key)
		if sealErr != nil {
			return dtos.Fail[*dtos.LLMOptionsResponse](sealErr)
		}
		setting.APIKeyCipher = cipher
	}
	if connErr := validateConnection(view.Slug, view.Effective()); connErr != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](connErr)
	}
	if setting.Disabled && view.Slug == s.defaultSlug() {
		return dtos.Fail[*dtos.LLMOptionsResponse](badLLMRequest(
			fmt.Sprintf("không tắt được %s vì đang là provider mặc định — chọn provider mặc định khác trước", view.Slug)))
	}
	if err := s.catalog.Save(ctx, setting); err != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](err)
	}
	s.applyAfterSave(view)
	return dtos.Ok(s.listForAdmin(ctx))
}

// applyAfterSave cập nhật registry sau khi dòng cài đặt đã nằm im trong DB.
// Lỗi dựng client KHÔNG biến thao tác của admin thành mất trắng: provider vẫn
// được lưu, chỉ bị tháo agent nên hiện "chưa sẵn sàng" — sửa key/base_url rồi
// lưu lại là chạy. (Đường lưu key/model đơn thuần vẫn trả 400 qua saveAndApply
// vì ở đó client cũ là thứ đang phục vụ chat, không được âm thầm gỡ.)
func (s *llmSettingsService) applyAfterSave(view *ProviderView) {
	if err := s.applyToRegistry(view); err != nil {
		logs.Warnw("llm: đã lưu provider nhưng chưa dựng được client", "provider", view.Slug, "error", err)
		s.agents.Remove(view.Slug)
	}
}

// DeleteProvider bỏ dòng cài đặt của provider. Hai kết quả tuỳ nguồn: provider
// khai trong config thì quay về ĐÚNG file config (đây là cách "gỡ" một chỉnh sửa
// đè lên file), provider do UI tạo thì biến mất hẳn khỏi registry.
//
// Trả danh sách mới như SaveProvider.
func (s *llmSettingsService) DeleteProvider(ctx context.Context, slug string) dtos.Result[*dtos.LLMOptionsResponse] {
	view, err := s.catalog.Require(ctx, slug)
	if err != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](err)
	}
	// Chặn xoá provider MẶC ĐỊNH dù nó khai ở file hay do UI tạo: đây là cài đặt
	// cuối cùng còn hiệu lực cho mọi lượt chat không chọn provider rõ ràng, nên
	// gỡ là chat chết ngay (file có thể không còn key đúng). Admin muốn dọn thì
	// chọn provider mặc định khác trước — cùng một luật với nút Tắt ở trên.
	// Provider ở file vẫn xoá được từng phần: DELETE /key chỉ gỡ key đè.
	if view.Slug == s.defaultSlug() {
		return dtos.Fail[*dtos.LLMOptionsResponse](badLLMRequest(
			fmt.Sprintf("không xoá được %s vì đang là provider mặc định — chọn provider mặc định khác trước", view.Slug)))
	}
	if err := s.catalog.Delete(ctx, view.Slug); err != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](err)
	}
	s.resetRegistry(view)
	return dtos.Ok(s.listForAdmin(ctx))
}

// resetRegistry đưa registry về đúng cấu hình còn lại sau khi xoá cài đặt:
// provider của file thì dựng lại theo file (DB không còn đè lên), provider UI
// tạo thì gỡ hẳn. Dựng không nổi (file thiếu key) thì tháo agent + WARN — thà
// để provider "chưa sẵn sàng" còn hơn giữ client của cái key vừa bị xoá.
func (s *llmSettingsService) resetRegistry(view *ProviderView) {
	if !view.FromConfig {
		s.agents.Remove(view.Slug)
		return
	}
	// Setting đã bị xoá khỏi DB nên merge với nil: chỉ còn đúng những gì file khai.
	if err := s.agents.Rebuild(view.Slug, s.catalog.Effective(view.Base, nil)); err != nil {
		logs.Warnw("llm: provider trong config không dựng lại được sau khi xoá cài đặt",
			"provider", view.Slug, "error", err)
		s.agents.Remove(view.Slug)
	}
}

// validateConnection chặn từ sớm những định nghĩa không đời nào dựng được client,
// kèm lỗi đọc được cho admin. Chạy trên cấu hình ĐÃ hợp nhất (config + form) nên
// provider trong config thiếu key vẫn sửa được từng phần, chỉ khi kết quả cuối
// vẫn không đủ thì mới chết.
func validateConnection(name string, merged config.LLMProviderConfig) error {
	return validateProviderConnection("provider", name, merged.Type, merged.BaseURL,
		strings.TrimSpace(merged.APIKey) != "")
}
