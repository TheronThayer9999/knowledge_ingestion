package usecase

import (
	"context"
	"errors"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
)

// ProviderCatalog là NGUỒN CẤU HÌNH provider LLM duy nhất, giữ đúng một luật:
// configs/config.json là mặc định, bảng llm_provider_settings (admin sửa trong
// popup Nhà cung cấp AI — Đợt 28 nấc 3) thắng từng field. Trước khi gom lại đây,
// luật đó nằm rời rạc ở loader (boot), worker (compile), llmOptionsService (đọc)
// và llmSettingsService (ghi), mỗi chỗ tự đọc bảng + tự merge nên rất dễ lệch.
//
// Struct không có trạng thái sống lâu hơn 1 request: repo là nguồn, đọc mỗi lần
// cần nên admin sửa là các lượt sau thấy ngay.
type ProviderCatalog struct {
	llms   config.LLMsConfig
	repo   domain.ILLMProviderSettingRepository
	secret string
}

// NewProviderCatalog là constructor fx gọi — cfg và repo đã có sẵn trong graph.
func NewProviderCatalog(cfg config.IConfig, repo domain.ILLMProviderSettingRepository) *ProviderCatalog {
	return newProviderCatalog(cfg.GetLLMs(), repo, cfg.GetJWT().Secret)
}

// newProviderCatalog bản không cần config.IConfig: test dựng catalog trực tiếp
// từ llms + fake repo + secret.
func newProviderCatalog(llms config.LLMsConfig, repo domain.ILLMProviderSettingRepository, secret string) *ProviderCatalog {
	return &ProviderCatalog{llms: llms, repo: repo, secret: secret}
}

// ConfiguredDefault là slug mặc định KHAI TRONG FILE (có thể rỗng). Để phân biệt
// "file nói thế" với "registry đang thật sự mặc định cái gì" (ProviderRegistry).
func (c *ProviderCatalog) ConfiguredDefault() string { return c.llms.DefaultProvider }

// Base trả entry config của slug (chuẩn hoá hoa/thường) và cho biết nó có ở file
// không — provider chỉ có trong DB trả struct rỗng, DB là nguồn duy nhất.
func (c *ProviderCatalog) Base(slug string) (config.LLMProviderConfig, bool) {
	return providerBase(c.llms, slug)
}

// Setting đọc dòng cài đặt của một provider. (nil, nil) là trạng thái bình
// thường: provider mới, admin chưa nhập gì (ErrNotFound không phải lỗi ở đây).
func (c *ProviderCatalog) Setting(ctx context.Context, slug string) (*domain.LLMProviderSetting, error) {
	row, err := c.repo.Get(ctx, slug)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

// Settings đọc cả bảng, khoá theo slug đã chuẩn hoá. Lỗi thì WARN rồi coi như
// rỗng — bảng cài đặt là lớp phủ, không được làm chết menu model lẫn chat.
func (c *ProviderCatalog) Settings(ctx context.Context) map[string]*domain.LLMProviderSetting {
	rows, err := c.repo.List(ctx)
	if err != nil {
		logs.Warnw("llm: không đọc được cài đặt provider, dùng config", "error", err)
		return nil
	}
	out := make(map[string]*domain.LLMProviderSetting, len(rows))
	for _, row := range rows {
		out[slugKey(row.Slug)] = row
	}
	return out
}

// Save ghi dòng cài đặt (mã hoá key đã nằm ở caller trước khi gọi đây).
func (c *ProviderCatalog) Save(ctx context.Context, setting *domain.LLMProviderSetting) error {
	return c.repo.Save(ctx, setting)
}

// Delete bỏ dòng cài đặt — xoá HARD (repository dùng Unscoped) để lần tạo lại
// cùng slug không đụng uniqueIndex.
func (c *ProviderCatalog) Delete(ctx context.Context, slug string) error {
	return c.repo.Delete(ctx, slug)
}

// Effective là cấu hình THẬT dùng để dựng client: định nghĩa + model + key của
// DB (nếu có) đè lên config, kèm fallback base_url cho ollama.
func (c *ProviderCatalog) Effective(base config.LLMProviderConfig, setting *domain.LLMProviderSetting) config.LLMProviderConfig {
	return MergeProviderSetting(base, setting, c.secret)
}

// Seal mã hoá api key admin nhập bằng khoá JWT — đường DUY NHẤT một key đi vào
// DB. Secret ở lại trong catalog nên không service nào phải giữ nó hộ.
func (c *ProviderCatalog) Seal(key string) (string, error) {
	return utils.SealString(key, c.secret)
}

// Slugs là tập hợp (đã chuẩn hoá, A→Z) slug trong config và slug có dòng cài đặt.
// Map config không có thứ tự nên phải sort để boot lẫn output API tất định.
func (c *ProviderCatalog) Slugs(settings map[string]*domain.LLMProviderSetting) []string {
	return providerSlugs(c.llms, settings)
}

// Resolved trả MỌI provider hiệu lực — config ∪ DB, bỏ provider admin đang tắt.
// Loader lúc boot và worker compile dùng nó để không tự viết lại luật gộp.
func (c *ProviderCatalog) Resolved(ctx context.Context) []ResolvedProvider {
	return ProviderConfigs(c.llms, c.Settings(ctx), c.secret)
}

// View chuẩn hoá slug và gom những gì một thao tác cần biết về MỘT provider:
// entry config, dòng cài đặt (nil nếu chưa có), nguồn. Slug sai khuôn thì lỗi
// 400 đọc được. Không kiểm tra "provider có tồn tại ở đâu không" — xem Require.
func (c *ProviderCatalog) View(ctx context.Context, rawSlug string) (*ProviderView, error) {
	slug, err := providerSlug(rawSlug)
	if err != nil {
		return nil, err
	}
	base, fromConfig := c.Base(slug)
	setting, err := c.Setting(ctx, slug)
	if err != nil {
		return nil, err
	}
	return &ProviderView{catalog: c, Slug: slug, Base: base, Setting: setting, FromConfig: fromConfig}, nil
}

// Require là cửa vào của MỌI thao tác ghi theo slug: slug phải có mặt ở config
// hoặc đã có dòng cài đặt trong DB (provider admin tạo từ UI), không thì 404.
// Trước đây hàm này là lookupProvider (chỉ nhìn config) nên provider DB-only
// 404 — đó chính là chỗ chặn "cấu hình provider từ UI".
func (c *ProviderCatalog) Require(ctx context.Context, rawSlug string) (*ProviderView, error) {
	view, err := c.View(ctx, rawSlug)
	if err != nil {
		return nil, err
	}
	if view.Setting == nil && !view.FromConfig {
		return nil, unknownProvider(rawSlug)
	}
	return view, nil
}

// ProviderView là một provider nhìn từ phía service — biết catalog của mình nên
// caller không phải truyền lại secret hay config mỗi lần merge.
type ProviderView struct {
	catalog    *ProviderCatalog
	Slug       string
	Base       config.LLMProviderConfig
	Setting    *domain.LLMProviderSetting
	FromConfig bool
}

// NewSetting dựng dòng cài đặt trống khi provider chưa từng được lưu, để thao tác
// "sửa một field" chỉ việc ghi field của mình. Cột models NOT NULL default '[]'
// nên khởi tạo "[]" chứ để rỗng.
func (v *ProviderView) NewSetting() *domain.LLMProviderSetting {
	if v.Setting == nil {
		v.Setting = &domain.LLMProviderSetting{Slug: v.Slug, Models: "[]"}
	}
	return v.Setting
}

// Effective là cấu hình hiệu lực của chính provider này (config ∪ DB).
func (v *ProviderView) Effective() config.LLMProviderConfig {
	return v.catalog.Effective(v.Base, v.Setting)
}
