package usecase

import (
	"context"
	"sort"
	"strings"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// llmOptionsService phản chiếu cấu hình llms.providers ra UI (Đợt 28) — nguồn
// duy nhất cho menu chọn model của chat và danh sách kết nối ở Settings. Key
// KHÔNG BAO GIỜ rời khỏi process: DTO chỉ có tên provider, model và trạng thái
// key (admin), không có api_key.
type llmOptionsService struct {
	llms   config.LLMsConfig
	agents *eino.ProviderAgents
}

func NewLLMOptionsService(cfg config.IConfig, agents *eino.ProviderAgents) port.ILLMOptions {
	return &llmOptionsService{llms: cfg.GetLLMs(), agents: agents}
}

// Options dựng danh sách lựa chọn; role do API đọc từ token rồi truyền vào
// (user thường không nhận base_url/has_key). Không đọc DB, không gọi LLM.
func (s *llmOptionsService) Options(_ context.Context, role int) dtos.Result[*dtos.LLMOptionsResponse] {
	return dtos.Ok(buildLLMOptions(s.llms, s.agents, role))
}

// buildLLMOptions tách khỏi Options để test được thuần (không cần fake
// config.IConfig): sắp provider theo slug vì config là map (thứ tự không xác
// định), chốt default model đúng provider mặc định.
func buildLLMOptions(llms config.LLMsConfig, agents *eino.ProviderAgents, role int) *dtos.LLMOptionsResponse {
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
		opt := buildProviderOption(
			strings.ToLower(strings.TrimSpace(rawSlug)),
			llms.Providers[rawSlug], agents, defaultSlug, admin)
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
	if slug := strings.ToLower(strings.TrimSpace(configured)); slug != "" {
		return slug
	}
	if agents != nil && agents.DefaultProvider != "" {
		return agents.DefaultProvider
	}
	return eino.ProviderOllama
}

func buildProviderOption(slug string, pcfg config.LLMProviderConfig,
	agents *eino.ProviderAgents, defaultSlug string, admin bool) *dtos.LLMProviderOption {
	opt := &dtos.LLMProviderOption{
		Slug:         slug,
		Label:        providerLabel(slug, pcfg),
		Type:         pcfg.Type,
		DefaultModel: strings.TrimSpace(pcfg.Model),
		Models:       providerModels(pcfg),
		Available:    providerAvailable(agents, slug),
		Thinking:     providerThinking(agents, slug),
		IsDefault:    slug == defaultSlug,
	}
	if admin {
		opt.BaseURL = strings.TrimRight(strings.TrimSpace(pcfg.BaseURL), "/")
		hasKey := strings.TrimSpace(pcfg.APIKey) != ""
		opt.HasKey = &hasKey
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

// providerModels trim/dedup theo thứ tự khai báo; rỗng thì lấy đúng model mặc
// định hiện có để config cũ (chỉ có `model`) vẫn ra menu một dòng.
func providerModels(pcfg config.LLMProviderConfig) []string {
	models := make([]string, 0, len(pcfg.Models)+1)
	seen := make(map[string]bool, len(pcfg.Models))
	for _, raw := range pcfg.Models {
		model := strings.TrimSpace(raw)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	if len(models) == 0 {
		if model := strings.TrimSpace(pcfg.Model); model != "" {
			models = append(models, model)
		}
	}
	return models
}

// providerAvailable: loader đã dựng được agent nghĩa là key/cấu hình hợp lệ.
// Provider thiếu key vẫn nằm trong config nên vẫn hiện cho admin, nhưng FE
// không đưa vào menu chat.
func providerAvailable(agents *eino.ProviderAgents, slug string) bool {
	if agents == nil {
		return false
	}
	_, ok := agents.Agents[slug]
	return ok
}

// providerThinking chỉ true khi có client suy nghĩ RIÊNG — ThinkingAgents của
// provider không có công tắc được gán đúng agent thường (fallback), nên so
// con trỏ để không báo "hỗ trợ" sai cho nhóm OpenAI-compatible.
func providerThinking(agents *eino.ProviderAgents, slug string) bool {
	if agents == nil {
		return false
	}
	regular, ok := agents.Agents[slug]
	deep, has := agents.ThinkingAgents[slug]
	return ok && has && deep != nil && deep != regular
}
