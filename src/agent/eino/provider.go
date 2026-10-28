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
// validation. Backend có công tắc "suy nghĩ" riêng thì triển khai thêm
// thinkingModeler ở dưới — đó là năng lực TÙY CHỌN, không bắt buộc.
type LLMProvider interface {
	// Name là slug request chọn qua field `provider` (vd "ollama", "groq").
	Name() string
	// NewModel dựng client tool-calling; thiếu key/cấu hình thì trả lỗi để
	// registry bỏ qua provider này (boot vẫn lên).
	NewModel() (einomodel.ToolCallingChatModel, error)
	// DefaultModel là model khi request không ép model.
	DefaultModel() string
}

// thinkingModeler là năng lực tùy chọn: dựng thêm 1 client đã bật chế độ suy
// nghĩ của model. Lý do để tách riêng thay vì sửa NewModel(): LLMProvider có
// nơi dùng chẳng liên quan gì tới chat (worker compile lấy ISummarizer) — thêm
// tham số vào interface chính sẽ lan sửa sang mọi caller vì một cờ chỉ
// request chat mới quan tâm. Trả (nil, nil) = backend không có công tắc đó.
type thinkingModeler interface {
	NewThinkingModel() (einomodel.ToolCallingChatModel, error)
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
		return NewChatModel(p.cfg, false)
	}
}

// NewThinkingModel dựng bản client đã bật suy nghĩ. Chỉ Ollama có flag riêng
// (`think` trong /api/chat); nhóm OpenAI-compatible không có công tắc chung —
// model nào tự trả reasoning thì ReasoningContent vẫn về được FE, nên trả
// nil để registry dùng lại agent thường thay vì dựng client thứ hai vô ích
// (client thứ hai còn là một rate limiter thứ hai, dễ vượt cap rpm của backend).
func (p configProvider) NewThinkingModel() (einomodel.ToolCallingChatModel, error) {
	if p.cfg.Type != config.LLMTypeOllama {
		return nil, nil
	}
	return NewChatModel(p.cfg, true)
}

func (p configProvider) DefaultModel() string { return p.cfg.Model }

// BuildProviderAgents dựng agent cho từng provider đã đăng ký — provider nào
// NewModel lỗi (thiếu key/cấu hình) thì warn + bỏ qua, boot vẫn lên; request
// nào chọn nó mới 400. Tools dùng chung instance cho mọi agent (stateless theo
// request qua Scope trong ctx). Không còn agent nào thì fail-fast vì app mù LLM.
func BuildProviderAgents(system InstructionSet, defaultProvider string, tools []einotool.BaseTool, providers ...LLMProvider) (*ProviderAgents, error) {
	out := &ProviderAgents{
		Agents:          make(map[string]*ChatAgent),
		ThinkingAgents:  make(map[string]*ChatAgent),
		Models:          make(map[string]einomodel.ToolCallingChatModel),
		DefaultProvider: strings.ToLower(strings.TrimSpace(defaultProvider)),
		DefaultModel:    make(map[string]string),
		system:          system,
		tools:           tools,
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
		out.ThinkingAgents[name] = buildThinkingAgent(name, p, system, tools, agent)
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

// buildThinkingAgent dựng bản agent "suy nghĩ" cho 1 provider — provider không
// triển khai thinkingModeler, không hỗ trợ, hoặc dựng lỗi thì trả về đúng agent
// thường: cờ thinking của request khi đó chỉ thành "không có tác dụng ở backend
// này", chat vẫn chạy chứ không chết lúc boot hay thành 500.
func buildThinkingAgent(
	name string, p LLMProvider, system InstructionSet,
	tools []einotool.BaseTool, fallback *ChatAgent,
) *ChatAgent {
	tm, ok := p.(thinkingModeler)
	if !ok {
		return fallback
	}
	model, err := tm.NewThinkingModel()
	if err != nil {
		logs.Warnw("agent: không dựng được bản suy nghĩ, dùng bản thường", "provider", name, "error", err)
		return fallback
	}
	if model == nil {
		return fallback
	}
	agent, err := NewChatAgent(model, system, tools...)
	if err != nil {
		logs.Warnw("agent: dựng agent suy nghĩ lỗi, dùng bản thường", "provider", name, "error", err)
		return fallback
	}
	return agent
}

func availableProviders(m map[string]*ChatAgent) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// Rebuild dựng lại client + agent của MỘT provider rồi thay vào registry đang
// chạy — dùng khi admin lưu/xoá API key trong popup (Đợt 28 nấc 2): key được
// nhúng vào client lúc NewModel nên đổi key là phải dựng client mới, không có
// call option per-request. pcfg là cấu hình ĐÃ merge (DB thắng config).
//
// Không đụng DefaultProvider. Lượt chat đang chạy giữ agent cũ tới khi xong —
// thay map dưới Lock nên không ai đọc thấy trạng thái nửa vời.
func (p *ProviderAgents) Rebuild(slug string, pcfg config.LLMProviderConfig) error {
	if p == nil {
		return fmt.Errorf("eino agent: chưa được dựng")
	}
	name := NormalizeSlug(slug)
	if name == "" {
		return fmt.Errorf("eino agent: thiếu slug provider")
	}
	provider := NewConfigProvider(name, pcfg)
	model, err := provider.NewModel()
	if err != nil {
		return err
	}
	agent, err := NewChatAgent(model, p.system, p.tools...)
	if err != nil {
		return err
	}
	thinking := buildThinkingAgent(name, provider, p.system, p.tools, agent)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Agents[name] = agent
	p.Models[name] = model
	p.ThinkingAgents[name] = thinking
	p.DefaultModel[name] = provider.DefaultModel()
	return nil
}

// Remove bỏ một provider khỏi registry đang chạy — admin tắt hoặc xoá provider
// trong popup (Đợt 28 nấc 3). Lượt chat đang chạy giữ agent cũ trong stack của
// nó nên không giữa chừng mất model; chỉ request mới không tra thấy provider
// nữa và Resolve trả lỗi "chưa cấu hình" (API map 400).
//
// Không đụng DefaultProvider: service gọi trước một bước chặn "đang là provider
// mặc định" để không tự biến app thành mù LLM.
func (p *ProviderAgents) Remove(slug string) {
	if p == nil {
		return
	}
	name := NormalizeSlug(slug)
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.Agents, name)
	delete(p.ThinkingAgents, name)
	delete(p.Models, name)
	delete(p.DefaultModel, name)
}

// Has cho biết registry đang có agent của provider này — UI dùng để báo
// "provider chưa sẵn sàng" sau khi lưu mà không cần đoán qua lỗi Resolve.
func (p *ProviderAgents) Has(slug string) bool {
	_, ok := p.Agent(slug)
	return ok
}
