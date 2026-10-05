package apis

import (
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

type FileAPI struct {
	*baseController
	svc services.IFileService
}

func NewFileAPI(base *baseController, svc services.IFileService) *FileAPI {
	return &FileAPI{baseController: base, svc: svc}
}

// PresignUpload godoc
// @Summary Get a presigned upload URL
// @Description Signs a temporary PUT URL so the client can upload the file straight to storage without going through this server
// @Tags files
// @Accept json
// @Produce json
// @Param request body dtos.PresignUploadRequest true "upload payload"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Router /api/v1/uploads/presign [post]
func (a *FileAPI) PresignUpload(c *gin.Context) {
	req := &dtos.PresignUploadRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.PresignUpload(c, req))
}

// PresignUploads godoc
// @Summary Get presigned upload URLs for multiple files
// @Description Signs temporary PUT URLs for up to 20 files in one call, returned in the same order as the request
// @Tags files
// @Accept json
// @Produce json
// @Param request body dtos.PresignUploadsRequest true "batch upload payload"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Router /api/v1/uploads/presign/batch [post]
func (a *FileAPI) PresignUploads(c *gin.Context) {
	req := &dtos.PresignUploadsRequest{}
	if !a.Bind(c, req) {
		return
	}
	Render(c, a, a.svc.PresignUploads(c, req))
}
