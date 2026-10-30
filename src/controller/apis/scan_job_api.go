package apis

import (
	"strconv"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// ScanJobAPI phục vụ hàng đợi OCR quét (tab Quét): tạo job, theo dõi tiến độ,
// huỷ/xoá, OCR lại 1 trang, xem 1 trang và lấy URL tệp kết quả.
type ScanJobAPI struct {
	*baseController
	svc services.IScanJobService
}

func NewScanJobAPI(base *baseController, svc services.IScanJobService) *ScanJobAPI {
	return &ScanJobAPI{baseController: base, svc: svc}
}

// Create godoc
// @Summary Create a scan OCR job from an uploaded file
// @Tags scan
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "image or PDF"
// @Success 200 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/scan/jobs [post]
func (a *ScanJobAPI) Create(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		a.BadRequest(c, "thieu file upload")
		return
	}
	opened, err := file.Open()
	if err != nil {
		a.BadRequest(c, "khong doc duoc file")
		return
	}
	defer func() { _ = opened.Close() }()

	var req dtos.CreateScanJobRequest
	if err := c.ShouldBind(&req); err != nil {
		a.BadRequest(c, "tham so khong hop le")
		return
	}
	Render(c, a, a.svc.Create(c.Request.Context(), file.Filename, opened, req))
}

// Get godoc
// @Summary Get scan job progress
// @Tags scan
// @Produce json
// @Param id path int true "job id"
// @Success 200 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/scan/jobs/{id} [get]
func (a *ScanJobAPI) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		a.BadRequest(c, "id khong hop le")
		return
	}
	Render(c, a, a.svc.Get(c.Request.Context(), id))
}

// List godoc
// @Summary List scan jobs of current user
// @Tags scan
// @Produce json
// @Param status query string false "status filter"
// @Param limit query int false "limit"
// @Param offset query int false "offset"
// @Success 200 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/scan/jobs [get]
func (a *ScanJobAPI) List(c *gin.Context) {
	status := c.Query("status")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	Render(c, a, a.svc.List(c.Request.Context(), status, limit, offset))
}

// Cancel godoc
// @Summary Cancel a scan job
// @Tags scan
// @Produce json
// @Param id path int true "job id"
// @Success 200 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/scan/jobs/{id}/cancel [post]
func (a *ScanJobAPI) Cancel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		a.BadRequest(c, "id khong hop le")
		return
	}
	Render(c, a, a.svc.Cancel(c.Request.Context(), id))
}

// Delete godoc
// @Summary Delete a scan job
// @Tags scan
// @Produce json
// @Param id path int true "job id"
// @Success 200 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/scan/jobs/{id} [delete]
func (a *ScanJobAPI) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		a.BadRequest(c, "id khong hop le")
		return
	}
	Render(c, a, a.svc.Delete(c.Request.Context(), id))
}

// RetryPage godoc
// @Summary Re-OCR one page
// @Tags scan
// @Produce json
// @Param id path int true "job id"
// @Param page path int true "page number"
// @Success 200 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/scan/jobs/{id}/pages/{page}/retry [post]
func (a *ScanJobAPI) RetryPage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		a.BadRequest(c, "id khong hop le")
		return
	}
	page, err := strconv.Atoi(c.Param("page"))
	if err != nil {
		a.BadRequest(c, "page khong hop le")
		return
	}
	Render(c, a, a.svc.RetryPage(c.Request.Context(), id, page))
}

// GetPage godoc
// @Summary Get one OCR page with words, confidence and rendered image
// @Description Returns the per-page OCR result for the review tab: normalized word boxes (0..1), confidence per word, page text and a presigned URL of the rendered page image (rendered lazily and cached on first view). Scoped to the job owner.
// @Tags scan
// @Produce json
// @Param id path int true "job id"
// @Param page path int true "page number (1-based)"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 404 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/scan/jobs/{id}/pages/{page} [get]
func (a *ScanJobAPI) GetPage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		a.BadRequest(c, "id khong hop le")
		return
	}
	page, err := strconv.Atoi(c.Param("page"))
	if err != nil {
		a.BadRequest(c, "page khong hop le")
		return
	}
	Render(c, a, a.svc.GetPage(c.Request.Context(), id, page))
}

// DownloadURL godoc
// @Summary Get presigned URL for the result file (PDF)
// @Tags scan
// @Produce json
// @Param id path int true "job id"
// @Success 200 {object} dtos.ResponseResource
// @Security BearerAuth
// @Router /api/v1/scan/jobs/{id}/download [get]
func (a *ScanJobAPI) DownloadURL(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		a.BadRequest(c, "id khong hop le")
		return
	}
	Render(c, a, a.svc.DownloadURL(c.Request.Context(), id))
}
