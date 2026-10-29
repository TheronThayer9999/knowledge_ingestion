package usecase

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// UserProviderService là phần BYOK: mỗi tài khoản tự thêm kết nối LLM riêng
// (base URL + API key) và dùng nó cho các lượt chat của chính họ.
//
// Ba điều kiện bất di bất dịch:
//   - Key đi MỘT chiều: nhận vào → mã hoá → lưu. Không có đường đọc ngược ra,
//     kể cả với chính user đó; UI chỉ thấy has_key.
//   - Mọi truy vấn DB scope theo user_id — user khác đoán id cũng không đọc
//     được dòng của người khác (repo lọc ở WHERE, không lọc ở service).
//   - Kết nối riêng KHÔNG dùng cho worker compile — compile là việc của hệ
//     thống, dùng provider chung của admin.
type UserProviderService struct {
	repo    domain.ILLMUserProviderRepository
	agents  *eino.UserAgents
	fetcher *modelFetcher
	secret  string
	users   port.IUserResolver
}

// NewUserProviderService là constructor fx gọi — secret lấy từ config để mã
// hoá key giống hệt provider chung (cùng một khoá AES-GCM).
func NewUserProviderService(repo domain.ILLMUserProviderRepository, agents *eino.UserAgents,
	fetcher *modelFetcher, cfg config.IConfig, users port.IUserResolver) *UserProviderService {
	return &UserProviderService{
		repo: repo, agents: agents, fetcher: fetcher,
		secret: cfg.GetJWT().Secret, users: users,
	}
}

// List trả các kết nối riêng của người gọi — dùng ở màn Cài đặt.
func (s *UserProviderService) List(ctx context.Context) dtos.Result[[]*dtos.LLMProviderOption] {
	userID, err := s.userID(ctx)
	if err != nil {
		return dtos.Fail[[]*dtos.LLMProviderOption](err)
	}
	options, err := s.ListOptions(ctx, userID)
	if err != nil {
		return dtos.Fail[[]*dtos.LLMProviderOption](err)
	}
	return dtos.Ok(options)
}

// ListOptions là phần ĐỌC cho menu chọn model của chat — trả về lỗi thô để
// service options (đường đọc) tự xử lý theo luật của nó.
func (s *UserProviderService) ListOptions(ctx context.Context, userID int64) ([]*dtos.LLMProviderOption, error) {
	rows, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]*dtos.LLMProviderOption, 0, len(rows))
	for _, row := range rows {
		out = append(out, userProviderOption(row))
	}
	return out, nil
}

// Save tạo mới (id = 0) hoặc sửa một kết nối riêng. Sửa thì field bỏ trống
// giữ nguyên giá trị cũ — danh sách model đã tải không mất khi user chỉ đổi
// key. Lượt chat đang chạy giữ agent cũ tới khi xong, lượt sau dựng lại.
func (s *UserProviderService) Save(ctx context.Context, id int64,
	req *dtos.SaveUserProviderRequest) dtos.Result[*dtos.LLMProviderOption] {
	userID, err := s.userID(ctx)
	if err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	row, err := s.load(ctx, id, userID)
	if err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	if row == nil {
		if err := s.checkLimit(ctx, userID); err != nil {
			return dtos.Fail[*dtos.LLMProviderOption](err)
		}
		row = &domain.LLMUserProvider{UserID: userID, Models: "[]"}
	}
	if err := applyUserProviderForm(row, req); err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	if err := s.sealKey(row, req.APIKey); err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	if err := validateUserProvider(row); err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	if err := s.repo.Save(ctx, row); err != nil {
		return dtos.Fail[*dtos.LLMProviderOption](err)
	}
	s.agents.Drop(userProviderCacheKey(userID, row.ID))
	return dtos.Ok(userProviderOption(row))
}

// Delete xoá kết nối của người gọi và tháo agent tương ứng khỏi bộ nhớ đệm.
func (s *UserProviderService) Delete(ctx context.Context, id int64) dtos.Result[[]*dtos.LLMProviderOption] {
	userID, err := s.userID(ctx)
	if err != nil {
		return dtos.Fail[[]*dtos.LLMProviderOption](err)
	}
	if _, err := s.load(ctx, id, userID); err != nil {
		return dtos.Fail[[]*dtos.LLMProviderOption](err)
	}
	if err := s.repo.Delete(ctx, id, userID); err != nil {
		return dtos.Fail[[]*dtos.LLMProviderOption](err)
	}
	s.agents.Drop(userProviderCacheKey(userID, id))
	options, err := s.ListOptions(ctx, userID)
	if err != nil {
		return dtos.Fail[[]*dtos.LLMProviderOption](err)
	}
	return dtos.Ok(options)
}

// LoadModels hỏi chính provider của user bằng key của họ rồi lưu lại —
// danh sách đọc được không cần user tự gõ tay.
func (s *UserProviderService) LoadModels(ctx context.Context, id int64) dtos.Result[*dtos.LLMModelsResponse] {
	userID, err := s.userID(ctx)
	if err != nil {
		return dtos.Fail[*dtos.LLMModelsResponse](err)
	}
	row, err := s.load(ctx, id, userID)
	if err != nil {
		return dtos.Fail[*dtos.LLMModelsResponse](err)
	}
	if row == nil {
		return dtos.Fail[*dtos.LLMModelsResponse](unknownUserProvider(id))
	}
	models, err := s.fetcher.Fetch(ctx, s.effective(row))
	if err != nil {
		return dtos.Fail[*dtos.LLMModelsResponse](s.fetcher.WrapError(row.Label, err))
	}
	row.Models = encodeModelList(cleanModels(models))
	if err := s.repo.Save(ctx, row); err != nil {
		return dtos.Fail[*dtos.LLMModelsResponse](err)
	}
	return dtos.Ok(&dtos.LLMModelsResponse{Slug: domain.UserProviderSlug(row.ID), Models: models})
}

// Resolve dựng agent cho một lượt chat chạy trên kết nối riêng của user.
// Trả kèm client thô để bước phân loại câu hỏi dùng đúng provider của lượt.
func (s *UserProviderService) Resolve(ctx context.Context, userID int64, provider, model string,
	thinking bool) (*eino.ChatAgent, string, einomodel.ToolCallingChatModel, error) {
	id, ok := domain.ParseUserProviderSlug(provider)
	if !ok {
		return nil, "", nil, fmt.Errorf("provider %q không phải kết nối riêng", provider)
	}
	row, err := s.repo.Get(ctx, id, userID)
	if stderrors.Is(err, domain.ErrNotFound) {
		return nil, "", nil, unknownUserProvider(id)
	}
	if err != nil {
		return nil, "", nil, err
	}
	return s.agents.Resolve(userProviderCacheKey(userID, row.ID), s.effective(row), model, thinking)
}

// userID lấy tài khoản của người gọi từ ctx — không tin id client gửi lên.
func (s *UserProviderService) userID(ctx context.Context) (int64, error) {
	userID, ok := s.users.UserID(ctx)
	if !ok || userID <= 0 {
		return 0, badLLMRequest("không xác định được tài khoản")
	}
	return userID, nil
}

// load đọc dòng của user; id = 0 nghĩa là TẠO MỚI nên trả (nil, nil).
func (s *UserProviderService) load(ctx context.Context, id, userID int64) (*domain.LLMUserProvider, error) {
	if id <= 0 {
		return nil, nil
	}
	row, err := s.repo.Get(ctx, id, userID)
	if stderrors.Is(err, domain.ErrNotFound) {
		return nil, unknownUserProvider(id)
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

// checkLimit chặn user dựng quá nhiều kết nối — mỗi dòng là một secret mã
// hoá và một agent riêng trong bộ nhớ đệm.
func (s *UserProviderService) checkLimit(ctx context.Context, userID int64) error {
	count, err := s.repo.Count(ctx, userID)
	if err != nil {
		return err
	}
	if count >= domain.UserProviderMax {
		return badLLMRequest(fmt.Sprintf(
			"tối đa %d kết nối riêng mỗi tài khoản — xoá bớt một kết nối cũ", domain.UserProviderMax))
	}
	return nil
}

// sealKey mã hoá key user nhập. Key rỗng = giữ key đang có (sửa mà không
// đổi key thì không mất key).
func (s *UserProviderService) sealKey(row *domain.LLMUserProvider, key string) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return nil
	}
	cipher, err := utils.SealString(trimmed, s.secret)
	if err != nil {
		return err
	}
	row.APIKeyCipher = cipher
	return nil
}

// effective là cấu hình THẬT dùng để dựng client: định nghĩa + model + key
// đã giải mã. Dùng lại đúng luật gộp của provider chung (MergeProviderSetting)
// để hai đường không lệch nhau.
func (s *UserProviderService) effective(row *domain.LLMUserProvider) config.LLMProviderConfig {
	setting := &domain.LLMProviderSetting{
		Slug:         domain.UserProviderSlug(row.ID),
		Label:        row.Label,
		Type:         row.Type,
		BaseURL:      row.BaseURL,
		APIKeyCipher: row.APIKeyCipher,
		Model:        row.Model,
		Models:       row.Models,
	}
	return MergeProviderSetting(config.LLMProviderConfig{}, setting, s.secret)
}

// userProviderCacheKey là khoá bộ nhớ đệm agent — gồm cả user id vì mỗi
// user có key khác nhau cho cùng một base URL.
func userProviderCacheKey(userID, providerID int64) string {
	return fmt.Sprintf("%d:%d", userID, providerID)
}

// unknownUserProvider là 404 "kết nối không có" — nói chung cho cả khi dòng
// không tồn tại lẫn khi dòng có nhưng thuộc user khác: client không được
// biết sự khác biệt.
func unknownUserProvider(id int64) *errors.Error {
	return errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound,
		fmt.Sprintf("kết nối %d không tồn tại", id))
}

// applyUserProviderForm ghi form lên dòng — CHỈ field nào có giá trị mới
// đổi, nên sửa một ô không xoá base URL hay danh sách model đã lưu.
func applyUserProviderForm(row *domain.LLMUserProvider, req *dtos.SaveUserProviderRequest) error {
	if label := strings.TrimSpace(req.Label); label != "" {
		row.Label = label
	}
	if raw := strings.TrimSpace(req.Type); raw != "" {
		kind := NormalizeProviderType(raw)
		if kind == "" {
			return badLLMRequest(fmt.Sprintf(
				"giao thức %q không hỗ trợ — chỉ có Ollama hoặc OpenAI-compatible", raw))
		}
		row.Type = kind
	}
	if url := strings.TrimSpace(req.BaseURL); url != "" {
		row.BaseURL = url
	}
	if model := strings.TrimSpace(req.Model); model != "" {
		row.Model = model
	}
	if req.Models != nil {
		row.Models = encodeModelList(cleanModels(req.Models))
	}
	return nil
}

// validateUserProvider chặn sớm những kết nối không đời nào dựng được client,
// kèm lỗi đọc được cho user.
func validateUserProvider(row *domain.LLMUserProvider) error {
	if strings.TrimSpace(row.Label) == "" {
		return badLLMRequest("chưa đặt tên cho kết nối")
	}
	if NormalizeProviderType(row.Type) == "" {
		return badLLMRequest("chưa chọn giao thức — Ollama hoặc OpenAI-compatible")
	}
	url := strings.TrimSpace(row.BaseURL)
	if url == "" {
		return badLLMRequest("chưa có base URL cho kết nối")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return badLLMRequest("base URL phải bắt đầu bằng http:// hoặc https://")
	}
	if row.Type == config.LLMTypeOpenAI && strings.TrimSpace(row.APIKeyCipher) == "" {
		return badLLMRequest("kết nối OpenAI-compatible cần API key")
	}
	return nil
}

// userProviderOption dựng dòng UI cho một kết nối riêng. Key KHÔNG BAO GIỜ
// có mặt — chỉ has_key (có/không) và nguồn key.
func userProviderOption(row *domain.LLMUserProvider) *dtos.LLMProviderOption {
	hasKey := strings.TrimSpace(row.APIKeyCipher) != ""
	source := ""
	if hasKey {
		source = keySourceDB
	}
	return &dtos.LLMProviderOption{
		Slug:         domain.UserProviderSlug(row.ID),
		ID:           row.ID,
		Label:        row.Label,
		Type:         row.Type,
		DefaultModel: strings.TrimSpace(row.Model),
		Models:       userProviderModels(row),
		Available:    validateUserProvider(row) == nil,
		BaseURL:      strings.TrimRight(strings.TrimSpace(row.BaseURL), "/"),
		HasKey:       &hasKey,
		KeySource:    source,
	}
}

// userProviderModels là danh sách model của kết nối: model mặc định luôn ở
// đầu để menu chat không thiếu đúng model sẽ chạy khi request bỏ trống.
func userProviderModels(row *domain.LLMUserProvider) []string {
	models := decodeModelList(row.Models)
	defaultModel := strings.TrimSpace(row.Model)
	if defaultModel == "" {
		return models
	}
	for _, item := range models {
		if item == defaultModel {
			return models
		}
	}
	return append([]string{defaultModel}, models...)
}

// IUserProviderResolver là đường chat dùng provider riêng — đặt ở usecase
// (cùng chỗ với IProviderRegistry) vì nó trả về agent của package eino.
type IUserProviderResolver interface {
	// Resolve dựng agent + model cho kết nối riêng của userID.
	Resolve(ctx context.Context, userID int64, provider, model string,
		thinking bool) (*eino.ChatAgent, string, einomodel.ToolCallingChatModel, error)
}

// IUserProviderLister là phần ĐỌC cho menu chọn model — service options
// dùng để thêm nhóm "Kết nối của tôi" mà không cần quyền ghi.
type IUserProviderLister interface {
	ListOptions(ctx context.Context, userID int64) ([]*dtos.LLMProviderOption, error)
}
