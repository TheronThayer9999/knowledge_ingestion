package usecase

import (
	"context"
	"fmt"
	"strings"

	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// llmSettingsService là phần GHI của màn hình Nhà cung cấp AI (Đợt 28 nấc 2 + 3):
// lưu key (mã hoá), xoá key, tạo/sửa/xoá ĐỊNH NGHĨA provider, tải danh sách model
// từ chính provider và chọn model mặc định. Mọi thay đổi ghi DB rồi cập nhật
// registry đang chạy — không cần restart app.
//
// Ba collaborators, mỗi cái một vai: catalog (đọc/ghi cấu hình config ∪ DB, gồm
// cả secret mã hoá), registry agent (phần hiệu lực lúc chạy) và fetcher (cổng HTTP
// ra provider). Key chỉ đi MỘT chiều: nhận vào, mã hoá, lưu; không có đường đọc
// ngược ra client (kể cả admin) — UI chỉ thấy has_key/key_source.
type llmSettingsService struct {
	catalog *ProviderCatalog
	agents  IProviderRegistryAdmin
	fetcher *modelFetcher
}

// NewLLMSettingsService là constructor fx gọi.
func NewLLMSettingsService(catalog *ProviderCatalog, agents IProviderRegistryAdmin,
	fetcher *modelFetcher) port.ILLMSettings {
	return newLLMSettingsService(catalog, agents, fetcher)
}

// newLLMSettingsService bản trả concrete để test dựng service trực tiếp với fake.
func newLLMSettingsService(catalog *ProviderCatalog, agents IProviderRegistryAdmin,
	fetcher *modelFetcher) *llmSettingsService {
	return &llmSettingsService{catalog: catalog, agents: agents, fetcher: fetcher}
}

// SaveKey lưu key admin nhập (mã hoá) rồi dựng lại client của provider với key
// mới — key nhúng vào transport lúc NewModel nên phải rebuild mới có hiệu lực.
func (s *llmSettingsService) SaveKey(ctx context.Context, slug, apiKey string) dtos.Result[*dtos.LLMProviderOption] {
	view, err := s.catalog.Require(ctx, slug)
	if err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return dtos.Fail[*dtos.LLMProviderOption](badLLMRequest("api key không được rỗng"))
	}
	cipher, err := s.catalog.Seal(key)
	if err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	view.NewSetting().APIKeyCipher = cipher
	return s.saveAndApply(ctx, view)
}

// ClearKey rỗng hoá key đã lưu để provider quay về key trong config — không xoá
// dòng, nhờ vậy danh sách model đã tải và model đã chọn vẫn còn.
func (s *llmSettingsService) ClearKey(ctx context.Context, slug string) dtos.Result[*dtos.LLMProviderOption] {
	view, err := s.catalog.Require(ctx, slug)
	if err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	view.NewSetting().APIKeyCipher = ""
	return s.saveAndApply(ctx, view)
}

// SelectModel đặt model mặc định của provider. Không rebuild: model đi theo
// từng request (model.WithModel), chỉ cần cập nhật DefaultModel để lượt chat
// bỏ trống model dùng đúng model mới. Nhờ vậy chọn model được cả khi provider
// chưa có key (lúc đó nó chưa available, có key là chạy).
func (s *llmSettingsService) SelectModel(ctx context.Context, slug, model string) dtos.Result[*dtos.LLMProviderOption] {
	view, err := s.catalog.Require(ctx, slug)
	if err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	chosen := strings.TrimSpace(model)
	if chosen == "" {
		return dtos.Fail[*dtos.LLMProviderOption](badLLMRequest("model không được rỗng"))
	}
	view.NewSetting().Model = chosen
	if err := s.catalog.Save(ctx, view.Setting); err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	s.agents.SetDefaultModel(view.Slug, chosen)
	return dtos.Ok(s.option(view))
}

// LoadModels hỏi chính provider danh sách model (OpenAI-compatible: GET
// /models; Ollama: GET /api/tags), lưu lại rồi trả cho popup. Lần mở sau chỉ
// đọc DB, không gọi provider.
func (s *llmSettingsService) LoadModels(ctx context.Context, slug string) dtos.Result[*dtos.LLMModelsResponse] {
	view, err := s.catalog.Require(ctx, slug)
	if err != nil {
		return dtos.Fail[*dtos.LLMModelsResponse](err)
	}
	models, err := s.fetcher.Fetch(ctx, view.Effective())
	if err != nil {
		return dtos.Fail[*dtos.LLMModelsResponse](s.fetcher.WrapError(view.Slug, err))
	}
	setting := view.NewSetting()
	setting.Models = encodeModelList(cleanModels(models))
	if err := s.catalog.Save(ctx, setting); err != nil {
		return dtos.Fail[*dtos.LLMModelsResponse](err)
	}
	return dtos.Ok(&dtos.LLMModelsResponse{Slug: view.Slug, Models: models})
}

// saveAndApply ghi DB rồi cập nhật registry. Provider bị admin tắt thì tháo agent
// thay vì dựng (lưu key trên provider đang tắt là hợp lệ — key chờ tới lúc bật
// lại). Rebuild lỗi (base_url hỏng, key sai...) thì trả 400 đọc được — lượt chat
// cũ vẫn giữ agent đang chạy.
func (s *llmSettingsService) saveAndApply(ctx context.Context, view *ProviderView) dtos.Result[*dtos.LLMProviderOption] {
	if err := s.catalog.Save(ctx, view.Setting); err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	if err := s.applyToRegistry(view); err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	return dtos.Ok(s.option(view))
}

// applyToRegistry là bước duy nhất chạm registry: bật thì Rebuild, tắt thì
// Remove. Trả nil khi mọi việc ổn.
func (s *llmSettingsService) applyToRegistry(view *ProviderView) error {
	if view.Setting != nil && view.Setting.Disabled {
		s.agents.Remove(view.Slug)
		return nil
	}
	if err := s.agents.Rebuild(view.Slug, view.Effective()); err != nil {
		logs.Error(err, "llm: dựng lại provider lỗi", "provider", view.Slug)
		return badLLMRequest(fmt.Sprintf(
			"không dựng được kết nối %s — kiểm tra base_url/api key rồi thử lại", view.Slug))
	}
	return nil
}

// listForAdmin dựng lại bảng provider sau thao tác thay đổi DANH SÁCH (thêm/xoá)
// — FE nhận nguyên mảng mới, khỏi gọi lại /llm/options. Admin=true vì mọi
// endpoint gọi tới đây đều đã qua RequireRole.
func (s *llmSettingsService) listForAdmin(ctx context.Context) *dtos.LLMOptionsResponse {
	return buildLLMOptions(s.catalog, s.agents, s.catalog.Settings(ctx), domain.RoleAdmin)
}

// option trả đúng dòng UI admin cần cập nhật sau thao tác — khỏi gọi lại
// /llm/options. Admin=true vì mọi endpoint gọi tới đây đều đã qua RequireRole.
func (s *llmSettingsService) option(view *ProviderView) *dtos.LLMProviderOption {
	opt := buildProviderOption(view.Slug, view.Base, view.Setting, s.agents, s.defaultSlug(), true)
	opt.FromConfig = view.FromConfig
	return opt
}

// defaultSlug là provider mà chat sẽ dùng khi request không ép: file nói gì thì
// theo đó, thiếu thì registry tự trả Ollama mặc định.
func (s *llmSettingsService) defaultSlug() string {
	return effectiveProviderSlug(s.catalog.ConfiguredDefault(), s.agents)
}
