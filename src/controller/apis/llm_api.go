package apis

import (
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"

	"github.com/gin-gonic/gin"
)

// LLMAPI phản chiếu cấu hình provider/model ra UI (Đợt 28) — không bao giờ trả
// api_key. Endpoint ghi (key/model) đã bị chặn bởi RequireRole ở route, nên
// handler không phải tự kiểm quyền.
type LLMAPI struct {
	*baseController
	svc      port.ILLMOptions
	settings port.ILLMSettings
}

func NewLLMAPI(base *baseController, svc port.ILLMOptions, settings port.ILLMSettings) *LLMAPI {
	return &LLMAPI{baseController: base, svc: svc, settings: settings}
}

// Options godoc
// @Summary Danh sách provider/model cho UI
// @Description Trả danh sách provider đã cấu hình (sắp theo slug) kèm model gợi ý, trạng thái khả dụng và nhãn suy luận sâu. Không bao giờ trả api_key. `base_url`/`has_key`/`key_source`/`config_model` chỉ có với admin (role ≥ 1).
// @Tags llm
// @Produce json
// @Success 200 {object} dtos.ResponseResource
// @Failure 401 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/llm/options [get]
func (a *LLMAPI) Options(c *gin.Context) {
	role, _ := middlewares.GetUserRole(c)
	Render(c, a, a.svc.Options(c, role))
}

// SaveKey godoc
// @Summary Lưu API key cho provider (admin)
// @Description Mã hoá key rồi lưu DB; key trong DB thắng key config. Không có endpoint nào đọc ngược key ra client.
// @Tags llm
// @Accept json
// @Produce json
// @Param slug path string true "Slug provider trong config"
// @Param body body dtos.SaveLLMKeyRequest true "API key"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 403 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/llm/providers/{slug}/key [put]
func (a *LLMAPI) SaveKey(c *gin.Context) {
	var req dtos.SaveLLMKeyRequest
	if !a.Bind(c, &req) {
		return
	}
	Render(c, a, a.settings.SaveKey(c, c.Param("slug"), req.APIKey))
}

// ClearKey godoc
// @Summary Xoá API key đã lưu của provider (admin)
// @Description Xoá key trong DB để provider quay về key trong config. Model đã chọn và danh sách model đã tải vẫn giữ.
// @Tags llm
// @Produce json
// @Param slug path string true "Slug provider trong config"
// @Success 200 {object} dtos.ResponseResource
// @Failure 403 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/llm/providers/{slug}/key [delete]
func (a *LLMAPI) ClearKey(c *gin.Context) {
	Render(c, a, a.settings.ClearKey(c, c.Param("slug")))
}

// LoadModels godoc
// @Summary Tải danh sách model từ provider (admin)
// @Description Gọi chính provider (OpenAI-compatible: GET /models, Ollama: GET /api/tags) bằng key đang có hiệu lực, lưu lại rồi trả danh sách đã sắp A→Z.
// @Tags llm
// @Produce json
// @Param slug path string true "Slug provider trong config"
// @Success 200 {object} dtos.ResponseResource
// @Failure 403 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Failure 502 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/llm/providers/{slug}/models [post]
func (a *LLMAPI) LoadModels(c *gin.Context) {
	Render(c, a, a.settings.LoadModels(c, c.Param("slug")))
}

// SelectModel godoc
// @Summary Chọn model mặc định cho provider (admin)
// @Description Model chọn lưu DB và thắng model config cho các lượt chat không ép model.
// @Tags llm
// @Accept json
// @Produce json
// @Param slug path string true "Slug provider trong config"
// @Param body body dtos.SelectLLMModelRequest true "Model mặc định"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 403 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/llm/providers/{slug}/model [put]
func (a *LLMAPI) SelectModel(c *gin.Context) {
	var req dtos.SelectLLMModelRequest
	if !a.Bind(c, &req) {
		return
	}
	Render(c, a, a.settings.SelectModel(c, c.Param("slug"), req.Model))
}
