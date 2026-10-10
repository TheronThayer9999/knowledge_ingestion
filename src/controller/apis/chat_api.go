package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// ChatAPI hỏi đáp tri thức cho frontend chat — controller chỉ validate (Bind)
// rồi Render, BE điều phối agentic ở service.
type ChatAPI struct {
	*baseController
	svc services.IChatService
}

func NewChatAPI(base *baseController, svc services.IChatService) *ChatAPI {
	return &ChatAPI{baseController: base, svc: svc}
}

// Chat godoc
// @Summary Chat hỏi đáp tri thức (markdown)
// @Description Classify câu hỏi → search tài liệu upload → LLM tổng hợp thành Markdown kèm cites để frontend ReactJS render. Cần đăng nhập, model LLM truyền theo request.
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
