package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// CryptoAPI mở phiên mã hoá payload (Đợt 29). Controller chỉ validate rồi
// Render — mọi thuật toán nằm trong services.IChatCryptoService.
type CryptoAPI struct {
	*baseController
	svc services.IChatCryptoService
}

func NewCryptoAPI(base *baseController, svc services.IChatCryptoService) *CryptoAPI {
	return &CryptoAPI{baseController: base, svc: svc}
}

// Handshake godoc
// @Summary Mở phiên mã hoá payload chat
// @Description Client sinh cặp khoá X25519, gửi public key lên đây; server trả public key ephemeral + session id, hai phía tự dẫn xuất chung một khoá AES-256-GCM. Từ lượt sau, body của /chat, /chat/attachments, /conversations* đi trong phong bì {v,sid,nonce,ct} kèm header X-KI-Session. Route này LUÔN plaintext (nó là thứ dựng nên khoá) và cần Bearer token: phiên mã hoá gắn với user đã đăng nhập.
// @Tags crypto
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dtos.CryptoHandshakeRequest true "public key của client"
// @Success 200 {object} dtos.ResponseResource{data=dtos.CryptoHandshakeResponse}
// @Failure 400 {object} dtos.ResponseResource
// @Router /api/v1/crypto/handshake [post]
func (a *CryptoAPI) Handshake(c *gin.Context) {
	var req dtos.CryptoHandshakeRequest
	if !a.Bind(c, &req) {
		return
	}
	Render(c, a, a.svc.Handshake(c.Request.Context(), &req))
}
