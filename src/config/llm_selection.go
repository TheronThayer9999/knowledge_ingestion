package config

import (
	"fmt"
	"sort"
	"strings"
)

// Lựa chọn LLM theo thứ tự ưu tiên của các CLI quen thuộc (aider, llm của
// Simon Willison, LiteLLM): flag > env > file config > default trong code.
// Toàn bộ phần này nằm trong Load (không nằm ở main) để api và worker dùng
// chung một luật — worker compile cũng đọc llms.DefaultProvider.
//
// CỐ Ý không có dạng gộp "provider/model" như litellm: model thật của repo
// này có dấu "/" (vd 9router "cl/nvidia/nemotron-3.5-super-vl:free"), tách
// theo "/" đầu tiên sẽ cắt sai id. Vì vậy 2 lớp riêng: provider slug và model.

// LoadOption tinh chỉnh config sau khi đã đọc + validate file.
type LoadOption func(*appConfig) error

// WithLLMSelection ép provider (và model của provider đó) từ flag CLI — lớp
// ưu tiên cao nhất, chạy sau env nên đè được env.
func WithLLMSelection(provider, model string) LoadOption {
	return func(cfg *appConfig) error { return applyLLMSelection(cfg, provider, model) }
}

// applyLLMSelection áp 1 lớp lựa chọn lên config đã validate. Quy ước:
//   - provider rỗng  → giữ provider hiện tại (model một mình áp lên nó);
//   - model rỗng     → giữ model đang khai cho provider;
//   - cả hai rỗng    → không làm gì (giữ nguyên hành vi cũ, kể cả config để
//     provider mặc định thiếu model).
//
// Chỉ khi người dùng CHỦ ĐỘNG chọn (env/flag) mới fail-fast: slug lạ hoặc
// model rỗng phải chết ngay lúc boot kèm danh sách slug hợp lệ, thay vì để
// request chat đầu tiên mới trả 400.
func applyLLMSelection(cfg *appConfig, provider, model string) error {
	nameIn := strings.ToLower(strings.TrimSpace(provider))
	modelIn := strings.TrimSpace(model)
	if nameIn == "" && modelIn == "" {
		return nil
	}
	name := nameIn
	if name == "" {
		name = cfg.LLMs.DefaultProvider
	}
	p, ok := cfg.LLMs.Providers[name]
	if !ok {
		return fmt.Errorf("llms: provider %q chưa cấu hình (đang có: %s)",
			name, strings.Join(llmSlugs(cfg.LLMs.Providers), ", "))
	}
	if modelIn != "" {
		p.Model = modelIn
		cfg.LLMs.Providers[name] = p
	}
	if strings.TrimSpace(p.Model) == "" {
		return fmt.Errorf("llms.providers.%s.model đang trống: thêm -model <id> hoặc khai model trong config", name)
	}
	cfg.LLMs.DefaultProvider = name
	return nil
}

// llmSlugs trả slug provider đã sắp xếp — dùng cho thông báo lỗi và -list-llms
// (map Go không có thứ tự nên phải sort để output ổn định).
func llmSlugs(m map[string]LLMProviderConfig) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// FormatLLMs in bảng provider/model đã cấu hình cho flag -list-llms. Cố ý
// KHÔNG in api_key (chỉ key=set|missing): terminal hay bị chụp lại, và thiếu
// key chính là lý do loader bỏ qua một provider lúc boot — nhìn cột này là
// biết provider nào dùng được thật.
func FormatLLMs(cfg IConfig) string {
	llms := cfg.GetLLMs()
	var b strings.Builder
	fmt.Fprintf(&b, "provider mặc định: %s\n", llms.DefaultProvider)
	for _, slug := range llmSlugs(llms.Providers) {
		p := llms.Providers[slug]
		mark := " "
		if slug == llms.DefaultProvider {
			mark = "*"
		}
		key := "missing"
		if strings.TrimSpace(p.APIKey) != "" {
			key = "set"
		}
		fmt.Fprintf(&b, "%s %-12s type=%-6s key=%-7s model=%s\n    base_url=%s\n",
			mark, slug, p.Type, key, orDash(p.Model), orDash(p.BaseURL))
	}
	return strings.TrimRight(b.String(), "\n")
}

// orDash hiển thị giá trị trống thành "-" để bảng không bị mất cột.
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
