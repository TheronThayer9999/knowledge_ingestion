package apis

import (
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/controller/dtos"

	"github.com/gin-gonic/gin"
)

// LLMUserAPI là controller cho các kết nối LLM RIÊNG của từng tài khoản
// (BYOK) — user tự nhập base_url + API key của họ rồi dùng cho lượt chat của
// chính họ.
//
// Khác hẳn /llm/providers/* của admin: ở đây KHÔNG gate role, chỉ cần đăng
// nhập. Chủ sở hữu lấy từ token trong service (không có tham số user_id nào ở
// đây để client đoán nhầm), nên không có đường nào đọc được kết nối của người
// khác. API key đi MỘT chiều: nhận vào → mã hoá → lưu, không có endpoint đọc
// lại — response chỉ có has_key (có/không).
type LLMUserAPI struct {
	*baseController
	svc port.IUserProviders
}

func NewLLMUserAPI(base *baseController, svc port.IUserProviders) *LLMUserAPI {
	return &LLMUserAPI{baseController: base, svc: svc}
}

// List godoc
// @Summary Danh sách kết nối LLM của tôi
// @Description Các kết nối riêng của chính người đang đăng nhập. Key KHÔNG BAO GIỜ trả về — chỉ có `has_key`.
// @Tags llm-my
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Failure 401 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/llm/my/providers [get]
func (a *LLMUserAPI) List(c *gin.Context) {
	Render(c, a, a.svc.List(c))
}

// Create godoc
// @Summary Thêm kết nối LLM của tôi
// @Description Tạo kết nối riêng (base URL + API key + model). Key được mã hoá rồi lưu, không bao giờ trả lại. Tối đa 5 kết nối mỗi tài khoản.
// @Tags llm-my
// @Accept json
// @Produce json
// @Param request body dtos.SaveUserProviderRequest true "Kết nối mới"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/llm/my/providers [post]
func (a *LLMUserAPI) Create(c *gin.Context) {
	req := &dtos.SaveUserProviderRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Save(c, 0, req))
}

// Update godoc
// @Summary Sửa kết nối LLM của tôi
// @Description Sửa một kết nối của chính mình. Field bỏ trống giữ nguyên giá trị cũ; `api_key` để trống nghĩa là giữ key đang có. 404 nếu kết nối không tồn tại hoặc thuộc tài khoản khác.
// @Tags llm-my
// @Accept json
// @Produce json
// @Param id path int true "id kết nối"
// @Param request body dtos.SaveUserProviderRequest true "Thông tin mới"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/llm/my/providers/{id} [put]
func (a *LLMUserAPI) Update(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id kết nối không hợp lệ") {
		return
	}
	req := &dtos.SaveUserProviderRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Save(c, p.ID, req))
}

// Delete godoc
// @Summary Xoá kết nối LLM của tôi
// @Description Xoá kết nối của chính mình, trả lại danh sách còn lại. 404 nếu kết nối không tồn tại hoặc thuộc tài khoản khác.
// @Tags llm-my
// @Produce json
// @Param id path int true "id kết nối"
// @Success 200 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/llm/my/providers/{id} [delete]
func (a *LLMUserAPI) Delete(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id kết nối không hợp lệ") {
		return
	}
	Render(c, a, a.svc.Delete(c, p.ID))
}

// Catalog godoc
// @Summary Danh mục nhà cung cấp để gắn key của tôi
// @Description Danh sách provider của hệ thống mà người dùng được phép gắn key riêng. Chỉ có nhãn + giao thức — không có base URL nội bộ, không có key.
// @Tags llm-my
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Failure 401 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/llm/my/providers/catalog [get]
func (a *LLMUserAPI) Catalog(c *gin.Context) {
	Render(c, a, a.svc.Catalog(c))
}

// LoadModels godoc
// @Summary Tải danh sách model của kết nối của tôi
// @Description Gọi chính provider bằng key của người gọi rồi lưu danh sách model, trả về danh sách đã sắp A→Z.
// @Tags llm-my
// @Produce json
// @Param id path int true "id kết nối"
// @Success 200 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Failure 502 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/llm/my/providers/{id}/models [post]
func (a *LLMUserAPI) LoadModels(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id kết nối không hợp lệ") {
		return
	}
	Render(c, a, a.svc.LoadModels(c, p.ID))
}
