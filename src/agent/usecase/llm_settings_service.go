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
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
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

// llmSettingsService là phần GHI của màn hình Nhà cung cấp AI (Đợt 28 nấc 2):
// lưu key (mã hoá), xoá key, tải danh sách model từ chính provider và chọn
// model mặc định. Mọi thay đổi ghi DB rồi cập nhật registry đang chạy — không
// cần restart app.
//
// Key chỉ đi MỘT chiều: nhận vào, mã hoá, lưu; không có đường đọc ngược ra
// client (kể cả admin) — UI chỉ thấy has_key/key_source.
type llmSettingsService struct {
	llms   config.LLMsConfig
	agents *eino.ProviderAgents
	repo   domain.ILLMProviderSettingRepository
	secret string
	client *http.Client
}

func NewLLMSettingsService(cfg config.IConfig, agents *eino.ProviderAgents,
	repo domain.ILLMProviderSettingRepository) port.ILLMSettings {
	return &llmSettingsService{
		llms:   cfg.GetLLMs(),
		agents: agents,
		repo:   repo,
		secret: cfg.GetJWT().Secret,
		client: &http.Client{Timeout: llmModelsTimeout},
	}
}

// SaveKey lưu key admin nhập (mã hoá) rồi dựng lại client của provider với key
// mới — key nhúng vào transport lúc NewModel nên phải rebuild mới có hiệu lực.
func (s *llmSettingsService) SaveKey(ctx context.Context, slug, apiKey string) dtos.Result[*dtos.LLMProviderOption] {
	name, base, setting, err := s.providerOf(ctx, slug)
	if err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return dtos.Fail[*dtos.LLMProviderOption](badLLMRequest("api key không được rỗng"))
	}
	cipher, err := utils.SealString(key, s.secret)
	if err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	setting.APIKeyCipher = cipher
	return s.saveAndApply(ctx, name, base, setting)
}

// ClearKey rỗng hoá key đã lưu để provider quay về key trong config — không xoá
// dòng, nhờ vậy danh sách model đã tải và model đã chọn vẫn còn.
func (s *llmSettingsService) ClearKey(ctx context.Context, slug string) dtos.Result[*dtos.LLMProviderOption] {
	name, base, setting, err := s.providerOf(ctx, slug)
	if err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	setting.APIKeyCipher = ""
	return s.saveAndApply(ctx, name, base, setting)
}

// SelectModel đặt model mặc định của provider. Không rebuild: model đi theo
// từng request (model.WithModel), chỉ cần cập nhật DefaultModel để lượt chat
// bỏ trống model dùng đúng model mới. Nhờ vậy chọn model được cả khi provider
// chưa có key (lúc đó nó chưa available, có key là chạy).
func (s *llmSettingsService) SelectModel(ctx context.Context, slug, model string) dtos.Result[*dtos.LLMProviderOption] {
	name, base, setting, err := s.providerOf(ctx, slug)
	if err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	chosen := strings.TrimSpace(model)
	if chosen == "" {
		return dtos.Fail[*dtos.LLMProviderOption](badLLMRequest("model không được rỗng"))
	}
	setting.Model = chosen
	if err := s.repo.Save(ctx, setting); err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	s.agents.SetDefaultModel(name, chosen)
	return dtos.Ok(s.option(name, base, setting))
}

// LoadModels hỏi chính provider danh sách model (OpenAI-compatible: GET
// /models; Ollama: GET /api/tags), lưu lại rồi trả cho popup. Lần mở sau chỉ
// đọc DB, không gọi provider.
func (s *llmSettingsService) LoadModels(ctx context.Context, slug string) dtos.Result[*dtos.LLMModelsResponse] {
	name, base, setting, err := s.providerOf(ctx, slug)
	if err != nil {
		return dtos.Fail[*dtos.LLMModelsResponse](err)
	}
	models, err := s.fetchModels(ctx, MergeProviderSetting(base, setting, s.secret))
	if err != nil {
		return dtos.Fail[*dtos.LLMModelsResponse](s.fetchError(name, err))
	}
	setting.Models = encodeModelList(cleanModels(models))
	if err := s.repo.Save(ctx, setting); err != nil {
		return dtos.Fail[*dtos.LLMModelsResponse](err)
	}
	return dtos.Ok(&dtos.LLMModelsResponse{Slug: name, Models: models})
}

// providerOf là cửa vào của MỌI thao tác ghi: slug có mặt ở config hoặc đã có
// dòng cài đặt trong DB (provider admin tạo từ UI, Đợt 28 nấc 3) đều đi qua,
// và luôn trả một dòng ghi được để caller chỉ việc đổi đúng field của mình.
// Trước đây hàm này là lookupProvider (chỉ nhìn config) nên provider DB-only
// 404 — đó chính là chỗ chặn "cấu hình provider từ UI".
func (s *llmSettingsService) providerOf(ctx context.Context, slug string) (
	string, config.LLMProviderConfig, *domain.LLMProviderSetting, error) {
	name, err := providerSlug(slug)
	if err != nil {
		return "", config.LLMProviderConfig{}, nil, err
	}
	base, fromConfig := providerBase(s.llms, name)
	setting, err := s.setting(ctx, name)
	if err != nil {
		return "", config.LLMProviderConfig{}, nil, err
	}
	if setting != nil {
		return name, base, setting, nil
	}
	if !fromConfig {
		return "", config.LLMProviderConfig{}, nil, unknownProvider(slug)
	}
	return name, base, &domain.LLMProviderSetting{Slug: name, Models: "[]"}, nil
}

// settingsAll đọc toàn bộ bảng cài đặt theo slug đã chuẩn hoá — dùng khi phải
// trả lại cả danh sách provider sau thao tác thêm/xoá.
func (s *llmSettingsService) settingsAll(ctx context.Context) map[string]*domain.LLMProviderSetting {
	rows, err := s.repo.List(ctx)
	if err != nil {
		logs.Warnw("llm: không đọc được cài đặt provider, trả danh sách theo config", "error", err)
		return nil
	}
	out := make(map[string]*domain.LLMProviderSetting, len(rows))
	for _, row := range rows {
		out[slugKey(row.Slug)] = row
	}
	return out
}

// listForAdmin dựng lại bảng provider sau thao tác thay đổi DANH SÁCH (thêm/xoá)
// — FE nhận nguyên mảng mới, khỏi gọi lại /llm/options. Admin=true vì mọi
// endpoint gọi tới đây đều đã qua RequireRole.
func (s *llmSettingsService) listForAdmin(ctx context.Context) *dtos.LLMOptionsResponse {
	return buildLLMOptions(s.llms, s.agents, s.settingsAll(ctx), domain.RoleAdmin)
}

// setting đọc dòng cài đặt của provider, nil khi chưa có (ErrNotFound là trạng
// thái bình thường: provider mới, admin chưa nhập gì).
func (s *llmSettingsService) setting(ctx context.Context, slug string) (*domain.LLMProviderSetting, error) {
	row, err := s.repo.Get(ctx, slug)
	if stderrors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

// saveAndApply ghi DB rồi merge lại cấu hình và cập nhật registry. Provider bị
// admin tắt thì tháo agent thay vì dựng (lưu key trên provider đang tắt là
// hợp lệ — key chờ tới lúc bật lại). Rebuild lỗi (base_url hỏng, key sai...)
// thì trả 400 đọc được — lượt chat cũ vẫn giữ agent đang chạy.
func (s *llmSettingsService) saveAndApply(ctx context.Context, name string, base config.LLMProviderConfig,
	setting *domain.LLMProviderSetting) dtos.Result[*dtos.LLMProviderOption] {
	if err := s.repo.Save(ctx, setting); err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	if err := s.applyToRegistry(name, base, setting); err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	return dtos.Ok(s.option(name, base, setting))
}

// applyToRegistry là bước duy nhất chạm registry: bật thì Rebuild, tắt thì
// Remove. Trả nil khi mọi việc ổn.
func (s *llmSettingsService) applyToRegistry(name string, base config.LLMProviderConfig,
	setting *domain.LLMProviderSetting) error {
	if setting.Disabled {
		s.agents.Remove(name)
		return nil
	}
	if err := s.agents.Rebuild(name, MergeProviderSetting(base, setting, s.secret)); err != nil {
		logs.Error(err, "llm: dựng lại provider lỗi", "provider", name)
		return badLLMRequest(fmt.Sprintf(
			"không dựng được kết nối %s — kiểm tra base_url/api key rồi thử lại", name))
	}
	return nil
}

// option trả đúng dòng UI admin cần cập nhật sau thao tác — khỏi gọi lại
// /llm/options. Admin=true vì mọi endpoint gọi tới đây đều đã qua RequireRole.
func (s *llmSettingsService) option(name string, base config.LLMProviderConfig,
	setting *domain.LLMProviderSetting) *dtos.LLMProviderOption {
	defaultSlug := effectiveProviderSlug(s.llms.DefaultProvider, s.agents)
	opt := buildProviderOption(name, base, setting, s.agents, defaultSlug, true)
	opt.FromConfig = hasConfigProvider(s.llms, name)
	return opt
}

// fetchError đổi lỗi tải model thành lỗi đọc được cho admin: llmFetchError đã
// lọc (không URL/key) thì 502 kèm lý do; lỗi khác để Render xử lý (ctx chết →
// 499/504, còn lại 500 + log) — không lộ địa chỉ nội bộ ra client.
func (s *llmSettingsService) fetchError(name string, err error) error {
	var pe *llmFetchError
	if stderrors.As(err, &pe) {
		return errors.NewCustomHttpError(http.StatusBadGateway, http.StatusBadGateway,
			fmt.Sprintf("không tải được model của %s: %s", name, pe.msg))
	}
	logs.Warnw("llm: tải model lỗi", "provider", name, "error", err)
	return err
}

// providerTransportError đổi lỗi "không tới được provider" (tên miền sai, cổng
// bị từ chối, treo quá 20 giây) thành llmFetchError đã lọc — fetchError bọc
// thành 502 kèm lý do thay vì để lọt 500 "internal server error" vô nghĩa, trong
// khi log vẫn giữ nguyên lỗi gốc. Message không chứa host/URL: base_url là nội
// bộ của server, không có việc gì phải lộ ra client.
//
// ctx chết giữa lúc chờ (admin đóng popup, trình duyệt huỷ request) thì trả
// nguyên văn: đó là phía người dùng, không phải provider hỏng — bọc vào
// llmFetchError sẽ báo nhầm "nhà cung cấp không trả lời".
func providerTransportError(ctx context.Context, err error) error {
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

// llmFetchError là lỗi AN TOÀN để hiện cho admin (đã lọc bỏ URL/header).
type llmFetchError struct{ msg string }

func (e *llmFetchError) Error() string { return e.msg }

func newProviderError(format string, args ...any) error {
	return &llmFetchError{msg: fmt.Sprintf(format, args...)}
}

// unknownProvider là 404 "slug không có trong config" — admin gõ sai hay FE
// giữ danh sách cũ, không phải lỗi server.
func unknownProvider(slug string) *errors.Error {
	return errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound,
		fmt.Sprintf("provider %q không có trong cấu hình", strings.TrimSpace(slug)))
}

func badLLMRequest(msg string) *errors.Error {
	return errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, msg)
}

// fetchModels gọi endpoint danh sách model của provider. Key đi qua header
// Authorization; lỗi transport đi qua providerTransportError nên lời admin nhận
// được chỉ là lý do đã lọc, không bao giờ là error string của net/http (có host).
func (s *llmSettingsService) fetchModels(ctx context.Context, pcfg config.LLMProviderConfig) ([]string, error) {
	url, err := modelsURL(pcfg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if key := strings.TrimSpace(pcfg.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, providerTransportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, llmModelsBodyMax))
	if err != nil {
		return nil, providerTransportError(ctx, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, newProviderError("provider trả HTTP %d: %s", resp.StatusCode, clipProviderBody(body))
	}
	models, err := parseModelList(pcfg.Type, body)
	if err != nil {
		return nil, err
	}
	return models, nil
}

// modelsURL dựng URL danh sách model theo protocol: OpenAI-compatible có
// /models (bắt buộc key — thiếu thì báo ngay, khỏi gọi rồi nhận 401), Ollama
// dùng /api/tags và chạy được không key với server local.
func modelsURL(pcfg config.LLMProviderConfig) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(pcfg.BaseURL), "/")
	if base == "" {
		return "", newProviderError("provider chưa có base_url trong config")
	}
	if pcfg.Type == config.LLMTypeOllama {
		return eino.TrimAPIURL(base) + "/api/tags", nil
	}
	if strings.TrimSpace(pcfg.APIKey) == "" {
		return "", newProviderError("provider chưa có api key — lưu key trước khi tải model")
	}
	return base + "/models", nil
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
