package usecase

import (
	"context"
	"fmt"
	"strings"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// Đợt 28 nấc 3: provider không còn là độc quyền của configs/config.json. Admin
// thêm/sửa/xoá provider ngay trong popup "Nhà cung cấp AI" — định nghĩa (loại,
// base_url, nhãn) cùng key và model nằm ở bảng llm_provider_settings, loader hợp
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
	name, err := providerSlug(slug)
	if err != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](err)
	}
	base, _ := providerBase(s.llms, name)
	setting, err := s.setting(ctx, name)
	if err != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](err)
	}
	if setting == nil {
		setting = &domain.LLMProviderSetting{Slug: name, Models: "[]"}
	}
	if formErr := applyProviderForm(setting, req, s.secret); formErr != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](formErr)
	}
	merged := MergeProviderSetting(base, setting, s.secret)
	if connErr := validateConnection(name, merged); connErr != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](connErr)
	}
	if setting.Disabled && name == effectiveProviderSlug(s.llms.DefaultProvider, s.agents) {
		return dtos.Fail[*dtos.LLMOptionsResponse](badLLMRequest(
			fmt.Sprintf("không tắt được %s vì đang là provider mặc định — chọn provider mặc định khác trước", name)))
	}
	if err := s.repo.Save(ctx, setting); err != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](err)
	}
	s.applyAfterSave(name, base, setting)
	return dtos.Ok(s.listForAdmin(ctx))
}

// applyAfterSave cập nhật registry sau khi dòng cài đặt đã nằm im trong DB.
// Lỗi dựng client KHÔNG biến thao tác của admin thành mất trắng: provider vẫn
// được lưu, chỉ bị tháo agent nên hiện "chưa sẵn sàng" — sửa key/base_url rồi
// lưu lại là chạy. (Đường lưu key/model đơn thuần vẫn trả 400 qua saveAndApply
// vì ở đó client cũ là thứ đang phục vụ chat, không được âm thầm gỡ.)
func (s *llmSettingsService) applyAfterSave(name string, base config.LLMProviderConfig,
	setting *domain.LLMProviderSetting) {
	if err := s.applyToRegistry(name, base, setting); err != nil {
		logs.Warnw("llm: đã lưu provider nhưng chưa dựng được client", "provider", name, "error", err)
		s.agents.Remove(name)
	}
}

// DeleteProvider bỏ dòng cài đặt của provider. Hai kết quả tuỳ nguồn: provider
// khai trong config thì quay về ĐÚNG file config (đây là cách "gỡ" một chỉnh sửa
// đè lên file), provider do UI tạo thì biến mất hẳn khỏi registry.
//
// Không xoá mềm (repository dùng Unscoped) để lần tạo lại cùng slug không đụng
// uniqueIndex. Trả danh sách mới như SaveProvider.
func (s *llmSettingsService) DeleteProvider(ctx context.Context, slug string) dtos.Result[*dtos.LLMOptionsResponse] {
	name, err := providerSlug(slug)
	if err != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](err)
	}
	base, fromConfig := providerBase(s.llms, name)
	setting, err := s.setting(ctx, name)
	if err != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](err)
	}
	if setting == nil && !fromConfig {
		return dtos.Fail[*dtos.LLMOptionsResponse](unknownProvider(slug))
	}
	// Chặn xoá provider MẶC ĐỊNH dù nó khai ở file hay do UI tạo: đây là cài đặt
	// cuối cùng còn hiệu lực cho mọi lượt chat không chọn provider rõ ràng, nên
	// gỡ là chat chết ngay (file có thể không còn key đúng). Admin muốn dọn thì
	// chọn provider mặc định khác trước — cùng một luật với nút Tắt ở trên.
	// Provider ở file vẫn xoá được từng phần: DELETE /key chỉ gỡ key đè.
	if name == effectiveProviderSlug(s.llms.DefaultProvider, s.agents) {
		return dtos.Fail[*dtos.LLMOptionsResponse](badLLMRequest(
			fmt.Sprintf("không xoá được %s vì đang là provider mặc định — chọn provider mặc định khác trước", name)))
	}
	if err := s.repo.Delete(ctx, name); err != nil {
		return dtos.Fail[*dtos.LLMOptionsResponse](err)
	}
	s.resetRegistry(name, base, fromConfig)
	return dtos.Ok(s.listForAdmin(ctx))
}

// resetRegistry đưa registry về đúng cấu hình còn lại sau khi xoá cài đặt:
// provider của file thì dựng lại theo file (DB không còn đè lên), provider UI
// tạo thì gỡ hẳn. Dựng không nổi (file thiếu key) thì tháo agent + WARN — thà
// để provider "chưa sẵn sàng" còn hơn giữ client của cái key vừa bị xoá.
func (s *llmSettingsService) resetRegistry(name string, base config.LLMProviderConfig, fromConfig bool) {
	if !fromConfig {
		s.agents.Remove(name)
		return
	}
	if err := s.agents.Rebuild(name, MergeProviderSetting(base, nil, s.secret)); err != nil {
		logs.Warnw("llm: provider trong config không dựng lại được sau khi xoá cài đặt",
			"provider", name, "error", err)
		s.agents.Remove(name)
	}
}

// applyProviderForm ghi đơn form admin lên dòng cài đặt — CHỈ field nào gửi lên
// mới đổi, nên popup "sửa mỗi cái tên" không vô tình xoá base_url đã lưu. Pure
// function (không DB, không registry) để test trực tiếp.
func applyProviderForm(setting *domain.LLMProviderSetting, req *dtos.SaveLLMProviderRequest, secret string) error {
	if label := strings.TrimSpace(req.Label); label != "" {
		setting.Label = label
	}
	if raw := strings.TrimSpace(req.Type); raw != "" {
		kind := NormalizeProviderType(raw)
		if kind == "" {
			return badLLMRequest(fmt.Sprintf(
				"loại provider %q không hỗ trợ — chỉ có ollama hoặc openai-compatible", raw))
		}
		setting.Type = kind
	}
	if url := strings.TrimSpace(req.BaseURL); url != "" {
		setting.BaseURL = url
	}
	if model := strings.TrimSpace(req.Model); model != "" {
		setting.Model = model
	}
	if req.Models != nil {
		setting.Models = encodeModelList(cleanModels(req.Models))
	}
	if req.Disabled != nil {
		setting.Disabled = *req.Disabled
	}
	if key := strings.TrimSpace(req.APIKey); key != "" {
		cipher, err := utils.SealString(key, secret)
		if err != nil {
			return err
		}
		setting.APIKeyCipher = cipher
	}
	return nil
}

// validateConnection chặn từ sớm những định nghĩa không đời nào dựng được client,
// kèm lỗi đọc được cho admin. Chạy trên cấu hình ĐÃ hợp nhất (config + form) nên
// provider trong config thiếu key vẫn sửa được từng phần, chỉ khi kết quả cuối
// vẫn không đủ thì mới chết.
func validateConnection(name string, merged config.LLMProviderConfig) error {
	kind := NormalizeProviderType(merged.Type)
	if kind == "" {
		return badLLMRequest(fmt.Sprintf(
			"provider %s chưa có loại — chọn Ollama hoặc OpenAI-compatible", name))
	}
	url := strings.TrimSpace(merged.BaseURL)
	if url == "" {
		return badLLMRequest(fmt.Sprintf("provider %s chưa có base_url", name))
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return badLLMRequest("base_url phải bắt đầu bằng http:// hoặc https://")
	}
	// Ollama local chạy không key; OpenAI-compatible thì mọi request đều cần
	// Authorization nên thà báo ngay ở form còn hơn để chat nhận 401.
	if kind == config.LLMTypeOpenAI && strings.TrimSpace(merged.APIKey) == "" {
		return badLLMRequest(fmt.Sprintf("provider %s cần API key — nhập key rồi lưu lại", name))
	}
	return nil
}
