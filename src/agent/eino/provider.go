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

// NewProviderAgents dựng agent cho từng provider đã đăng ký — provider nào
// NewModel lỗi (thiếu key/cấu hình) thì warn + bỏ qua, boot vẫn lên; request
// nào chọn nó mới 400. Tools dùng chung instance cho mọi agent (stateless theo
// request qua Scope trong ctx). Không còn agent nào thì fail-fast vì app mù LLM.
func NewProviderAgents(system InstructionSet, defaultProvider string, tools []einotool.BaseTool, providers ...LLMProvider) (*ProviderAgents, error) {
	out := newProviderRegistry(system, defaultProvider, tools)
	for _, p := range providers {
		if p == nil {
			continue
		}
		name := NormalizeSlug(p.Name())
		if name == "" {
			continue
		}
		entry, err := newProviderEntry(name, p, system, tools)
		if err != nil {
			logs.Warnw("agent: bỏ qua provider (thiếu cấu hình)", "provider", name, "error", err)
			continue
		}
		out.put(name, entry)
	}
	if len(out.agents) == 0 {
		return nil, fmt.Errorf("eino agent: không dựng được provider nào")
	}
	if !out.Has(out.defaultProvider) {
		logs.Warnw("agent: provider mặc định chưa cấu hình, request không ép provider sẽ 400",
			"default", out.defaultProvider, "available", out.AvailableProviders())
	}
	return out, nil
}

// newProviderRegistry dựng shell registry với 4 map rỗng — constructor duy nhất
// được phép tạo ProviderAgents (bên ngoài không còn composite literal).
func newProviderRegistry(system InstructionSet, defaultProvider string, tools []einotool.BaseTool) *ProviderAgents {
	name := NormalizeSlug(defaultProvider)
	if name == "" {
		name = ProviderOllama
	}
	return &ProviderAgents{
		agents:          make(map[string]*ChatAgent),
		thinkingAgents:  make(map[string]*ChatAgent),
		models:          make(map[string]einomodel.ToolCallingChatModel),
		defaultModel:    make(map[string]string),
		defaultProvider: name,
		system:          system,
		tools:           tools,
	}
}

// providerEntry là một bộ đã dựng xong của MỘT provider: agent thường, bản suy
// nghĩ (có thể trùng agent thường) kèm client thô và model mặc định. Cả 2 đường
// dựng — boot (NewProviderAgents) và rebuild sau khi admin lưu key (Rebuild) —
// đi qua nó nên không bao giờ lệch nhau.
type providerEntry struct {
	agent        *ChatAgent
	thinking     *ChatAgent
	model        einomodel.ToolCallingChatModel
	defaultModel string
}

// newProviderEntry dựng client + agent cho 1 provider. Trả lỗi khi thiếu
// key/cấu hình để caller bỏ qua (boot) hoặc báo 400 (lưu từ popup).
func newProviderEntry(name string, p LLMProvider, system InstructionSet, tools []einotool.BaseTool) (*providerEntry, error) {
	model, err := p.NewModel()
	if err != nil {
		return nil, err
	}
	agent, err := NewChatAgent(model, system, tools...)
	if err != nil {
		return nil, err
	}
	return &providerEntry{
		agent:        agent,
		thinking:     buildThinkingAgent(name, p, system, tools, agent),
		model:        model,
		defaultModel: p.DefaultModel(),
	}, nil
}

// put ghi một bộ provider vào registry dưới khoá ghi — đường ghi DUY NHẤT của
// 4 map, nên không ai mutate registry lúc đang chạy mà không qua khoá.
func (p *ProviderAgents) put(name string, entry *providerEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agents[name] = entry.agent
	p.thinkingAgents[name] = entry.thinking
	p.models[name] = entry.model
	p.defaultModel[name] = entry.defaultModel
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
// Không đụng defaultProvider. Lượt chat đang chạy giữ agent cũ tới khi xong —
// thay map dưới Lock nên không ai đọc thấy trạng thái nửa vời.
func (p *ProviderAgents) Rebuild(slug string, pcfg config.LLMProviderConfig) error {
	if p == nil {
		return fmt.Errorf("eino agent: chưa được dựng")
	}
	name := NormalizeSlug(slug)
	if name == "" {
		return fmt.Errorf("eino agent: thiếu slug provider")
	}
	entry, err := newProviderEntry(name, NewConfigProvider(name, pcfg), p.system, p.tools)
	if err != nil {
		return err
	}
	p.put(name, entry)
	return nil
}

// Remove bỏ một provider khỏi registry đang chạy — admin tắt hoặc xoá provider
// trong popup (Đợt 28 nấc 3). Lượt chat đang chạy giữ agent cũ trong stack của
// nó nên không giữa chừng mất model; chỉ request mới không tra thấy provider
// nữa và Resolve trả lỗi "chưa cấu hình" (API map 400).
//
// Không đụng defaultProvider: service gọi trước một bước chặn "đang là provider
// mặc định" để không tự biến app thành mù LLM.
func (p *ProviderAgents) Remove(slug string) {
	if p == nil {
		return
	}
	name := NormalizeSlug(slug)
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.agents, name)
	delete(p.thinkingAgents, name)
	delete(p.models, name)
	delete(p.defaultModel, name)
}

// Has cho biết registry đang có agent của provider này — UI dùng để báo
// "provider chưa sẵn sàng" sau khi lưu mà không cần đoán qua lỗi Resolve.
func (p *ProviderAgents) Has(slug string) bool {
	_, ok := p.Agent(slug)
	return ok
}

// AvailableProviders liệt kê slug đang dựng được agent, sắp A→Z — lời lỗi "chưa
// cấu hình" và dòng warn lúc boot dùng chung một nguồn, không tự đoán từ map.
func (p *ProviderAgents) AvailableProviders() string {
	if p == nil {
		return ""
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return availableProviders(p.agents)
}
