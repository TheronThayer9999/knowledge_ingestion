package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	apperrors "knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// TestNormalizeProviderType: UI/Admin đọc tài liệu provider dưới tên
// "openai-compatible" nhưng backend chỉ có 2 khuôn client — nhận cả hai cách
// gọi, lưu về giá trị nội bộ; type lạ phải trả "" để caller báo 400.
func TestNormalizeProviderType(t *testing.T) {
	cases := map[string]string{
		"ollama":            config.LLMTypeOllama,
		" Ollama ":          config.LLMTypeOllama,
		"openai":            config.LLMTypeOpenAI,
		"openai-compatible": config.LLMTypeOpenAI,
		"OPENAI_COMPATIBLE": config.LLMTypeOpenAI,
		"openai compatible": config.LLMTypeOpenAI,
		"":                  "",
		"anthropic":         "",
		"azure-openai":      "",
		"ollama-cloud":      "",
		"vLLM":              "",
	}
	for raw, want := range cases {
		if got := NormalizeProviderType(raw); got != want {
			t.Errorf("NormalizeProviderType(%q) = %q, muốn %q", raw, got, want)
		}
	}
}

// TestMergeProviderSettingDefWinsConfig: định nghĩa provider (label/type/
// base_url) trong DB đè entry config — đó là cách admin sửa provider của file
// mà không đụng file. Field bỏ trống thì config thắng.
func TestMergeProviderSettingDefWinsConfig(t *testing.T) {
	base := config.LLMProviderConfig{
		Type: config.LLMTypeOllama, Label: "Local", Model: "gemma4:31b",
		BaseURL: "http://localhost:11434",
	}
	merged := MergeProviderSetting(base, &domain.LLMProviderSetting{
		Slug: "ollama", Label: "Nhà", Type: config.LLMTypeOpenAI, BaseURL: "https://x.example/v1",
	}, "test-secret")
	if merged.Label != "Nhà" || merged.Type != config.LLMTypeOpenAI || merged.BaseURL != "https://x.example/v1" {
		t.Errorf("def của DB phải thắng config, nhận %+v", merged)
	}
	if merged.Model != "gemma4:31b" {
		t.Errorf("model không nằm trong form định nghĩa phải giữ của config, nhận %q", merged.Model)
	}

	// Dòng DB rỗng mọi cột định nghĩa = không đè gì.
	untouched := MergeProviderSetting(base, &domain.LLMProviderSetting{Slug: "ollama"}, "test-secret")
	if untouched.Type != base.Type || untouched.Label != base.Label ||
		untouched.BaseURL != base.BaseURL || untouched.Model != base.Model {
		t.Errorf("setting rỗng làm đổi config: %+v", untouched)
	}
}

// TestMergeProviderSettingOllamaBaseURLFallback: provider ollama UI tạo mới chỉ
// chọn loại mà quên base_url thì fallback về localhost:11434, đúng luật
// config.Load — nếu không, NewChatModel nhận URL rỗng và lỗi rất khó đọc.
func TestMergeProviderSettingOllamaBaseURLFallback(t *testing.T) {
	merged := MergeProviderSetting(config.LLMProviderConfig{}, &domain.LLMProviderSetting{
		Slug: "mine", Type: config.LLMTypeOllama, Model: "llama3.1:8b",
	}, "test-secret")
	if merged.BaseURL != defaultOllamaBaseURL {
		t.Errorf("base_url = %q, muốn %q", merged.BaseURL, defaultOllamaBaseURL)
	}
}

// TestProviderConfigsUnionOrderAndDisabled: danh sách hiệu lực là config ∪ DB
// (provider UI tạo cũng có mặt), sắp A→Z cho boot tất định, và provider bị tắt
// không được dựng agent.
func TestProviderConfigsUnionOrderAndDisabled(t *testing.T) {
	llms, settings := crudFixture(t)
	got := ProviderConfigs(llms, settings, "test-secret")

	var slugs []string
	for _, item := range got {
		slugs = append(slugs, item.Slug)
	}
	if strings.Join(slugs, ",") != "9router,groq,minimax" {
		t.Fatalf("slugs = %v, muốn minimax vào cùng danh sách và bỏ provider tắt", slugs)
	}
	bySlug := make(map[string]ResolvedProvider, len(got))
	for _, item := range got {
		bySlug[item.Slug] = item
	}
	if bySlug["9router"].FromConfig != true || bySlug["minimax"].FromConfig != false {
		t.Errorf("from_config = %v/%v, muốn true cho provider của file, false cho provider UI tạo",
			bySlug["9router"].FromConfig, bySlug["minimax"].FromConfig)
	}
	if bySlug["minimax"].Config.BaseURL != "https://api.minimax.example/v1" {
		t.Errorf("minimax config = %+v", bySlug["minimax"].Config)
	}
}

// crudFixture: 9router + ollama (tắt) trong config, minimax chỉ có ở DB, groq
// là provider của file có model do admin chọn.
func crudFixture(t *testing.T) (config.LLMsConfig, map[string]*domain.LLMProviderSetting) {
	llms := config.LLMsConfig{
		DefaultProvider: "9router",
		Providers: map[string]config.LLMProviderConfig{
			"9Router": {Type: config.LLMTypeOpenAI, Model: "m-a", BaseURL: "http://localhost:20128/v1", APIKey: "k"},
			"ollama":  {Type: config.LLMTypeOllama, Model: "gemma4:31b", BaseURL: "http://localhost:11434"},
		},
	}
	settings := map[string]*domain.LLMProviderSetting{
		"ollama":  {Slug: "ollama", Disabled: true},
		"minimax": {Slug: "minimax", Type: config.LLMTypeOpenAI, BaseURL: "https://api.minimax.example/v1", Model: "MiniMax-M2", APIKeyCipher: sealTestKey(t, "db-key")},
		"groq":    {Slug: "groq", Model: "llama-3.3-70b-versatile"},
	}
	return llms, settings
}

func sealTestKey(t *testing.T, key string) string {
	t.Helper()
	cipher, err := utils.SealString(key, "test-secret")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	return cipher
}

// TestBuildLLMOptionsListsDBOnlyProviderForEveryone: provider UI tạo phải hiện ở
// menu chat (user thường) ngay khi có agent — không có lý do gì nó chỉ là
// "của admin". Với user thường không có base_url/has_key.
func TestBuildLLMOptionsListsDBOnlyProviderForEveryone(t *testing.T) {
	catalog, agents := optionsFixture()
	agents.markReady("minimax")
	settings := map[string]*domain.LLMProviderSetting{
		"minimax": {
			Slug: "minimax", Label: "MiniMax", Type: config.LLMTypeOpenAI,
			BaseURL: "https://api.minimax.example/v1", Model: "MiniMax-M2",
			APIKeyCipher: sealTestKey(t, "minimax-key"),
		},
	}
	user := buildLLMOptions(catalog, agents, settings, domain.RoleUser)
	if len(user.Providers) != 4 {
		t.Fatalf("user thấy %d provider, muốn 4 (3 config + 1 DB-only)", len(user.Providers))
	}
	var minimax *dtos.LLMProviderOption
	for _, opt := range user.Providers {
		if opt.Slug == "minimax" {
			minimax = opt
		}
	}
	if minimax == nil {
		t.Fatalf("thiếu provider UI tạo: %v", slugsOf(user))
	}
	if minimax.Label != "MiniMax" || minimax.Type != config.LLMTypeOpenAI ||
		minimax.DefaultModel != "MiniMax-M2" || !minimax.Available {
		t.Errorf("minimax = %+v", minimax)
	}
	if minimax.BaseURL != "" || minimax.HasKey != nil || minimax.FromConfig {
		t.Errorf("user thường không được thấy field admin / phải biết provider này không có ở file: %+v", minimax)
	}

	admin := buildLLMOptions(catalog, agents, settings, domain.RoleAdmin)
	for _, opt := range admin.Providers {
		if opt.Slug == "minimax" {
			if opt.BaseURL != "https://api.minimax.example/v1" || opt.HasKey == nil || !*opt.HasKey {
				t.Errorf("admin phải thấy base_url + has_key của provider UI tạo: %+v", opt)
			}
			if opt.FromConfig {
				t.Error("minimax không khai ở file nên from_config phải false")
			}
		}
	}
}

// TestBuildLLMOptionsDisabledOnlyForAdmin: provider admin tắt vẫn phải hiện với
// admin (để bật lại) với disabled=true + available=false, và biến mất khỏi menu
// chat của user thường.
func TestBuildLLMOptionsDisabledOnlyForAdmin(t *testing.T) {
	catalog, agents := optionsFixture()
	settings := map[string]*domain.LLMProviderSetting{
		"groq": {Slug: "groq", Disabled: true},
	}
	user := buildLLMOptions(catalog, agents, settings, domain.RoleUser)
	for _, slug := range slugsOf(user) {
		if slug == "groq" {
			t.Fatalf("provider đang tắt vẫn hiện ở menu chat: %v", slugsOf(user))
		}
	}
	admin := buildLLMOptions(catalog, agents, settings, domain.RoleAdmin)
	var groq *dtos.LLMProviderOption
	for _, opt := range admin.Providers {
		if opt.Slug == "groq" {
			groq = opt
		}
	}
	if groq == nil || !groq.Disabled || groq.Available {
		t.Errorf("admin phải thấy groq disabled + không available, nhận %+v", groq)
	}
}

// TestApplyProviderFormOnlyTouchesGivenFields: sửa mỗi nhãn không được xoá
// base_url/model đã lưu (form gửi lên thiếu field) và type "openai-compatible"
// phải lưu về "openai". Hàm này là pure form-mapper nên KHÔNG chạm key — secret
// nằm ở catalog, việc mã hoá do SaveProvider làm (test cấp service bên dưới).
func TestApplyProviderFormOnlyTouchesGivenFields(t *testing.T) {
	setting := &domain.LLMProviderSetting{
		Slug: "groq", Type: config.LLMTypeOpenAI, BaseURL: "https://api.groq.com/openai/v1",
		Model: "old", APIKeyCipher: sealTestKey(t, "old-key"), Models: `["old","keep"]`,
	}
	err := applyProviderForm(setting, &dtos.SaveLLMProviderRequest{Label: "Groq Free"})
	if err != nil {
		t.Fatalf("form hợp lệ: %v", err)
	}
	if setting.Label != "Groq Free" || setting.BaseURL == "" || setting.Model != "old" ||
		setting.APIKeyCipher == "" || setting.Models != `["old","keep"]` {
		t.Errorf("field không gửi phải giữ nguyên: %+v", setting)
	}

	err = applyProviderForm(setting, &dtos.SaveLLMProviderRequest{
		Type: "openai-compatible", BaseURL: "https://api.groq.com/openai/v1/",
		Model: "llama-3.3-70b-versatile", Models: []string{" m1 ", "m1", "", "m2"}, APIKey: "fresh",
	})
	if err != nil {
		t.Fatalf("form đầy đủ: %v", err)
	}
	if setting.Type != config.LLMTypeOpenAI {
		t.Errorf("type = %q, muốn chuẩn hoá về %q", setting.Type, config.LLMTypeOpenAI)
	}
	if setting.Model != "llama-3.3-70b-versatile" || setting.Models != `["m1","m2"]` {
		t.Errorf("model/models = %q / %s", setting.Model, setting.Models)
	}
	if strings.Contains(toJSON(t, setting), "fresh") {
		t.Error("form mapper không được nhét api key thô vào dòng cài đặt")
	}

	if err := applyProviderForm(setting, &dtos.SaveLLMProviderRequest{Type: "anthropic"}); err == nil {
		t.Error("type lạ phải bị chặn ở form")
	}
}

// TestSaveProviderSealsKeyAndRebuilds: khoá phần "key đi một chiều" của nghiệp vụ
// tạo provider — api key admin nhập phải vào DB dưới dạng cipher (giải mã đúng
// key, không có chuỗi thô ở đâu), và client dựng lại phải nhận key vừa giải mã
// được (không thì provider mới lưu xong vẫn 401).
func TestSaveProviderSealsKeyAndRebuilds(t *testing.T) {
	svc, agents, repo := settingsFixture(optionsLLMs(), nil)

	res := svc.SaveProvider(context.Background(), "minimax", &dtos.SaveLLMProviderRequest{
		Label: "MiniMax", Type: "openai-compatible", BaseURL: "https://api.minimax.example/v1",
		Model: "MiniMax-M2", APIKey: "fresh-key",
	})
	if res.Err != nil {
		t.Fatalf("SaveProvider: %v", res.Err)
	}
	saved := repo.rows["minimax"]
	if saved == nil || saved.APIKeyCipher == "" {
		t.Fatalf("không có dòng cài đặt đã lưu: %+v", saved)
	}
	if strings.Contains(toJSON(t, saved), "fresh-key") {
		t.Error("key thô không được nằm ở cột nào của dòng cài đặt")
	}
	plain, err := utils.OpenString(saved.APIKeyCipher, "test-secret")
	if err != nil || plain != "fresh-key" {
		t.Errorf("giải mã cipher = %q/%v, muốn fresh-key", plain, err)
	}

	rebuilt, ok := agents.rebuilt["minimax"]
	if !ok {
		t.Fatalf("lưu provider xong phải dựng client, registry = %v", agents.rebuilt)
	}
	if rebuilt.APIKey != "fresh-key" || rebuilt.BaseURL != "https://api.minimax.example/v1" ||
		rebuilt.Model != "MiniMax-M2" {
		t.Errorf("client dựng bằng %+v, muốn định nghĩa + key vừa lưu", rebuilt)
	}
	if !agents.Has("minimax") {
		t.Error("provider mới phải hiện ra là available")
	}
	// Danh sách trả về là của admin nên FE khỏi gọi lại /llm/options — nhưng
	// không được nhân cơ hội này mà lộ key.
	raw := toJSON(t, res.Data)
	if strings.Contains(raw, "fresh-key") || strings.Contains(raw, saved.APIKeyCipher) {
		t.Errorf("response lộ key: %s", raw)
	}
}

// TestSaveProviderRejectsUnbuildableDefinition: form hợp lệ khuôn nhưng không đời
// nào dựng được client (openai-compatible thiếu key) phải chết ngay ở validate,
// trước khi ghi DB — để chat không nhận 401 từ một provider "đã lưu".
func TestSaveProviderRejectsUnbuildableDefinition(t *testing.T) {
	svc, _, repo := settingsFixture(optionsLLMs(), nil)

	res := svc.SaveProvider(context.Background(), "khac", &dtos.SaveLLMProviderRequest{
		Type: "openai-compatible", BaseURL: "https://api.khac.example/v1", Model: "m1",
	})
	if res.Err == nil {
		t.Fatal("thiếu key phải bị chặn")
	}
	if apperrors.From(res.Err).HttpCode != 400 {
		t.Errorf("phải là 400 đọc được, nhận %+v", res.Err)
	}
	if repo.rows["khac"] != nil {
		t.Error("bị chặn thì không được ghi dòng cài đặt")
	}
}

// TestValidateConnection: những định nghĩa không đời nào dựng được client phải
// chết ngay ở form với lỗi đọc được, thay vì để chat nhận 401/500.
func TestValidateConnection(t *testing.T) {
	cases := []struct {
		name   string
		cfg    config.LLMProviderConfig
		wantOK bool
	}{
		{"thiếu type", config.LLMProviderConfig{BaseURL: "https://x/v1", APIKey: "k"}, false},
		{"type lạ", config.LLMProviderConfig{Type: "anthropic", BaseURL: "https://x/v1", APIKey: "k"}, false},
		{"thiếu base_url", config.LLMProviderConfig{Type: config.LLMTypeOpenAI, APIKey: "k"}, false},
		{"base_url không phải http", config.LLMProviderConfig{Type: config.LLMTypeOpenAI, BaseURL: "ftp://x/v1", APIKey: "k"}, false},
		{"openai thiếu key", config.LLMProviderConfig{Type: config.LLMTypeOpenAI, BaseURL: "https://x/v1"}, false},
		{"ollama không key vẫn được", config.LLMProviderConfig{Type: config.LLMTypeOllama, BaseURL: "http://localhost:11434"}, true},
		{"openai đủ", config.LLMProviderConfig{Type: config.LLMTypeOpenAI, BaseURL: "https://x/v1", APIKey: "k"}, true},
	}
	for _, tc := range cases {
		err := validateConnection("p", tc.cfg)
		if tc.wantOK && err != nil {
			t.Errorf("%s: muốn hợp lệ, nhận %v", tc.name, err)
		}
		if !tc.wantOK && err == nil {
			t.Errorf("%s: phải bị chặn", tc.name)
		}
	}
}

// TestProviderSlug: slug làm khoá DB và đi vào URL path nên phải là chữ thường,
// không dấu gạch chéo/khoảng trắng. "/" đặc biệt nguy hiểm vì model id thật có
// dấu "/" (cl/nvidia/...), nhỡ tay dán vào ô slug là tạo ra route lệch.
func TestProviderSlug(t *testing.T) {
	for _, raw := range []string{"9router", " Groq ", "ollama-cloud", "a.b_c-1"} {
		got, err := providerSlug(raw)
		if err != nil {
			t.Errorf("%q hợp lệ mà báo %v", raw, err)
			continue
		}
		if want := strings.ToLower(strings.TrimSpace(raw)); got != want {
			t.Errorf("chuẩn hoá %q = %q", raw, got)
		}
	}
	for _, raw := range []string{"", "  ", "Groq/key", "has space", "ông-x", strings.Repeat("a", 65), "/etc/passwd"} {
		if _, err := providerSlug(raw); err == nil {
			t.Errorf("slug %q phải bị chặn", raw)
		}
	}
}

// TestCleanModelsCap: một lần "Tải model" từ Ollama local có thể trả hàng trăm
// model — trần số phần tử giữ cột jsonb và menu chat dùng được.
func TestCleanModelsCap(t *testing.T) {
	in := make([]string, 0, providerModelsCap+50)
	for i := 0; i < providerModelsCap+50; i++ {
		in = append(in, fmt.Sprintf("model-%d", i))
	}
	out := cleanModels(in)
	if len(out) != providerModelsCap {
		t.Fatalf("len = %d, muốn %d", len(out), providerModelsCap)
	}
	if got := cleanModels([]string{" a ", "", "a", "b"}); strings.Join(got, ",") != "a,b" {
		t.Errorf("trim/dedup = %v, muốn [a b]", got)
	}
}

// TestLLMOptionsJSONStillNeverLeaksKey là chốt an toàn của nấc 3: thêm field
// định nghĩa provider (base_url/from_config/disabled) không được mở đường cho
// key ra client.
func TestLLMOptionsJSONStillNeverLeaksKey(t *testing.T) {
	catalog, agents := optionsFixture()
	cipher := sealTestKey(t, "super-secret-db-key")
	settings := map[string]*domain.LLMProviderSetting{
		"minimax": {Slug: "minimax", Type: config.LLMTypeOpenAI, BaseURL: "https://api.minimax.example/v1", Model: "M2", APIKeyCipher: cipher},
		"groq":    {Slug: "groq", Disabled: true},
	}
	for _, role := range []int{domain.RoleUser, domain.RoleAdmin} {
		raw := toJSON(t, buildLLMOptions(catalog, agents, settings, role))
		if strings.Contains(raw, "api_key") || strings.Contains(raw, "cipher") ||
			strings.Contains(raw, "super-secret-db-key") || strings.Contains(raw, cipher) {
			t.Fatalf("role %d lộ key: %s", role, raw)
		}
	}
}

func slugsOf(res *dtos.LLMOptionsResponse) []string {
	out := make([]string, 0, len(res.Providers))
	for _, opt := range res.Providers {
		out = append(out, opt.Slug)
	}
	return out
}

// settingsFixture dựng service cấp test từ đúng 3 collaborators mà
// NewLLMSettingsService nhận: catalog (config + repo giả), registry fake và
// fetcher không client (test CRUD không gọi ra ngoài). Registry có sẵn agent cho
// mọi provider khai trong llms, mặc định = llms.DefaultProvider — muốn đổi mặc
// định thì sửa llms trước khi gọi.
func settingsFixture(llms config.LLMsConfig, rows map[string]*domain.LLMProviderSetting) (
	*llmSettingsService, *fakeRegistry, *memorySettingRepo) {
	if rows == nil {
		rows = map[string]*domain.LLMProviderSetting{}
	}
	repo := &memorySettingRepo{rows: rows}
	available := make([]string, 0, len(llms.Providers))
	for slug := range llms.Providers {
		available = append(available, slugKey(slug))
	}
	agents := newFakeRegistry(slugKey(llms.DefaultProvider), available...)
	svc := newLLMSettingsService(newProviderCatalog(llms, repo, "test-secret"), agents, newLLMModelFetcher(nil))
	return svc, agents, repo
}

// memorySettingRepo là bản ILLMProviderSettingRepository chạy trên map, giữ lại
// slug của lần Delete gần nhất để test chứng minh guard chạy TRƯỚC khi đụng DB.
type memorySettingRepo struct {
	rows       map[string]*domain.LLMProviderSetting
	deleted    string
	deleteRuns int
}

func (r *memorySettingRepo) List(context.Context) ([]*domain.LLMProviderSetting, error) {
	out := make([]*domain.LLMProviderSetting, 0, len(r.rows))
	for _, row := range r.rows {
		out = append(out, row)
	}
	return out, nil
}

func (r *memorySettingRepo) Get(_ context.Context, slug string) (*domain.LLMProviderSetting, error) {
	row, ok := r.rows[slug]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return row, nil
}

func (r *memorySettingRepo) Save(_ context.Context, setting *domain.LLMProviderSetting) error {
	r.rows[setting.Slug] = setting
	return nil
}

func (r *memorySettingRepo) Delete(_ context.Context, slug string) error {
	r.deleted, r.deleteRuns = slug, r.deleteRuns+1
	delete(r.rows, slug)
	return nil
}

// TestDeleteProviderRefusesDefaultFromEitherSource khoá hai lỗ E2E bắt được ở
// cùng một chỗ: provider MẶC ĐỊNH khai trong config từng xoá được (200), còn
// provider UI tạo thì được bỏ qua guard vì điều kiện cũ đòi fromConfig=false.
// Một luật duy nhất cho cả hai nguồn — đây là cài đặt cuối cùng của mọi lượt
// chat không chọn provider rõ ràng.
func TestDeleteProviderRefusesDefaultFromEitherSource(t *testing.T) {
	llms := optionsLLMs()
	svc, _, repo := settingsFixture(llms, nil)

	res := svc.DeleteProvider(context.Background(), "9router")
	if res.Err == nil {
		t.Fatalf("xoá provider mặc định của file phải bị chặn, nhận %v", res.Data)
	}
	if apperrors.From(res.Err).HttpCode != 400 {
		t.Errorf("phải là 400 đọc được, nhận %+v", res.Err)
	}
	if !strings.Contains(res.Err.Error(), "provider mặc định") {
		t.Errorf("lỗi phải nói rõ lý do, nhận %q", res.Err.Error())
	}
	if repo.deleteRuns != 0 {
		t.Error("guard phải chạy trước khi xoá DB")
	}

	// Provider do UI tạo và đang là mặc định: cùng luật (setting != nil,
	// fromConfig false — chính là nhánh từng lọt).
	minimax := &domain.LLMProviderSetting{
		Slug: "minimax", Type: config.LLMTypeOpenAI,
		BaseURL: "https://api.minimax.example/v1", Model: "M2",
	}
	llms.DefaultProvider = "minimax"
	svc2, _, repo2 := settingsFixture(llms, map[string]*domain.LLMProviderSetting{"minimax": minimax})
	if res := svc2.DeleteProvider(context.Background(), "minimax"); res.Err == nil {
		t.Fatal("provider UI tạo đang mặc định cũng phải bị chặn")
	}
	if repo2.deleteRuns != 0 || repo2.rows["minimax"] == nil {
		t.Error("bị chặn thì dòng cài đặt phải còn nguyên trong DB")
	}
}

// TestDeleteProviderRemovesDBOnlyRow: phần còn lại của nghiệp vụ xoá — provider
// không phải mặc định thì gỡ dòng DB và tháo agent, provider của file thì dựng
// lại theo file.
func TestDeleteProviderRemovesDBOnlyRow(t *testing.T) {
	llms := optionsLLMs()
	svc, agents, repo := settingsFixture(llms, map[string]*domain.LLMProviderSetting{
		"minimax": {Slug: "minimax", Type: config.LLMTypeOpenAI, BaseURL: "https://api.minimax.example/v1"},
	})
	agents.markReady("minimax")

	res := svc.DeleteProvider(context.Background(), "minimax")
	if res.Err != nil {
		t.Fatalf("xoá provider không mặc định: %v", res.Err)
	}
	if repo.deleted != "minimax" || repo.rows["minimax"] != nil {
		t.Errorf("repo = %q/%v, muốn dòng bị xoá", repo.deleted, repo.rows["minimax"])
	}
	if agents.Has("minimax") {
		t.Error("provider UI tạo xoá xong phải biến mất khỏi registry")
	}
	for _, opt := range res.Data.Providers {
		if opt.Slug == "minimax" {
			t.Error("danh sách trả về vẫn còn provider vừa xoá")
		}
	}

	// Slug không có ở config lẫn DB là 404, không phải 200 "xoá thành công".
	if res := svc.DeleteProvider(context.Background(), "khong-ton-tai"); res.Err == nil {
		t.Error("slug lạ phải 404")
	} else if apperrors.From(res.Err).HttpCode != 404 {
		t.Errorf("HttpCode = %d, muốn 404", apperrors.From(res.Err).HttpCode)
	}
}

// TestDeleteConfigProviderRestoresFileConfig: xoá cài đặt của provider KHAI TRONG
// file là "gỡ chỉnh sửa đè lên file" — dòng DB biến mất nhưng agent phải được
// dựng lại đúng những gì file nói, không phải bị tháo.
func TestDeleteConfigProviderRestoresFileConfig(t *testing.T) {
	llms := optionsLLMs()
	svc, agents, repo := settingsFixture(llms, map[string]*domain.LLMProviderSetting{
		"groq": {Slug: "groq", Model: "model-cua-admin", APIKeyCipher: sealTestKey(t, "db-key")},
	})

	res := svc.DeleteProvider(context.Background(), "groq")
	if res.Err != nil {
		t.Fatalf("xoá cài đặt provider của file: %v", res.Err)
	}
	if repo.rows["groq"] != nil {
		t.Error("dòng cài đặt phải bị xoá")
	}
	restored, ok := agents.rebuilt["groq"]
	if !ok {
		t.Fatalf("provider của file phải được dựng lại, nhận removed=%v", agents.removed)
	}
	if restored.Model != "qwen/qwen3.8-27b" || restored.APIKey != "" {
		t.Errorf("dựng lại = %+v, muốn đúng model của file và không còn key đè", restored)
	}
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}
