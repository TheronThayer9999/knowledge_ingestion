package usecase

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"syscall"
	"time"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
)

const (
	// llmModelsTimeout chặn provider treo: "Tải model" là thao tác admin bấm
	// chờ, 20s vẫn đủ cho danh sách vài trăm model của Ollama.
	llmModelsTimeout = 20 * time.Second
	// llmModelsBodyMax trần body đọc vào — danh sách model Ollama có thể dài
	// vài MB, nhưng không đọc vô hạn từ một endpoint ngoài tầm kiểm soát.
	llmModelsBodyMax = 8 << 20
	// llmProviderErrorClip cắt body lỗi của provider trước khi hiện cho admin.
	llmProviderErrorClip = 200
)

// llmFetchError là lỗi AN TOÀN để hiện cho admin (đã lọc bỏ URL/header).
type llmFetchError struct {
	msg string
	// status là HTTP status của provider (0 = không phải lỗi HTTP: transport
	// hoặc body không đọc được). Đường dò model dùng nó để biết có nên thử
	// URL ứng viên kế tiếp không.
	status int
}

func (e *llmFetchError) Error() string { return e.msg }

func newProviderError(format string, args ...any) error {
	return &llmFetchError{msg: fmt.Sprintf(format, args...)}
}

// newProviderHTTPError là lỗi có status — để đường dò model phân biệt "URL
// này không có endpoint" (404/405, thử URL khác) với lỗi thật của provider
// (401 sai key, 5xx hỏng).
func newProviderHTTPError(status int, format string, args ...any) error {
	return &llmFetchError{msg: fmt.Sprintf(format, args...), status: status}
}

// modelFetcher là bước "hỏi chính provider nó có model nào" của popup Nhà cung
// cấp AI (Đợt 28 nấc 2): HTTP ra ngoài, parsing 2 khuôn API, phân loại lỗi
// transport. Tách khỏi llmSettingsService vì nghiệp vụ của service là ghi cấu
// hình + cập nhật registry, còn đây là một cổng ra thế giới — test service khỏi
// phải dựng http.Client và test fetcher khỏi phải dựng registry.
type modelFetcher struct {
	client *http.Client
}

// NewLLMModelFetcher là constructor fx gọi — timeout chặn provider treo.
func NewLLMModelFetcher() *modelFetcher {
	return newLLMModelFetcher(&http.Client{Timeout: llmModelsTimeout})
}

// newLLMModelFetcher bản nhận client để test trỏ vào httptest.Server.
func newLLMModelFetcher(client *http.Client) *modelFetcher {
	return &modelFetcher{client: client}
}

// ModelList là kết quả dò model: danh sách model + base URL ĐÃ dùng để dò.
// Base URL có thể khác base người dùng gõ (vd họ dán thiếu "/v1") — caller
// lưu lại để client chat dựng đúng base đó, không đoán.
type ModelList struct {
	Models  []string
	BaseURL string
}

// Fetch gọi endpoint danh sách model của provider. Key đi qua header
// Authorization; lỗi transport đi qua transportError nên lời admin nhận được chỉ
// là lý do đã lọc, không bao giờ là error string của net/http (có host).
//
// Thử nhiều URL ứng viên (xem modelsEndpoints) và trả về base của ứng viên
// trúng: người dùng hay dán URL thiếu "/v1", mà đường dẫn đúng chỉ biết được
// khi hỏi provider.
func (f *modelFetcher) Fetch(ctx context.Context, pcfg config.LLMProviderConfig) (*ModelList, error) {
	endpoints, err := modelsEndpoints(pcfg)
	if err != nil {
		return nil, err
	}
	var last error
	for _, ep := range endpoints {
		models, err := f.fetchOne(ctx, ep, pcfg)
		if err == nil {
			return &ModelList{Models: models, BaseURL: ep.base}, nil
		}
		if !isMissingEndpoint(err) {
			return nil, err
		}
		last = err
	}
	return nil, last
}

// fetchOne gọi MỘT URL dò model rồi parse khuôn trả về.
func (f *modelFetcher) fetchOne(ctx context.Context, ep modelsEndpoint,
	pcfg config.LLMProviderConfig) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if key := strings.TrimSpace(pcfg.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, f.transportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, llmModelsBodyMax))
	if err != nil {
		return nil, f.transportError(ctx, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, providerStatusError(pcfg.Type, resp.StatusCode, body)
	}
	return parseModelList(pcfg.Type, body)
}

// WrapError đổi lỗi tải model thành lỗi đọc được cho admin: llmFetchError đã lọc
// (không URL/key) thì 502 kèm lý do; lỗi khác để Render xử lý (ctx chết →
// 499/504, còn lại 500 + log) — không lộ địa chỉ nội bộ ra client.
func (f *modelFetcher) WrapError(name string, err error) error {
	var pe *llmFetchError
	if stderrors.As(err, &pe) {
		return errors.NewCustomHttpError(http.StatusBadGateway, http.StatusBadGateway,
			fmt.Sprintf("không tải được model của %s: %s", name, pe.msg))
	}
	logs.Warnw("llm: tải model lỗi", "provider", name, "error", err)
	return err
}

// transportError đổi lỗi "không tới được provider" (tên miền sai, cổng bị từ
// chối, treo quá 20 giây) thành llmFetchError đã lọc — WrapError bọc thành 502
// kèm lý do thay vì để lọt 500 "internal server error" vô nghĩa, trong khi log
// vẫn giữ nguyên lỗi gốc. Message không chứa host/URL: base_url là nội bộ của
// server, không có việc gì phải lộ ra client. Ngoại lệ duy nhất là địa chỉ bị
// CHẶN — đó là base URL chính người dùng gõ, nói lại giúp họ tự sửa.
//
// ctx chết giữa lúc chờ (admin đóng popup, trình duyệt huỷ request) thì trả
// nguyên văn: đó là phía người dùng, không phải provider hỏng — bọc vào
// llmFetchError sẽ báo nhầm "nhà cung cấp không trả lời".
func (f *modelFetcher) transportError(ctx context.Context, err error) error {
	if ctx.Err() != nil || stderrors.Is(err, context.Canceled) ||
		stderrors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return newProviderError("%s", transportReason(err))
}

// transportReason chọn câu mô tả theo LOẠI lỗi của stdlib chứ không so khớp
// chuỗi: message gốc ("no such host", "connectex: ...") đổi theo OS và ngôn ngữ
// hệ thống, còn kiểu lỗi thì không. Không nhận diện được thì câu chung — thà
// mơ hồ còn hơn in nguyên văn error string của net/http ra UI.
func transportReason(err error) string {
	var netErr net.Error
	if stderrors.As(err, &netErr) && netErr.Timeout() {
		return "nhà cung cấp không trả lời trong 20 giây"
	}
	var dnsErr *net.DNSError
	if stderrors.As(err, &dnsErr) {
		return "tên miền trong base_url không tồn tại"
	}
	switch {
	case stderrors.Is(err, syscall.ECONNREFUSED):
		return "không có dịch vụ nào lắng nghe ở cổng đó"
	case stderrors.Is(err, syscall.EHOSTUNREACH), stderrors.Is(err, syscall.ENETUNREACH):
		return "máy chủ đó không tới được từ máy này"
	}
	return "không kết nối được tới nhà cung cấp"
}

// modelsEndpoint là một ứng viên để dò model: URL cần gọi và base URL ứng
// viên (lưu lại khi trúng, để client chat dùng đúng base đó).
type modelsEndpoint struct {
	url  string
	base string
}

// modelsEndpoints trả các ứng viên dò model theo thứ tự ưu tiên.
//
// OpenAI-compatible: người dùng hay dán URL THIẾU "/v1" (vd
// "https://api.groq.com/openai" trong khi provider thật là
// "https://api.groq.com/openai/v1"), nên thử base trước, rồi base + "/v1",
// rồi base + "/api/v1" (gateway tự host đặt API dưới /api/v1). Base đã có
// đoạn phiên bản (v1, v2, v1beta) thì không thêm nữa — thêm sẽ thành
// /v1/v1. Đường dẫn đúng chỉ biết được khi hỏi provider, không suy ra được
// bằng luật tĩnh (vd Gemini OpenAI-compat là ".../v1beta/openai" — không
// phải ".../openai/v1").
//
// Ollama: một URL duy nhất {base}/api/tags (API native, không phải OpenAI).
func modelsEndpoints(pcfg config.LLMProviderConfig) ([]modelsEndpoint, error) {
	base := eino.NormalizeBaseURL(pcfg.Type, pcfg.BaseURL)
	if base == "" {
		return nil, newProviderError("provider chưa có base_url trong config")
	}
	if pcfg.Type == config.LLMTypeOllama {
		return []modelsEndpoint{{url: base + "/api/tags", base: base}}, nil
	}
	if strings.TrimSpace(pcfg.APIKey) == "" {
		return nil, newProviderError("provider chưa có api key — lưu key trước khi tải model")
	}
	out := []modelsEndpoint{{url: base + "/models", base: base}}
	if !eino.HasVersionPath(base) {
		out = append(out,
			modelsEndpoint{url: base + "/v1/models", base: base + "/v1"},
			modelsEndpoint{url: base + "/api/v1/models", base: base + "/api/v1"},
		)
	}
	return out, nil
}

// isMissingEndpoint báo lỗi này có phải "URL này không có endpoint" để thử ứng
// viên kế tiếp. CHỈ 404/405 mới đáng thử tiếp — 401 (sai key), 403 hay 5xx là
// lỗi thật của provider, đổi URL không giúp gì.
func isMissingEndpoint(err error) bool {
	var pe *llmFetchError
	if !stderrors.As(err, &pe) {
		return false
	}
	return pe.status == http.StatusNotFound || pe.status == http.StatusMethodNotAllowed
}

// providerStatusError dựng lời đọc được cho người dùng từ status của provider.
// 404/405 = không có endpoint ở base URL này (hay do thiếu /v1) — nói rõ để
// user biết sửa base URL; 401/403 = key sai hoặc thiếu quyền; còn lại kèm
// body đã cắt ngắn vì lỗi 500 của provider hay nói điều đáng biết.
func providerStatusError(kind string, status int, body []byte) error {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		if kind == config.LLMTypeOllama {
			return newProviderHTTPError(status,
				"provider không có endpoint /api/tags (HTTP %d) — kiểm tra lại base URL của Ollama", status)
		}
		return newProviderHTTPError(status,
			"provider không có endpoint danh sách model (HTTP %d) — kiểm tra lại base URL, thường phải kết thúc bằng /v1", status)
	case http.StatusUnauthorized:
		// 401 ở endpoint /models KHÔNG chắc là sai key: có gateway
		// xác thực /chat/completions khác /models. Nên nói rõ cả hai khả
		// năng và nhắc nhập model thủ công — không chặn kết nối (các OSS lớn
		// cũng không coi /models hỏng là chết connection).
		return newProviderHTTPError(status,
			"provider trả 401 cho endpoint danh sách model — key có thể sai, hoặc endpoint này dùng kiểu xác thực khác (có thể nhập model thủ công)")
	case http.StatusForbidden:
		return newProviderHTTPError(status,
			"provider không cho truy cập (HTTP 403) — key không có quyền đọc danh sách model (có thể nhập model thủ công)")
	default:
		return newProviderHTTPError(status, "provider trả HTTP %d: %s", status, clipProviderBody(body))
	}
}

// parseModelList đọc 2 khuôn phổ biến: OpenAI {"data":[{"id"}]} và Ollama
// {"models":[{"name"}]}. Sắp A→Z + dedup để popup ổn định giữa các lần tải.
func parseModelList(kind string, body []byte) ([]string, error) {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, newProviderError("provider trả dữ liệu không đọc được")
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(payload.Data)+len(payload.Models))
	appendModel := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	if kind == config.LLMTypeOllama {
		for _, m := range payload.Models {
			appendModel(m.Name)
		}
	} else {
		for _, m := range payload.Data {
			appendModel(m.ID)
		}
	}
	if len(out) == 0 {
		return nil, newProviderError("provider không trả model nào")
	}
	sort.Strings(out)
	return out, nil
}

// clipProviderBody gọn body lỗi của provider thành 1 dòng ngắn — đủ để admin
// biết sai key hay sai model, không đổ cả trang HTML vào UI.
func clipProviderBody(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	runes := []rune(text)
	if len(runes) > llmProviderErrorClip {
		return string(runes[:llmProviderErrorClip]) + "…"
	}
	if text == "" {
		return "(rỗng)"
	}
	return text
}
