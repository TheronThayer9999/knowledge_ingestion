package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// ConversationAPI là controller phiên chat — chỉ Bind + Render, owner lấy từ
// token ngay trong service (nên không có tham số user_id nào ở đây để client
// đoán nhầm/đoán gian).
type ConversationAPI struct {
	*baseController
	svc services.IConversationService
}

func NewConversationAPI(base *baseController, svc services.IConversationService) *ConversationAPI {
	return &ConversationAPI{baseController: base, svc: svc}
}

// Create godoc
// @Summary Mở một phiên chat
// @Description Tạo phiên rỗng để chat nhiều lượt gắn vào (conversation_id). Mọi field tuỳ chọn; title để trống thì lượt chat đầu tiên tự điền theo câu hỏi.
// @Tags conversations
// @Accept json
// @Produce json
// @Param request body dtos.CreateConversationRequest true "payload phiên"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/conversations [post]
func (a *ConversationAPI) Create(c *gin.Context) {
	req := &dtos.CreateConversationRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Create(c, req))
}

// List godoc
// @Summary Danh sách phiên chat của tôi
// @Description Mới nhất trước theo lượt cuối, kèm số message của từng phiên. `before` là keyset con trỏ (id phiên cuối của trang trước).
// @Tags conversations
// @Produce json
// @Param limit query int false "số phiên, mặc định 20, tối đa 100"
// @Param before query int false "id phiên con trỏ"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/conversations [get]
func (a *ConversationAPI) List(c *gin.Context) {
	req := &dtos.ListConversationsRequest{}
	if !a.BindQuery(c, req) {
		return
	}
	Render(c, a, a.svc.List(c, req))
}

// Get godoc
// @Summary Một phiên chat
// @Description 404 nếu phiên không tồn tại HOẶC không phải của người đang đăng nhập
// @Tags conversations
// @Produce json
// @Param id path int true "conversation id"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/conversations/{id} [get]
func (a *ConversationAPI) Get(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id phiên không hợp lệ") {
		return
	}
	Render(c, a, a.svc.Get(c, p.ID))
}

// Rename godoc
// @Summary Đổi tên phiên chat
// @Description Đổi title của phiên của chính mình
// @Tags conversations
// @Accept json
// @Produce json
// @Param id path int true "conversation id"
// @Param request body dtos.RenameConversationRequest true "title mới"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/conversations/{id} [patch]
func (a *ConversationAPI) Rename(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id phiên không hợp lệ") {
		return
	}
	req := &dtos.RenameConversationRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Rename(c, p.ID, req))
}

// Delete godoc
// @Summary Xoá phiên chat
// @Description Xoá mềm phiên (biến khỏi danh sách); message ở lại trong DB vì chưa có purge cứng
// @Tags conversations
// @Produce json
// @Param id path int true "conversation id"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/conversations/{id} [delete]
func (a *ConversationAPI) Delete(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id phiên không hợp lệ") {
		return
	}
	Render(c, a, a.svc.Delete(c, p.ID))
}

// Messages godoc
// @Summary Lịch sử message của một phiên
// @Description Mở lại phiên cũ: trả message đã lưu KÈM cites (chip [^n] sống lại). Phân trang ngược bằng `before`, kết quả oldest-first trong cửa sổ.
// @Tags conversations
// @Produce json
// @Param id path int true "conversation id"
// @Param limit query int false "số message, mặc định 50, tối đa 200"
// @Param before query int false "id message con trỏ"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/conversations/{id}/messages [get]
func (a *ConversationAPI) Messages(c *gin.Context) {
	var p dtos.IDParam
	if !a.BindUri(c, &p, "id phiên không hợp lệ") {
		return
	}
	req := &dtos.ListMessagesRequest{}
	if !a.BindQuery(c, req) {
		return
	}
	Render(c, a, a.svc.Messages(c, p.ID, req))
}
