package usecase

import (
	"context"
	"sort"
	"strings"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// llmOptionsService phản chiếu cấu hình llms.providers ra UI (Đợt 28) — nguồn
// duy nhất cho menu chọn model của chat và danh sách kết nối ở Settings. Config
// là nơi khai báo provider; bảng llm_provider_settings chỉ ghi đè key/model mà
// admin nhập trong popup. Key KHÔNG BAO GIỜ rời khỏi process: DTO chỉ có tên
// provider, model và trạng thái key (admin), không có api_key.
type llmOptionsService struct {
	llms   config.LLMsConfig
	agents *eino.ProviderAgents
	repo   domain.ILLMProviderSettingRepository
}

func NewLLMOptionsService(cfg config.IConfig, agents *eino.ProviderAgents,
	repo domain.ILLMProviderSettingRepository) port.ILLMOptions {
	return &llmOptionsService{llms: cfg.GetLLMs(), agents: agents, repo: repo}
}

// Options dựng danh sách lựa chọn; role do API đọc từ token rồi truyền vào
// (user thường không nhận base_url/has_key). Không gọi LLM.
func (s *llmOptionsService) Options(ctx context.Context, role int) dtos.Result[*dtos.LLMOptionsResponse] {
	return dtos.Ok(buildLLMOptions(s.llms, s.agents, s.settings(ctx), role))
}

// settings đọc bảng cài đặt provider. Lỗi đọc thì WARN rồi coi như rỗng — menu
// model và chat vẫn chạy theo config, không vì một bảng phụ mà đứt cả trang.
func (s *llmOptionsService) settings(ctx context.Context) map[string]*domain.LLMProviderSetting {
	rows, err := s.repo.List(ctx)
	if err != nil {
		logs.Warnw("llm: không đọc được cài đặt provider, dùng config", "error", err)
		return nil
	}
	out := make(map[string]*domain.LLMProviderSetting, len(rows))
	for _, row := range rows {
		out[slugKey(row.Slug)] = row
	}
	return out
}

// buildLLMOptions tách khỏi Options để test được thuần (không cần fake
// config.IConfig/repo): sắp provider theo slug vì config là map (thứ tự không
// xác định), chốt default model đúng provider mặc định.
func buildLLMOptions(llms config.LLMsConfig, agents *eino.ProviderAgents,
	settings map[string]*domain.LLMProviderSetting, role int) *dtos.LLMOptionsResponse {
	defaultSlug := effectiveProviderSlug(llms.DefaultProvider, agents)
	admin := role >= domain.RoleAdmin
	slugs := make([]string, 0, len(llms.Providers))
	for slug := range llms.Providers {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	res := &dtos.LLMOptionsResponse{
		DefaultProvider: defaultSlug,
		Providers:       make([]*dtos.LLMProviderOption, 0, len(slugs)),
	}
	for _, rawSlug := range slugs {
		slug := slugKey(rawSlug)
		opt := buildProviderOption(slug, llms.Providers[rawSlug], settings[slug], agents, defaultSlug, admin)
		if opt.IsDefault {
			res.DefaultModel = opt.DefaultModel
		}
		res.Providers = append(res.Providers, opt)
	}
	return res
}

// effectiveProviderSlug chuẩn hoá default_provider giống Resolve: rỗng thì rơi
// về slug ollama mặc định để UI và chat nói cùng một provider.
func effectiveProviderSlug(configured string, agents *eino.ProviderAgents) string {
	if slug := slugKey(configured); slug != "" {
		return slug
	}
	if agents != nil && agents.DefaultProvider != "" {
		return agents.DefaultProvider
	}
	return eino.ProviderOllama
}

// buildProviderOption dựng 1 dòng cho UI/API từ cấu hình config + cài đặt DB
// của cùng provider (model DB thắng config). base_url/has_key/key_source/
// config_model chỉ dành cho admin.
func buildProviderOption(slug string, pcfg config.LLMProviderConfig, setting *domain.LLMProviderSetting,
	agents *eino.ProviderAgents, defaultSlug string, admin bool) *dtos.LLMProviderOption {
	effective := withSettingModel(pcfg, setting)
	opt := &dtos.LLMProviderOption{
		Slug:         slug,
		Label:        providerLabel(slug, pcfg),
		Type:         pcfg.Type,
		DefaultModel: strings.TrimSpace(effective.Model),
		Models:       providerModels(effective),
		Available:    providerAvailable(agents, slug),
		Thinking:     providerThinking(agents, slug),
		IsDefault:    slug == defaultSlug,
	}
	if admin {
		opt.BaseURL = strings.TrimRight(strings.TrimSpace(pcfg.BaseURL), "/")
		source := providerKeySource(pcfg, setting)
		hasKey := source != ""
		opt.HasKey = &hasKey
		opt.KeySource = source
		opt.ConfigModel = strings.TrimSpace(pcfg.Model)
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

// providerAvailable: loader đã dựng được agent nghĩa là key/cấu hình hợp lệ.
// Provider thiếu key vẫn nằm trong config nên vẫn hiện cho admin, nhưng FE
// không đưa vào menu chat.
func providerAvailable(agents *eino.ProviderAgents, slug string) bool {
	_, ok := agents.Agent(slug)
	return ok
}

// providerThinking chỉ true khi có client suy nghĩ RIÊNG (xem
// ProviderAgents.ThinkingDistinct) — nhóm OpenAI-compatible dùng lại agent
// thường nên không được báo "hỗ trợ" sai.
func providerThinking(agents *eino.ProviderAgents, slug string) bool {
	return agents.ThinkingDistinct(slug)
}
