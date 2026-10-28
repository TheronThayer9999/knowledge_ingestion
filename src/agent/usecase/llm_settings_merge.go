package usecase

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
)

// Nguồn của api key đang có hiệu lực — giá trị trả cho UI admin, KHÔNG phải key.
const (
	keySourceConfig = "config"
	keySourceDB     = "database"
)

// defaultOllamaBaseURL là fallback của provider ollama khi cả config lẫn UI đều
// bỏ trống base_url — cùng giá trị config.Load dùng cho entry ollama, để provider
// tạo từ UI không phải hỏi người dùng một địa chỉ ai cũng có.
const defaultOllamaBaseURL = "http://localhost:11434"

// providerModelsCap trần số model lưu vào cột jsonb cho một provider — Ollama
// local có thể trả hàng trăm model, nhiều quá thì menu chat thành vô dụng và
// một lần "Tải model" ghi cả MB vào DB.
const providerModelsCap = 300

// providerSlugRx là khuôn slug được phép làm khoá llm_provider_settings và đi
// vào URL path.
var providerSlugRx = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// slugKey chuẩn hoá slug giống registry agent (lowercase + trim) — config là
// map nên khoá có thể lẫn hoa/thường, còn request client gửi lên cũng vậy.
func slugKey(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

// lookupProvider tìm entry config theo slug không phân biệt hoa/thường và trả
// kèm bản slug đã chuẩn hoá (khoá của registry/DB).
func lookupProvider(llms config.LLMsConfig, slug string) (string, config.LLMProviderConfig, bool) {
	wanted := slugKey(slug)
	if wanted == "" {
		return "", config.LLMProviderConfig{}, false
	}
	for raw, pcfg := range llms.Providers {
		if slugKey(raw) == wanted {
			return wanted, pcfg, true
		}
	}
	return "", config.LLMProviderConfig{}, false
}

// NormalizeProviderType chấp nhận cả cách gọi quen thuộc ngoài UI. Backend chỉ
// có 2 protocol ("ollama" | "openai"), nhưng admin đọc tài liệu provider dưới
// tên "openai-compatible" (Groq/OpenRouter/NVIDIA đều cùng khuôn này) — nhận cả
// hai, lưu về giá trị nội bộ để registry và config dùng chung một khuôn.
// Trả "" khi type lạ để caller báo 400 đọc được, không âm thầm dựng sai client.
func NormalizeProviderType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case config.LLMTypeOllama:
		return config.LLMTypeOllama
	case config.LLMTypeOpenAI, "openai-compatible", "openai_compatible", "openai compatible":
		return config.LLMTypeOpenAI
	default:
		return ""
	}
}

// withSettingDef áp ĐỊNH NGHĨA provider (label/type/base_url) admin nhập từ UI
// lên entry config. Rỗng = theo config; slug chỉ có ở DB thì base là struct
// rỗng và DB là nguồn duy nhất.
func withSettingDef(base config.LLMProviderConfig, setting *domain.LLMProviderSetting) config.LLMProviderConfig {
	if setting == nil {
		return base
	}
	if label := strings.TrimSpace(setting.Label); label != "" {
		base.Label = label
	}
	if kind := NormalizeProviderType(setting.Type); kind != "" {
		base.Type = kind
	}
	if url := strings.TrimSpace(setting.BaseURL); url != "" {
		base.BaseURL = url
	}
	return base
}

// withSettingModel áp model mặc định + danh sách model admin đã chọn/tải lên
// entry config. Rỗng thì giữ config — config là mặc định.
func withSettingModel(base config.LLMProviderConfig, setting *domain.LLMProviderSetting) config.LLMProviderConfig {
	if setting == nil {
		return base
	}
	if model := strings.TrimSpace(setting.Model); model != "" {
		base.Model = model
	}
	if models := decodeModelList(setting.Models); len(models) > 0 {
		base.Models = models
	}
	return base
}

// withSettingKey áp key đã lưu trong DB lên entry config. Key DB hỏng/không
// giải mã được (đổi JWT_SECRET) thì WARN rồi giữ key config — không làm chết
// provider đang chạy vì một dòng cài đặt cũ.
func withSettingKey(base config.LLMProviderConfig, setting *domain.LLMProviderSetting, secret string) config.LLMProviderConfig {
	if setting == nil || strings.TrimSpace(setting.APIKeyCipher) == "" {
		return base
	}
	key, err := utils.OpenString(setting.APIKeyCipher, secret)
	if err != nil {
		logs.Warnw("llm: không giải mã được api key đã lưu, dùng key trong config",
			"provider", setting.Slug, "error", err)
		return base
	}
	if strings.TrimSpace(key) != "" {
		base.APIKey = key
	}
	return base
}

// MergeProviderSetting là cấu hình THẬT dùng để dựng client: định nghĩa
// (label/type/base_url) + model + key của DB (nếu có) đè lên config. Loader lúc
// boot, lần rebuild sau khi lưu key và lần admin sửa provider từ UI đều đi qua
// đây để ba đường không lệch nhau.
func MergeProviderSetting(base config.LLMProviderConfig, setting *domain.LLMProviderSetting, secret string) config.LLMProviderConfig {
	merged := withSettingKey(withSettingModel(withSettingDef(base, setting), setting), setting, secret)
	// Ollama không có default toàn cục trong DB (provider UI tạo mới chỉ có 2
	// ô type + base_url); base_url vẫn rỗng thì dựng client sẽ lỗi khó đọc,
	// nên áp đúng luật fallback mà config.Load dùng cho entry ollama.
	if merged.Type == config.LLMTypeOllama && strings.TrimSpace(merged.BaseURL) == "" {
		merged.BaseURL = defaultOllamaBaseURL
	}
	return merged
}

// ResolvedProvider là một provider sau khi hợp nhất config + DB — đơn vị mà
// loader dựng agent và mà UI liệt kê.
type ResolvedProvider struct {
	Slug string
	// FromConfig là slug có mặt trong configs/config.json. UI dùng nó để phân
	// biệt "chỉnh đè một provider của file" (xoá cài đặt là quay về file) với
	// "provider do UI tạo" (xoá là mất hẳn).
	FromConfig bool
	Config     config.LLMProviderConfig
}

// ProviderConfigs trả MỌI provider hiệu lực — config ∪ DB, sắp theo slug, bỏ
// provider admin tắt. Loader dùng nó lúc boot để provider tạo từ UI sống ngay
// sau lần restart, và để rebuild về sau dùng đúng một luật gộp.
func ProviderConfigs(llms config.LLMsConfig,
	settings map[string]*domain.LLMProviderSetting, secret string) []ResolvedProvider {
	out := make([]ResolvedProvider, 0, len(llms.Providers)+len(settings))
	for _, slug := range providerSlugs(llms, settings) {
		setting := settings[slug]
		if setting != nil && setting.Disabled {
			continue
		}
		base, fromConfig := providerBase(llms, slug)
		out = append(out, ResolvedProvider{
			Slug:       slug,
			FromConfig: fromConfig,
			Config:     MergeProviderSetting(base, setting, secret),
		})
	}
	return out
}

// providerSlugs là tập hợp (đã chuẩn hoá, sắp A→Z) slug trong config và slug có
// dòng cài đặt trong DB. Map config không có thứ tự nên phải sort để boot và
// output API tất định.
func providerSlugs(llms config.LLMsConfig, settings map[string]*domain.LLMProviderSetting) []string {
	seen := make(map[string]bool, len(llms.Providers)+len(settings))
	for raw := range llms.Providers {
		if slug := slugKey(raw); slug != "" {
			seen[slug] = true
		}
	}
	for slug := range settings {
		if key := slugKey(slug); key != "" {
			seen[key] = true
		}
	}
	out := make([]string, 0, len(seen))
	for slug := range seen {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

// providerBase lấy entry config của slug đã chuẩn hoá; DB-only trả struct rỗng.
func providerBase(llms config.LLMsConfig, slug string) (config.LLMProviderConfig, bool) {
	_, base, ok := lookupProvider(llms, slug)
	return base, ok
}

// providerSlug chuẩn hoá + chặn slug không dùng được làm khoá. Dấu "/" bị loại
// có chủ đích: slug đi vào URL path của /llm/providers/<slug>/... nên mọi ký tự
// phân cách đều làm hỏng route.
func providerSlug(raw string) (string, error) {
	slug := slugKey(raw)
	if slug == "" {
		return "", badLLMRequest("slug provider không được rỗng")
	}
	if !providerSlugRx.MatchString(slug) {
		return "", badLLMRequest(fmt.Sprintf(
			"slug %q không hợp lệ: chỉ chữ thường, số, - _ . và dài tối đa 64 ký tự", slug))
	}
	return slug, nil
}

// cleanModels gọn mảng model admin dán/nếu từ form: trim, bỏ rỗng, bỏ trùng, cắt
// trần số phần tử — giữ đúng khuôn mà providerModels (đường đọc) mong đợi.
func cleanModels(models []string) []string {
	seen := make(map[string]bool, len(models))
	out := make([]string, 0, len(models))
	for _, raw := range models {
		model := strings.TrimSpace(raw)
		if model == "" || seen[model] || len(out) >= providerModelsCap {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

// providerKeySource cho biết key đang dùng đến từ đâu — "" nghĩa là chưa có
// key (provider ollama local vẫn chạy được). Không giải mã, chỉ nhìn cột cipher.
func providerKeySource(pcfg config.LLMProviderConfig, setting *domain.LLMProviderSetting) string {
	if setting != nil && strings.TrimSpace(setting.APIKeyCipher) != "" {
		return keySourceDB
	}
	if strings.TrimSpace(pcfg.APIKey) != "" {
		return keySourceConfig
	}
	return ""
}

// decodeModelList đọc jsonb mảng model; hỏng/rỗng thì trả nil (config thắng).
func decodeModelList(raw string) []string {
	if strings.TrimSpace(raw) == "" || raw == "[]" {
		return nil
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		logs.Warnw("llm: cột models không phải mảng JSON hợp lệ, bỏ qua", "error", err)
		return nil
	}
	return models
}

// encodeModelList ghi mảng model xuống jsonb — luôn ra chuỗi hợp lệ để cột
// NOT NULL default '[]' không bị ghi đè bằng rỗng.
func encodeModelList(models []string) string {
	if len(models) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(models)
	if err != nil {
		return "[]"
	}
	return string(raw)
}
