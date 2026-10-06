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
// @Description Signs a temporary PUT URL so the client can upload the file straight to storage without going through this server. The file itself is NOT stored — only its name is read from the multipart part to derive the key and content type
// @Tags files
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "file whose name is used for the presign (body is never stored)"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Router /api/v1/uploads/presign [post]
func (a *FileAPI) PresignUpload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		a.BadRequest(c, "No file is uploaded")
		return
	}
	req := &dtos.PresignUploadRequest{
		File: file,
	}

	Render(c, a, a.svc.PresignUpload(c, req))
}

// PresignUploads godoc
// @Summary Get presigned upload URLs for multiple files
// @Description Signs temporary PUT URLs for up to 20 files in one call, returned in the same order as the request. Files are NOT stored — only their names are read from the multipart parts to derive keys and content types
// @Tags files
// @Accept multipart/form-data
// @Produce json
// @Param files formData file true "files to presign (field name \"files\", up to 20)"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Router /api/v1/uploads/presign/batch [post]
func (a *FileAPI) PresignUploads(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		a.BadRequest(c, "No files are uploaded")
		return
	}
	uploads := form.File["files"]
	if len(uploads) == 0 {
		a.BadRequest(c, "No files are uploaded")
		return
	}
	req := &dtos.PresignUploadsRequest{
		Files: make([]*dtos.PresignUploadFile, 0, len(uploads)),
	}
	for _, f := range uploads {
		name := f.Filename
		req.Files = append(req.Files, &dtos.PresignUploadFile{Filename: &name})
	}
	if err := a.validateRequest(req); err != nil {
		a.BadRequest(c, err.Error())
		return
	}
	Render(c, a, a.svc.PresignUploads(c, req))
}
