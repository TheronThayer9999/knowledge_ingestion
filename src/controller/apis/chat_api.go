package apis

import (
	"context"
	"net/http"
	"time"

	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// chatAttachmentTimeout chặn tệp lớn/nặng giữ request HTTP mãi — trích đồng bộ
// nên phải có trần thời gian; quá hạn rơi vào nhánh ctx của Render (không 500).
const chatAttachmentTimeout = 60 * time.Second

// ChatAPI hỏi đáp tri thức cho frontend chat — controller chỉ validate (Bind)
// rồi Render, BE điều phối agentic ở service. Ngoài lượt hỏi còn có endpoint
// trích text tệp đính kèm (đi thẳng vào prompt của lượt, không vào kho).
type ChatAPI struct {
	*baseController
	svc         port.IChatter
	attachments services.IChatAttachmentService
}

func NewChatAPI(base *baseController, svc port.IChatter, attachments services.IChatAttachmentService) *ChatAPI {
	return &ChatAPI{baseController: base, svc: svc, attachments: attachments}
}

// Chat godoc
// @Summary Chat hỏi đáp tri thức (markdown)
// @Description Agent tự vòng [nghĩ → gọi tool tìm tài liệu → đọc → điều chỉnh] rồi trả Markdown kèm cites để frontend ReactJS render. Cần đăng nhập; provider/model chọn theo request, `thinking: true` bật suy luận sâu (model tự nghĩ trước khi đáp, đoạn nghĩ về theo field `reasoning`).
// @Tags chat
// @Accept json
// @Produce json
// @Param request body dtos.ChatRequest true "chat message"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/chat [post]
func (a *ChatAPI) Chat(c *gin.Context) {
	req := &dtos.ChatRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.Chat(c, req))
}

// Attachment godoc
// @Summary Extract text from an uploaded file for one chat turn
// @Description Reads the file synchronously with the same extractor registry as the ingestion pipeline and returns its plain text. The text is then sent back inside ChatRequest.attachment for that single turn — it is NOT indexed into the knowledge base, so the answer comes immediately and carries no citations from the file. Admin-free, any signed-in user may attach a file. Objects larger than 15 MB are rejected.
// @Tags chat
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "file to read for this chat turn (max 15 MB)"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/chat/attachments [post]
func (a *ChatAPI) Attachment(c *gin.Context) {
	// Chặn body TRƯỚC khi gin đọc vào RAM; cộng dư địa cho header multipart.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, constants.CHAT_ATTACHMENT_MAX_BYTES+1<<20)
	file, err := c.FormFile("file")
	if err != nil {
		a.BadRequest(c, "không đọc được tệp đính kèm (tối đa 15 MB)")
		return
	}
	if file.Size > constants.CHAT_ATTACHMENT_MAX_BYTES {
		a.BadRequest(c, "tệp đính kèm vượt quá 15 MB")
		return
	}
	opened, err := file.Open()
	if err != nil {
		a.BadRequest(c, "không đọc được tệp đính kèm")
		return
	}
	// File header do multipart giữ trong RAM/temp; đóng sau khi trích xong.
	defer func() { _ = opened.Close() }()
	ctx, cancel := context.WithTimeout(c, chatAttachmentTimeout)
	defer cancel()
	Render(c, a, a.attachments.Extract(ctx, file.Filename, opened))
}
