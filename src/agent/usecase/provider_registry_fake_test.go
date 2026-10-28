package usecase

import (
	"fmt"
	"sort"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/config"
)

// fakeRegistry là IProviderRegistryAdmin của test: không client LLM, không cần
// dựng agent thật — test khai provider nào "đã dựng được agent", provider nào có
// bản suy nghĩ riêng, và ghi lại mọi thao tác ghi để assert service gọi đúng
// bước (Rebuild sau khi lưu key, Remove khi tắt provider…).
type fakeRegistry struct {
	defaultSlug string
	available   map[string]bool
	thinking    map[string]bool

	rebuilt map[string]config.LLMProviderConfig
	removed []string
	models  map[string]string
	// rebuildErr dựng sẵn để test đường "lưu rồi nhưng không dựng được client".
	rebuildErr error
}

func newFakeRegistry(defaultSlug string, available ...string) *fakeRegistry {
	f := &fakeRegistry{
		defaultSlug: defaultSlug,
		available:   make(map[string]bool, len(available)),
		thinking:    make(map[string]bool),
		rebuilt:     make(map[string]config.LLMProviderConfig),
		models:      make(map[string]string),
	}
	for _, slug := range available {
		f.available[slug] = true
	}
	return f
}

// withThinking đánh dấu provider có client "suy nghĩ" RIÊNG (chỉ ollama thật sự
// có), trả chính nó để test nối lệnh khai báo.
func (f *fakeRegistry) withThinking(slugs ...string) *fakeRegistry {
	for _, slug := range slugs {
		f.thinking[slug] = true
	}
	return f
}

// markReady dựng thêm provider "đã có agent" sau khi tạo fixture — mô phỏng đúng
// lúc boot dựng xong agent cho một provider chỉ có trong DB (admin tạo từ UI),
// để test danh sách thấy cả available lẫn from_config.
func (f *fakeRegistry) markReady(slugs ...string) *fakeRegistry {
	for _, slug := range slugs {
		f.available[slugKey(slug)] = true
	}
	return f
}

func (f *fakeRegistry) EffectiveProvider(provider string) string {
	if name := strings.ToLower(strings.TrimSpace(provider)); name != "" {
		return name
	}
	return f.defaultSlug
}

func (f *fakeRegistry) Resolve(provider, model string, thinking bool) (*eino.ChatAgent, string, error) {
	name := f.EffectiveProvider(provider)
	if !f.available[name] {
		return nil, "", fmt.Errorf("eino agent: provider %q chưa cấu hình (mới có: %s)", name, f.AvailableProviders())
	}
	if model == "" {
		model = f.models[name]
	}
	return &eino.ChatAgent{}, model, nil
}

func (f *fakeRegistry) DefaultProvider() string { return f.defaultSlug }

func (f *fakeRegistry) RawModel(string) (einomodel.ToolCallingChatModel, bool) { return nil, false }

func (f *fakeRegistry) Has(slug string) bool {
	return f.available[strings.ToLower(strings.TrimSpace(slug))]
}

func (f *fakeRegistry) ThinkingDistinct(slug string) bool {
	name := strings.ToLower(strings.TrimSpace(slug))
	return f.available[name] && f.thinking[name]
}

func (f *fakeRegistry) AvailableProviders() string {
	names := make([]string, 0, len(f.available))
	for slug, ok := range f.available {
		if ok {
			names = append(names, slug)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (f *fakeRegistry) Rebuild(slug string, pcfg config.LLMProviderConfig) error {
	name := strings.ToLower(strings.TrimSpace(slug))
	if f.rebuildErr != nil {
		return f.rebuildErr
	}
	f.rebuilt[name] = pcfg
	f.available[name] = true
	return nil
}

func (f *fakeRegistry) Remove(slug string) {
	name := strings.ToLower(strings.TrimSpace(slug))
	f.removed = append(f.removed, name)
	delete(f.available, name)
	delete(f.thinking, name)
}

func (f *fakeRegistry) SetDefaultModel(slug, model string) {
	f.models[strings.ToLower(strings.TrimSpace(slug))] = strings.TrimSpace(model)
}
