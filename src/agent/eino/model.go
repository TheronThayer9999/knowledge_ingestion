package eino

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	ollamamodel "github.com/cloudwego/eino-ext/components/model/ollama"
	openaiprovider "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
)

// Provider name request chọn qua field `provider` của ChatRequest — slug lấy
// từ key map llms.providers trong config, thêm backend mới chỉ cần khai báo
// config (type "ollama" hoặc "openai"), không sửa usecase hay prompt file.
// ProviderOllama chỉ còn là default slug khi config thiếu default_provider.
const (
	ProviderOllama = "ollama"
)

// NewChatModel dựng ChatModel Eino trên Ollama bằng lib eino-ext — protocol
// /api/chat, tool-calling, message mapping, callbacks đều do framework lo.
// Wiring giữ lại vì là đặc thù triển khai: cắt hậu tố /api* khỏi base_url
// (vận hành hay copy URL dính hậu tố), Bearer key cho Ollama Cloud qua
// HTTPClient, default temperature/max_tokens từ config. Model cụ thể KHÔNG
// kẹt ở constructor — theo từng request qua
// adk.WithChatModelOptions(model.WithModel(...)) lúc agent Run. Trả interface
// ToolCallingChatModel để loader đổi backend mà không sửa agent, classifier
// hay usecase.
//
// thinking bật công tắc `think` của Ollama (model suy nghĩ trước khi đáp). Flag
// này lib chỉ nhận LÚC DỰNG client — không có call option per-request — nên
// request bật thinking phải dùng một client riêng dựng sẵn lúc startup (xem
// ProviderAgents.ThinkingAgents). false thì payload y hệt trước (không gửi
// field, server giữ hành vi mặc định của model).
func NewChatModel(cfg config.LLMProviderConfig, thinking bool) (einomodel.ToolCallingChatModel, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("eino ollama: base_url is required")
	}
	timeout := cfg.TimeoutSec
	if timeout <= 0 {
		timeout = 120
	}
	conf := &ollamamodel.ChatModelConfig{
		BaseURL: trimAPIURL(cfg.BaseURL),
		HTTPClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
			// Ollama cloud (gemma4) cũng dính cap ~30 req/phút như Groq —
			// rate_limit_rpm dùng chung cho mọi type. loggingTransport bọc
			// ngoài cùng để mọi request đều có dòng log đối chiếu.
			Transport: newLoggingTransport(ProviderOllama, cfg.Model,
				newRateLimitTransport(cfg.RateLimitRPM, authTransport(cfg.APIKey))),
		},
		Model: cfg.Model,
		Options: &ollamamodel.Options{
			Temperature: float32(cfg.Temperature),
			// TopP 0 thì để nguyên — json tag omitempty của api.Options tự
			// bỏ, không gửi 0 (provider tự quyết).
			TopP:       float32(cfg.TopP),
			NumPredict: cfg.MaxTokens,
		},
	}
	if thinking {
		// Con trỏ mới phân được "yêu cầu không nghĩ" (false) với "không nói gì"
		// (nil) — bản thường để nil đúng như hành vi cũ.
		conf.Thinking = &ollamamodel.ThinkValue{Value: true}
	}
	return ollamamodel.NewChatModel(context.Background(), conf)
}

// ProviderAgents là registry agent chat theo backend LLM — request chọn qua
// field `provider` (mặc định theo cấu hình). Mỗi provider 1 ChatAgent riêng
// (cùng bộ tool, client khác nhau) dựng 1 lần lúc startup qua
// BuildProviderAgents; thêm backend mới không đụng tới struct này.
type ProviderAgents struct {
	Agents map[string]*ChatAgent
	// ThinkingAgents là bản agent cùng provider nhưng client đã bật công tắc
	// suy nghĩ của model — request có `thinking: true` đi đường này. Provider
	// không có công tắc riêng thì trỏ đúng vào agent ở Agents (cờ thành "không
	// có tác dụng", không phải lỗi).
	ThinkingAgents map[string]*ChatAgent
	// Models là client thô theo provider — classifier vòng 1 dùng chung client
	// mặc định, khỏi tốn thêm lượt dựng.
	Models map[string]einomodel.ToolCallingChatModel
	// DefaultProvider là provider khi request không ép (lấy từ llm.provider).
	DefaultProvider string
	// DefaultModel là model mặc định theo provider khi request không ép model.
	DefaultModel map[string]string
}

// EffectiveProvider chuẩn hoá slug provider cho 1 request: lowercase/trim,
// rỗng thì provider mặc định của registry. Chat dùng nó để ghi response/lưu
// phiên đúng provider THỰC ĐÃ CHẠY, không phải chuỗi client gửi (có thể rỗng).
func (p *ProviderAgents) EffectiveProvider(provider string) string {
	name := strings.ToLower(strings.TrimSpace(provider))
	if name == "" && p != nil {
		name = p.DefaultProvider
	}
	return name
}

// Resolve chọn agent + model cho 1 request: provider rỗng thì mặc
// định, model rỗng thì model mặc định của provider đó, thinking bật thì lấy
// bản agent đã bật suy nghĩ (nếu provider đó có). Provider chưa cấu
// hình (vd openrouter thiếu key) thì lỗi rõ để caller map 400, không 500.
func (p *ProviderAgents) Resolve(provider, model string, thinking bool) (*ChatAgent, string, error) {
	if p == nil {
		return nil, "", fmt.Errorf("eino agent: chưa được dựng")
	}
	name := p.EffectiveProvider(provider)
	agent, ok := p.Agents[name]
	if !ok {
		return nil, "", fmt.Errorf("eino agent: provider %q chưa cấu hình (mới có: %s)", name, availableProviders(p.Agents))
	}
	if thinking {
		if deep, has := p.ThinkingAgents[name]; has {
			agent = deep
		}
	}
	if strings.TrimSpace(model) == "" {
		model = p.DefaultModel[name]
	}
	if strings.TrimSpace(model) == "" {
		return nil, "", fmt.Errorf("eino agent: provider %q chưa có model mặc định, request phải ép model", name)
	}
	return agent, model, nil
}

// loggingTransport ghi 1 dòng log cho MỖI request HTTPS thật ra khỏi process
// — đây là chốt duy nhất chứng minh "dashboard provider thấy request" khớp
// với "log mình thấy gì". site đọc từ ctx (utils.WithLLMSite) nên biết request
// thuộc phase nào: compile của worker, chat (N vòng tool trong 1 lượt) hay
// classify — thiếu site thì in "không rõ" để lộ chỗ gọi chưa gắn nhãn.
//
// Bọc NGOÀI retryTransport/rateLimitTransport: 1 dòng = 1 lời gọi logic, thời
// gian và status là kết quả cuối (các lượt thử lại đã có warn riêng).
type loggingTransport struct {
	provider string
	model    string
	base     http.RoundTripper
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	site := utils.LLMSiteFromCtx(ctx)
	start := time.Now()
	logs.Infow("llm: request", "site", site, "provider", t.provider, "model", t.model,
		"host", req.URL.Host, "path", req.URL.Path, "trace_id", utils.TraceIDFromCtx(ctx))
	resp, err := t.base.RoundTrip(req)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		logs.Error(err, "llm: request lỗi", "site", site, "provider", t.provider,
			"model", t.model, "host", req.URL.Host, "elapsed_ms", elapsed,
			"trace_id", utils.TraceIDFromCtx(ctx))
		return nil, err
	}
	logs.Infow("llm: request xong", "site", site, "provider", t.provider, "model", t.model,
		"status", resp.StatusCode, "elapsed_ms", elapsed, "trace_id", utils.TraceIDFromCtx(ctx))
	return resp, nil
}

// newLoggingTransport bọc base (rỗng thì http.DefaultTransport) — caller không
// cần nhánh if, giống newRateLimitTransport.
func newLoggingTransport(provider, model string, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &loggingTransport{provider: provider, model: model, base: base}
}

// retryTransport bọc Transport khác, retry khi gặp 429/5xx — model free
// (OpenRouter shared pool) hay trả 429 theo cửa sổ phút; retry vài lần với
// backoff là qua, khỏi fail cả lượt chat. Tôn trọng Retry-After của server
// (trần 60s). Body request được đệm để gửi lại được. Lỗi khác và hết lượt
// thì trả về như cũ để caller map 500/400. name chỉ dùng cho log (slug
// provider trong config).
type retryTransport struct {
	name       string
	base       http.RoundTripper
	maxRetries int
	baseDelay  time.Duration
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	maxRetries := t.maxRetries
	if maxRetries <= 0 {
		maxRetries = 4
	}
	delay := t.baseDelay
	if delay <= 0 {
		delay = 2 * time.Second
	}
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
	}
	for attempt := 0; ; attempt++ {
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.ContentLength = int64(len(body))
		}
		resp, err := base.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return resp, nil
		}
		if attempt >= maxRetries {
			return resp, nil
		}
		wait := delay << attempt
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, convErr := strconv.Atoi(strings.TrimSpace(ra)); convErr == nil && secs > 0 {
				wait = time.Duration(secs) * time.Second
			}
		}
		if wait > 60*time.Second {
			wait = 60 * time.Second
		}
		_ = resp.Body.Close()
		logs.Warnw("llm: dính giới hạn, thử lại", "provider", t.name, "attempt", attempt+1, "status", resp.StatusCode, "wait_ms", wait.Milliseconds())
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}
}

// rateLimitTransport là token-bucket giữ nhịp request/phút trước khi gửi đi
// backend — Groq gói free chỉ ~30 req/phút, vượt là 429 (dù retryTransport
// có tự thử lại thì để server chối rồi chờ vẫn phí lượt). Bucket đầy = rpm
// token, mỗi RoundTrip tốn 1 token, hết thì block đợi token mới (ceil(60/rpm)
// giây/token) — san đều thay vì dồn dập. Đặt LÀM BASE của retryTransport để
// mọi lượt thử lại cũng giữ nhịp. rpm <= 0 thì không bọc (trả về base).
type rateLimitTransport struct {
	rpm      float64
	base     http.RoundTripper
	mu       sync.Mutex
	tokens   float64
	lastFill time.Time
}

// newRateLimitTransport bọc base bằng rate limiter khi rpm > 0, ngược lại
// trả base nguyên bản — caller không cần nhánh if.
func newRateLimitTransport(rpm int, base http.RoundTripper) http.RoundTripper {
	if rpm <= 0 {
		return base
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &rateLimitTransport{
		rpm:      float64(rpm),
		base:     base,
		tokens:   float64(rpm), // bắt đầu đầy bucket — burst 1 lượt đầu bình thường
		lastFill: time.Now(),
	}
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Tính lại số token theo thời gian trôi (refill tuyến tính rpm/60 giây)
	// rồi lấy 1 token; hết thì ngủ theo nhịp 1 token mới. ctx bị hủy (request
	// client đóng) thì thoát ngay, không treo vô hạn.
	for {
		t.mu.Lock()
		now := time.Now()
		t.tokens = math.Min(t.rpm, t.tokens+now.Sub(t.lastFill).Seconds()*t.rpm/60)
		t.lastFill = now
		if t.tokens >= 1 {
			t.tokens--
			t.mu.Unlock()
			break
		}
		wait := time.Duration((1 - t.tokens) * 60 / t.rpm * float64(time.Second))
		t.mu.Unlock()
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}
	return t.base.RoundTrip(req)
}

// NewOpenAIChatModel dựng ChatModel Eino trên backend OpenAI-compatible
// /chat/completions (OpenRouter, Groq, OpenAI, DeepSeek, ...) bằng lib
// openai của eino-ext — cùng interface ToolCallingChatModel với Ollama nên
// agent/tool/history dùng chung, chỉ khác backend. slug là tên provider trong
// config (log + thông báo lỗi). Key rỗng thì lỗi ngay ở đây để loader bỏ qua
// provider này (boot vẫn lên, request nào chọn slug đó mới báo chưa cấu
// hình). base_url bắt buộc — không có default chung cho mọi backend
// OpenAI-compatible; Temperature/TopP/MaxTokens lấy từ config (TopP 0 là
// không gửi, provider tự quyết).
func NewOpenAIChatModel(slug string, cfg config.LLMProviderConfig) (einomodel.ToolCallingChatModel, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("eino %s: api_key is required", slug)
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("eino %s: base_url is required", slug)
	}
	timeout := cfg.TimeoutSec
	if timeout <= 0 {
		timeout = 120
	}
	temp := float32(cfg.Temperature)
	maxTokens := cfg.MaxTokens
	out := &openaiprovider.ChatModelConfig{
		APIKey:  strings.TrimSpace(cfg.APIKey),
		BaseURL: base,
		Model:   cfg.Model,
		// pointer.temperature luôn gửi cả 0 — RAG cần temp 0 (ít bịa) chứ
		// không phải "provider tự quyết như 1.0".
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		HTTPClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
			// retry bọc ngoài, rate limit làm base — mọi lượt thử lại cũng
			// qua limiter, không vượt cap khi server đang chói 429.
			// loggingTransport bọc ngoài cùng: 1 dòng log = 1 lời gọi logic
			// (các lượt thử lại đã có warn riêng của retryTransport).
			Transport: newLoggingTransport(slug, cfg.Model, &retryTransport{
				name:       slug,
				base:       newRateLimitTransport(cfg.RateLimitRPM, http.DefaultTransport),
				maxRetries: 4, baseDelay: 2 * time.Second,
			}),
		},
	}
	if cfg.TopP > 0 {
		topP := float32(cfg.TopP)
		out.TopP = &topP
	}
	return openaiprovider.NewChatModel(context.Background(), out)
}

// trimAPIURL cắt hậu tố API khỏi base_url đã cấu hình — lib framework parse
// URL nguyên văn nên phải dọn trước, không thì request thành /api/api/chat.
func trimAPIURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	for _, suffix := range []string{"/api/chat", "/api/tags", "/api/embed", "/api"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return base
}

// authTransport gắn Bearer token cho Ollama Cloud — lib chính thức/ext không
// có khái niệm API key nên key đi theo Transport; local không key thì transport
// nguyên bản.
func authTransport(apiKey string) http.RoundTripper {
	if strings.TrimSpace(apiKey) == "" {
		return http.DefaultTransport
	}
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.Header.Set("Authorization", "Bearer "+apiKey)
		return http.DefaultTransport.RoundTrip(req)
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
