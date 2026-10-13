package eino

import (
	"fmt"
	"sort"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
)

// LLMProvider là hợp đồng 1 backend LLM cắm vào chat — mặc định đã có
// configProvider (provider.go) cover cả "ollama" lẫn mọi backend
// OpenAI-compatible qua type trong config; thêm protocol lạ hẳn mới cần
// struct implement 3 method này. Không sửa usecase, prompt, Resolve hay
// validation.
type LLMProvider interface {
	// Name là slug request chọn qua field `provider` (vd "ollama", "groq").
	Name() string
	// NewModel dựng client tool-calling; thiếu key/cấu hình thì trả lỗi để
	// registry bỏ qua provider này (boot vẫn lên).
	NewModel() (einomodel.ToolCallingChatModel, error)
	// DefaultModel là model khi request không ép model.
	DefaultModel() string
}

// configProvider bọc 1 entry llms.providers (config.LLMProviderConfig) thành
// LLMProvider — `type` trong config chọn cách dựng client ("ollama" native,
// "openai" cho mọi backend OpenAI-compatible). Thêm backend mới chỉ cần
// khai báo config, không viết struct provider mới.
type configProvider struct {
	slug string
	cfg  config.LLMProviderConfig
}

// NewConfigProvider đăng ký backend từ 1 entry config (slug là key trong
// map llms.providers — request chọn qua field `provider`).
func NewConfigProvider(slug string, cfg config.LLMProviderConfig) LLMProvider {
	return configProvider{slug: strings.ToLower(strings.TrimSpace(slug)), cfg: cfg}
}

func (p configProvider) Name() string { return p.slug }

func (p configProvider) NewModel() (einomodel.ToolCallingChatModel, error) {
	switch p.cfg.Type {
	case config.LLMTypeOpenAI:
		return NewOpenAIChatModel(p.slug, p.cfg)
	default: // config.LLMTypeOllama — Load đã validate, default an toàn
		return NewChatModel(p.cfg)
	}
}

func (p configProvider) DefaultModel() string { return p.cfg.Model }

// BuildProviderAgents dựng agent cho từng provider đã đăng ký — provider nào
// NewModel lỗi (thiếu key/cấu hình) thì warn + bỏ qua, boot vẫn lên; request
// nào chọn nó mới 400. Tools dùng chung instance cho mọi agent (stateless theo
// request qua Scope trong ctx). Không còn agent nào thì fail-fast vì app mù LLM.
func BuildProviderAgents(system, defaultProvider string, tools []einotool.BaseTool, providers ...LLMProvider) (*ProviderAgents, error) {
	out := &ProviderAgents{
		Agents:          make(map[string]*ChatAgent),
		Models:          make(map[string]einomodel.ToolCallingChatModel),
		DefaultProvider: strings.ToLower(strings.TrimSpace(defaultProvider)),
		DefaultModel:    make(map[string]string),
	}
	if out.DefaultProvider == "" {
		out.DefaultProvider = ProviderOllama
	}
	for _, p := range providers {
		if p == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(p.Name()))
		if name == "" {
			continue
		}
		model, err := p.NewModel()
		if err != nil {
			logs.Warnw("agent: bỏ qua provider (thiếu cấu hình)", "provider", name, "error", err)
			continue
		}
		agent, err := NewChatAgent(model, system, tools...)
		if err != nil {
			return nil, err
		}
		out.Agents[name] = agent
		out.Models[name] = model
		out.DefaultModel[name] = p.DefaultModel()
	}
	if len(out.Agents) == 0 {
		return nil, fmt.Errorf("eino agent: không dựng được provider nào")
	}
	if _, ok := out.Agents[out.DefaultProvider]; !ok {
		logs.Warnw("agent: provider mặc định chưa cấu hình, request không ép provider sẽ 400",
			"default", out.DefaultProvider, "available", availableProviders(out.Agents))
	}
	return out, nil
}

func availableProviders(m map[string]*ChatAgent) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
