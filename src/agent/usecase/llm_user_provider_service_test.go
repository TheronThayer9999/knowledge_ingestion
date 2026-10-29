package usecase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// fakeUserProviderRepo là repo giả của bảng llm_user_providers — GIỮ NGUYÊN
// luật scope user_id như repo thật: mọi Get/Delete đều lọc theo user, nên test
// đọc được cả hành vi "đoán id cũng không thấy dòng của người khác".
type fakeUserProviderRepo struct {
	rows    []*domain.LLMUserProvider
	nextID  int64
	saveErr error
}

func (r *fakeUserProviderRepo) List(_ context.Context, userID int64) ([]*domain.LLMUserProvider, error) {
	out := []*domain.LLMUserProvider{}
	for _, row := range r.rows {
		if row.UserID == userID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *fakeUserProviderRepo) Get(_ context.Context, id, userID int64) (*domain.LLMUserProvider, error) {
	for _, row := range r.rows {
		if row.ID == id && row.UserID == userID {
			return row, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *fakeUserProviderRepo) Save(_ context.Context, p *domain.LLMUserProvider) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	if p.ID == 0 {
		r.nextID++
		p.ID = r.nextID
		r.rows = append(r.rows, p)
		return nil
	}
	for i, row := range r.rows {
		if row.ID == p.ID {
			r.rows[i] = p
			return nil
		}
	}
	r.rows = append(r.rows, p)
	return nil
}

func (r *fakeUserProviderRepo) Delete(_ context.Context, id, userID int64) error {
	for i, row := range r.rows {
		if row.ID == id && row.UserID == userID {
			r.rows = append(r.rows[:i], r.rows[i+1:]...)
			return nil
		}
	}
	return nil
}

func (r *fakeUserProviderRepo) Count(_ context.Context, userID int64) (int64, error) {
	var n int64
	for _, row := range r.rows {
		if row.UserID == userID {
			n++
		}
	}
	return n, nil
}

// fakeUserResolver là IUserResolver giả — user_id lấy từ ctx (không phải body).
type fakeUserResolver struct{ id int64 }

func (f fakeUserResolver) UserID(context.Context) (int64, bool) { return f.id, f.id > 0 }

// fakeUserConfig là IConfig giả — service chỉ cần secret để mã hoá key.
type fakeUserConfig struct{}

func (fakeUserConfig) GetApp() config.AppConfig             { return config.AppConfig{} }
func (fakeUserConfig) GetDatabase() config.DBConfig         { return config.DBConfig{} }
func (fakeUserConfig) GetStorage() config.S3Config          { return config.S3Config{} }
func (fakeUserConfig) GetEmbedding() config.EmbeddingConfig { return config.EmbeddingConfig{} }
func (fakeUserConfig) GetLLMs() config.LLMsConfig           { return config.LLMsConfig{} }
func (fakeUserConfig) GetQdrant() config.QdrantConfig       { return config.QdrantConfig{} }
func (fakeUserConfig) GetOCR() config.OCRConfig             { return config.OCRConfig{} }
func (fakeUserConfig) GetRedis() config.RedisConfig         { return config.RedisConfig{} }
func (fakeUserConfig) GetRabbitMQ() config.RabbitMQConfig   { return config.RabbitMQConfig{} }
func (fakeUserConfig) GetJWT() config.JWTConfig             { return config.JWTConfig{Secret: "test-secret"} }
func (fakeUserConfig) GetCORS() config.CorsConfig           { return config.CorsConfig{} }
func (fakeUserConfig) GetWorker() config.WorkerConfig       { return config.WorkerConfig{} }
func (fakeUserConfig) GetAgent() config.AgentConfig         { return config.AgentConfig{} }
func (fakeUserConfig) GetVLM() config.VLMConfig             { return config.VLMConfig{} }
func (fakeUserConfig) GetChatCrypto() config.ChatCryptoConfig {
	return config.ChatCryptoConfig{}
}

// userProviderFixture dựng service + repo giả cho user id 7, kèm danh mục
// provider giả (config) — đường BYOK tra định nghĩa ở đây.
func userProviderFixture(userID int64) (*UserProviderService, *fakeUserProviderRepo) {
	return userProviderFixtureWith(userProviderLLMs("https://api.groq.com/openai/v1"), userID)
}

// userProviderLLMs là danh mục provider giả: groq (openai-compatible, cần key)
// và ollama (local, không key).
func userProviderLLMs(groqBase string) config.LLMsConfig {
	return config.LLMsConfig{
		DefaultProvider: "groq",
		Providers: map[string]config.LLMProviderConfig{
			"groq": {
				Type: config.LLMTypeOpenAI, Label: "Groq",
				Model: "llama-3.3-70b-versatile", BaseURL: groqBase,
			},
			"ollama": {
				Type: config.LLMTypeOllama, Label: "Ollama",
				Model: "gemma4:31b", BaseURL: "http://localhost:11434",
			},
		},
	}
}

// userProviderFixtureWith dựng service với danh mục provider tuỳ chọn (test
// dò model cần base URL trỏ về httptest server).
func userProviderFixtureWith(llms config.LLMsConfig, userID int64) (*UserProviderService, *fakeUserProviderRepo) {
	repo := &fakeUserProviderRepo{}
	settingRepo := &memorySettingRepo{rows: map[string]*domain.LLMProviderSetting{}}
	catalog := newProviderCatalog(llms, settingRepo, "test-secret")
	svc := NewUserProviderService(repo, eino.NewUserAgents(eino.InstructionSet{}, nil),
		newLLMModelFetcher(nil), fakeUserConfig{}, fakeUserResolver{id: userID}, catalog)
	return svc, repo
}

// ctxWithUser là ctx giả — service lấy user_id từ đây qua IUserResolver.
func ctxWithUser() context.Context { return context.Background() }

// TestUserProviderSlugNamespace khoá vùng slug của provider riêng: "u_<id>"
// không lẫn với slug provider chung, và admin không dùng được vùng này.
func TestUserProviderSlugNamespace(t *testing.T) {
	if got := domain.UserProviderSlug(12); got != "u_12" {
		t.Errorf("slug = %q, muốn u_12", got)
	}
	id, ok := domain.ParseUserProviderSlug("u_12")
	if !ok || id != 12 {
		t.Errorf("parse u_12 = %d/%v, muốn 12/true", id, ok)
	}
	if _, ok := domain.ParseUserProviderSlug("groq"); ok {
		t.Error("slug provider chung không được parse thành provider riêng")
	}
	if _, ok := domain.ParseUserProviderSlug("u_0"); ok {
		t.Error("u_0 không phải id hợp lệ")
	}
	if !domain.IsReservedUserProviderSlug("u_1") {
		t.Error("u_1 phải nằm trong vùng dành riêng")
	}
	if _, err := providerSlug("u_5"); err == nil {
		t.Error("admin không được tạo provider trong vùng của user")
	}
}

// TestSaveUserProviderSealsKey: key nhập vào được mã hoá lưu, và DTO trả ra
// CHỈ có has_key — không có đường nào đọc ngược key thật. Base URL lấy từ
// danh mục (user không tự gõ URL nữa).
func TestSaveUserProviderSealsKey(t *testing.T) {
	svc, repo := userProviderFixture(7)
	res := svc.Save(ctxWithUser(), 0, &dtos.SaveUserProviderRequest{
		Provider: "groq", APIKey: "gsk-secret", Model: "llama-3.3-70b",
	})
	if res.Err != nil {
		t.Fatalf("save lỗi: %v", res.Err)
	}
	if res.Data.HasKey == nil || !*res.Data.HasKey {
		t.Errorf("has_key = %v, muốn true", res.Data.HasKey)
	}
	if res.Data.Slug != "u_1" {
		t.Errorf("slug = %q, muốn u_1", res.Data.Slug)
	}
	if res.Data.Provider != "groq" {
		t.Errorf("provider = %q, muốn groq", res.Data.Provider)
	}
	row := repo.rows[0]
	if row.BaseURL != "https://api.groq.com/openai/v1" {
		t.Errorf("base_url = %q, muốn lấy từ danh mục", row.BaseURL)
	}
	if row.Label != "Groq" {
		t.Errorf("label = %q, muốn lấy nhãn của provider trong danh mục", row.Label)
	}
	if strings.Contains(row.APIKeyCipher, "gsk-secret") {
		t.Fatal("key thật không được nằm trong cột cipher")
	}
	if row.APIKeyCipher == "" {
		t.Error("key phải được mã hoá lưu lại")
	}
}

// TestSaveUserProviderKeepsKeyWhenBlank: sửa mà không nhập key thì giữ key cũ
// (nút "Tải model" đã lưu danh sách model không bị mất).
func TestSaveUserProviderKeepsKeyWhenBlank(t *testing.T) {
	svc, repo := userProviderFixture(7)
	svc.Save(ctxWithUser(), 0, &dtos.SaveUserProviderRequest{
		Provider: "groq", APIKey: "k1",
	})
	cipher := repo.rows[0].APIKeyCipher
	res := svc.Save(ctxWithUser(), 1, &dtos.SaveUserProviderRequest{
		Label: "Groq đổi tên",
	})
	if res.Err != nil {
		t.Fatalf("save lỗi: %v", res.Err)
	}
	if repo.rows[0].APIKeyCipher != cipher {
		t.Error("bỏ trống api_key thì phải giữ key đang có")
	}
	if res.Data.Label != "Groq đổi tên" {
		t.Errorf("label = %q, muốn đổi tên", res.Data.Label)
	}
}

// TestSaveUserProviderCreateNeedsProviderAndKey: tạo mới thiếu nhà cung cấp /
// key hoặc chọn provider không có trong danh mục thì phải báo 400 với câu đọc
// được. Binding ở tầng DTO KHÔNG chặn được (bỏ trống = "giữ giá trị cũ" khi
// sửa) nên luật "đủ thông tin mới dựng được client" nằm ở service — thiếu thì
// phải ra lỗi, không phải lưu dòng hỏng.
func TestSaveUserProviderCreateNeedsProviderAndKey(t *testing.T) {
	svc, _ := userProviderFixture(7)
	cases := []struct {
		name string
		req  *dtos.SaveUserProviderRequest
	}{
		{"thiếu nhà cung cấp", &dtos.SaveUserProviderRequest{APIKey: "k"}},
		{"provider không có trong danh mục", &dtos.SaveUserProviderRequest{
			Provider: "khong-ton-tai", APIKey: "k"}},
		{"thiếu key", &dtos.SaveUserProviderRequest{Provider: "groq"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := svc.Save(ctxWithUser(), 0, tc.req)
			if res.Err == nil {
				t.Fatal("phải báo lỗi khi thiếu thông tin")
			}
			e := errors.From(res.Err)
			if e == nil || e.GetHttpCode() != http.StatusBadRequest {
				t.Fatalf("http code = %v, muốn 400", e)
			}
		})
	}
}

// TestSaveUserProviderOllamaNeedsNoKey: provider ollama local chạy không key —
// luật "cần key" chỉ áp cho giao thức openai-compatible.
func TestSaveUserProviderOllamaNeedsNoKey(t *testing.T) {
	svc, _ := userProviderFixture(7)
	res := svc.Save(ctxWithUser(), 0, &dtos.SaveUserProviderRequest{Provider: "ollama"})
	if res.Err != nil {
		t.Fatalf("ollama không cần key: %v", res.Err)
	}
	if res.Data.HasKey == nil || *res.Data.HasKey {
		t.Errorf("has_key = %v, muốn false", res.Data.HasKey)
	}
}

// TestUserProviderCatalogHidesBaseURL: danh mục cho user chọn KHÔNG có base
// URL và không có key — base URL là cấu hình nội bộ, và user không tự gõ
// được nên cũng không cần thấy.
func TestUserProviderCatalogHidesBaseURL(t *testing.T) {
	svc, _ := userProviderFixture(7)
	res := svc.Catalog(ctxWithUser())
	if res.Err != nil {
		t.Fatalf("catalog lỗi: %v", res.Err)
	}
	if len(res.Data) != 2 {
		t.Fatalf("catalog có %d mục, muốn 2", len(res.Data))
	}
	raw := toJSON(t, res.Data)
	if strings.Contains(raw, "base_url") || strings.Contains(raw, "api.groq.com") {
		t.Errorf("catalog lộ base URL nội bộ: %s", raw)
	}
	for _, item := range res.Data {
		if item.Slug == "" || item.Label == "" || item.Type == "" {
			t.Errorf("mục catalog thiếu thông tin: %+v", item)
		}
	}
}

// TestUserProviderScopedByOwner: user khác đoán id cũng không sửa/xoá được
// dòng của người khác — 404 cho cả hai, client không biết sự khác biệt.
func TestUserProviderScopedByOwner(t *testing.T) {
	svc, _ := userProviderFixture(7)
	svc.Save(ctxWithUser(), 0, &dtos.SaveUserProviderRequest{
		Provider: "groq", APIKey: "k1",
	})
	other, _ := userProviderFixture(9)

	saveRes := other.Save(ctxWithUser(), 1, &dtos.SaveUserProviderRequest{
		Label: "Cướp", Provider: "groq",
	})
	assertNotFound(t, saveRes.Err)

	delRes := other.Delete(ctxWithUser(), 1)
	assertNotFound(t, delRes.Err)

	listRes := other.List(ctxWithUser())
	if listRes.Err != nil {
		t.Fatalf("list lỗi: %v", listRes.Err)
	}
	if len(listRes.Data) != 0 {
		t.Errorf("user khác thấy %d kết nối, muốn 0", len(listRes.Data))
	}
}

// TestUserProviderLimit chặn user dựng quá nhiều kết nối — mỗi dòng là một
// secret mã hoá và một agent riêng trong bộ nhớ đệm.
func TestUserProviderLimit(t *testing.T) {
	svc, _ := userProviderFixture(7)
	for i := 0; i < domain.UserProviderMax; i++ {
		res := svc.Save(ctxWithUser(), 0, &dtos.SaveUserProviderRequest{
			Label: "P", Provider: "groq", APIKey: "k",
		})
		if res.Err != nil {
			t.Fatalf("tạo %d: %v", i, res.Err)
		}
	}
	res := svc.Save(ctxWithUser(), 0, &dtos.SaveUserProviderRequest{
		Label: "P", Provider: "groq", APIKey: "k",
	})
	if res.Err == nil {
		t.Error("phải chặn kết nối vượt trần")
	}
}

// TestLoadModelsUsesOwnKey: nút "Tải model" hỏi CHÍNH provider bằng key của
// user — server dùng key hệ thống thì thành gọi nhầm tài khoản khác.
func TestLoadModelsUsesOwnKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"},{"id":"m2"}]}`))
	}))
	defer srv.Close()

	svc, repo := userProviderFixtureWith(userProviderLLMs(srv.URL), 7)
	svc.fetcher = newLLMModelFetcher(srv.Client())
	res := svc.Save(ctxWithUser(), 0, &dtos.SaveUserProviderRequest{
		Provider: "groq", APIKey: "gsk-secret",
	})
	if res.Err != nil {
		t.Fatalf("save lỗi: %v", res.Err)
	}
	models := svc.LoadModels(ctxWithUser(), res.Data.ID)
	if models.Err != nil {
		t.Fatalf("tải model lỗi: %v", models.Err)
	}
	if gotAuth != "Bearer gsk-secret" {
		t.Errorf("Authorization = %q, muốn key của user", gotAuth)
	}
	if len(models.Data.Models) != 2 {
		t.Errorf("models = %v, muốn 2 model", models.Data.Models)
	}
	if !strings.Contains(repo.rows[0].Models, "m1") {
		t.Errorf("danh sách model chưa lưu: %q", repo.rows[0].Models)
	}
}

// TestLoadModelsKeepsDiscoveredBase: base URL trong danh mục thiếu "/v1"
// (provider thật chỉ có /v1/models) thì server phải LƯU lại base đúng —
// client chat dựng từ base đó, nếu giữ base cũ thì mọi lượt chat sau đều 404.
func TestLoadModelsKeepsDiscoveredBase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer srv.Close()

	svc, repo := userProviderFixtureWith(userProviderLLMs(srv.URL+"/openai"), 7)
	svc.fetcher = newLLMModelFetcher(srv.Client())
	res := svc.Save(ctxWithUser(), 0, &dtos.SaveUserProviderRequest{
		Provider: "groq", APIKey: "k",
	})
	if res.Err != nil {
		t.Fatalf("save lỗi: %v", res.Err)
	}
	models := svc.LoadModels(ctxWithUser(), res.Data.ID)
	if models.Err != nil {
		t.Fatalf("tải model lỗi: %v", models.Err)
	}
	if len(models.Data.Models) != 1 || models.Data.Models[0] != "m1" {
		t.Errorf("models = %v, muốn [m1]", models.Data.Models)
	}
	if repo.rows[0].BaseURL != srv.URL+"/openai/v1" {
		t.Errorf("base đã lưu = %q, muốn %q", repo.rows[0].BaseURL, srv.URL+"/openai/v1")
	}
}

// TestChatProviderErrorKeeps404: lượt chat chọn kết nối không tồn tại phải trả
// 404 (không phải 400 chung chung) — client biết là "kết nối không còn".
func TestChatProviderErrorKeeps404(t *testing.T) {
	err := chatProviderError(unknownUserProvider(3))
	e := errors.From(err)
	if e == nil || e.GetHttpCode() != 404 {
		t.Fatalf("http code = %v, muốn 404", e)
	}
	plain := chatProviderError(errors.NewCustomHttpError(400, 400, "chưa chọn model"))
	if e := errors.From(plain); e == nil || e.GetHttpCode() != 400 {
		t.Fatalf("http code = %v, muốn 400", e)
	}
}

// assertNotFound kiểm tra lỗi là 404 — lỗi khác (500) là rò rỉ thông tin.
func assertNotFound(t *testing.T, err error) {
	t.Helper()
	e := errors.From(err)
	if e == nil || e.GetHttpCode() != 404 {
		t.Fatalf("lỗi = %v, muốn 404", err)
	}
}
