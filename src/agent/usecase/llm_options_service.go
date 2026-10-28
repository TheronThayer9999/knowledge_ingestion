package usecase

import (
	"context"
	"strings"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// llmOptionsService phản chiếu provider LLM ra UI (Đợt 28) — nguồn duy nhất cho
// menu chọn model của chat và danh sách kết nối ở Settings. Provider đến từ
// configs/config.json VÀ từ bảng llm_provider_settings (admin tạo/sửa trong
// popup, nấc 3) — cả hai đường đều qua providerCatalog. Key KHÔNG BAO GIỜ rời
// khỏi process: DTO chỉ có tên provider, model và trạng thái key (admin),
// không có api_key.
type llmOptionsService struct {
	catalog *ProviderCatalog
	agents  IProviderRegistry
}

// NewLLMOptionsService là constructor fx gọi — catalog lo cấu hình, registry lo
// provider nào thật sự đang chạy được.
func NewLLMOptionsService(catalog *ProviderCatalog, agents IProviderRegistry) port.ILLMOptions {
	return &llmOptionsService{catalog: catalog, agents: agents}
}

// Options dựng danh sách lựa chọn; role do API đọc từ token rồi truyền vào
// (user thường không nhận base_url/has_key). Không gọi LLM.
func (s *llmOptionsService) Options(ctx context.Context, role int) dtos.Result[*dtos.LLMOptionsResponse] {
	return dtos.Ok(buildLLMOptions(s.catalog, s.agents, s.catalog.Settings(ctx), role))
}

// buildLLMOptions tách khỏi Options để test được thuần: liệt kê config ∪ DB vì
// từ Đợt 28 nấc 3 provider có thể chỉ tồn tại trong bảng cài đặt (admin tạo từ
// UI, không sửa file config). Sắp theo slug vì config là map (thứ tự không xác
// định), chốt default model đúng provider mặc định.
//
// Provider bị tắt chỉ hiện với admin — menu chat của user thường không được thấy
// cái mà chat sẽ từ chối.
func buildLLMOptions(catalog *ProviderCatalog, agents IProviderRegistry,
	settings map[string]*domain.LLMProviderSetting, role int) *dtos.LLMOptionsResponse {
	defaultSlug := effectiveProviderSlug(catalog.ConfiguredDefault(), agents)
	admin := role >= domain.RoleAdmin
	slugs := catalog.Slugs(settings)
	res := &dtos.LLMOptionsResponse{
		DefaultProvider: defaultSlug,
		Providers:       make([]*dtos.LLMProviderOption, 0, len(slugs)),
	}
	for _, slug := range slugs {
		setting := settings[slug]
		if setting != nil && setting.Disabled && !admin {
			continue
		}
		base, fromConfig := catalog.Base(slug)
		opt := buildProviderOption(slug, base, setting, agents, defaultSlug, admin)
		opt.FromConfig = fromConfig
		if opt.IsDefault {
			res.DefaultModel = opt.DefaultModel
		}
		res.Providers = append(res.Providers, opt)
	}
	return res
}

// effectiveProviderSlug chuẩn hoá default_provider giống Resolve: rỗng thì rơi
// về slug ollama mặc định để UI và chat nói cùng một provider.
func effectiveProviderSlug(configured string, agents IProviderRegistry) string {
	if slug := slugKey(configured); slug != "" {
		return slug
	}
	if agents != nil && agents.DefaultProvider() != "" {
		return agents.DefaultProvider()
	}
	return eino.ProviderOllama
}

// buildProviderOption dựng 1 dòng cho UI/API từ cấu hình config + cài đặt DB
// của cùng provider: định nghĩa (label/type/base_url), model và danh sách model
// trong DB đều thắng config. base_url/has_key/key_source/config_model/
// from_config/disabled chỉ dành cho admin.
func buildProviderOption(slug string, pcfg config.LLMProviderConfig, setting *domain.LLMProviderSetting,
	agents IProviderRegistry, defaultSlug string, admin bool) *dtos.LLMProviderOption {
	effective := withSettingDef(withSettingModel(pcfg, setting), setting)
	opt := &dtos.LLMProviderOption{
		Slug:         slug,
		Label:        providerLabel(slug, effective),
		Type:         effective.Type,
		DefaultModel: strings.TrimSpace(effective.Model),
		Models:       providerModels(effective),
		Available:    agents.Has(slug),
		Thinking:     agents.ThinkingDistinct(slug),
		IsDefault:    slug == defaultSlug,
	}
	if admin {
		opt.BaseURL = strings.TrimRight(strings.TrimSpace(effective.BaseURL), "/")
		source := providerKeySource(pcfg, setting)
		hasKey := source != ""
		opt.HasKey = &hasKey
		opt.KeySource = source
		opt.ConfigModel = strings.TrimSpace(pcfg.Model)
		opt.Disabled = setting != nil && setting.Disabled
	}
	return opt
}

// providerLabel fallback về slug — UI không phải tự đoán khi config cũ chưa
// có field label.
func providerLabel(slug string, pcfg config.LLMProviderConfig) string {
	if label := strings.TrimSpace(pcfg.Label); label != "" {
		return label
	}
	return slug
}

// providerModels trim/dedup theo thứ tự khai báo rồi chèn model mặc định lên
// đầu nếu chưa có — menu chat không được thiếu đúng model sẽ chạy khi request
// bỏ trống, kể cả khi danh sách vừa tải từ provider khác model đó.
func providerModels(pcfg config.LLMProviderConfig) []string {
	models := make([]string, 0, len(pcfg.Models)+2)
	seen := make(map[string]bool, len(pcfg.Models)+1)
	for _, raw := range pcfg.Models {
		model := strings.TrimSpace(raw)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	if model := strings.TrimSpace(pcfg.Model); model != "" && !seen[model] {
		models = append([]string{model}, models...)
	}
	return models
}
