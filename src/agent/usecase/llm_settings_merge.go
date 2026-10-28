package usecase

import (
	"encoding/json"
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

// mergeProviderSetting là cấu hình THẬT dùng để dựng client: model + key của
// DB (nếu có) đè lên config. Loader lúc boot và lần rebuild sau khi lưu key
// đều đi qua đây để hai đường không lệch nhau.
func MergeProviderSetting(base config.LLMProviderConfig, setting *domain.LLMProviderSetting, secret string) config.LLMProviderConfig {
	return withSettingKey(withSettingModel(base, setting), setting, secret)
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
