package apis

import (
	"io"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"
	"mime/multipart"

	"github.com/gin-gonic/gin"
)

type FileAPI struct {
	*baseController
	svc services.IFileService
}

func NewFileAPI(base *baseController, svc services.IFileService) *FileAPI {
	return &FileAPI{baseController: base, svc: svc}
}

// readHeadAndSize đọc 512B đầu để sniff rồi drain phần còn lại ra /dev/null
// để đo tổng size thật — không buffer file vào RAM hay đĩa. Dừng ở
// MAX_FILE_SIZE+1 byte để file vượt ngưỡng 400 sớm thay vì nuốt hết stream.
func readHeadAndSize(part *multipart.Part) (head []byte, size int64, err error) {
	head, err = utils.ReadHead(part)
	if err != nil {
		return nil, 0, err
	}
	// head đã vượt ngưỡng thì khỏi drain tiếp
	if int64(len(head)) > constants.MAX_FILE_SIZE {
		return head, int64(len(head)) + 1, nil
	}
	drained, err := io.Copy(io.Discard, io.LimitReader(part, constants.MAX_FILE_SIZE+1-int64(len(head))))
	if err != nil {
		return nil, 0, err
	}
	return head, int64(len(head)) + drained, nil
}

// PresignUpload godoc
// @Summary Get a presigned upload URL
// @Description Signs a temporary PUT URL so the client can upload the file straight to storage without going through this server. Reads the multipart stream part by part — only the file name and first 512 sniff bytes are kept, the rest is never buffered or stored
// @Tags files
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "file whose name is used for the presign (body is never stored)"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Router /api/v1/uploads/presign [post]
func (a *FileAPI) PresignUpload(c *gin.Context) {
	mr, err := c.Request.MultipartReader()
	if err != nil {
		a.BadRequest(c, "No file is uploaded")
		return
	}
	for seen := 0; ; seen++ {
		if seen >= constants.MAX_STREAM_PARTS {
			a.BadRequest(c, "quá nhiều part trong request")
			return
		}
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			a.BadRequest(c, "No file is uploaded")
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			continue
		}
		head, size, err := readHeadAndSize(part)
		if err != nil {
			a.BadRequest(c, "không đọc được file đính kèm")
			return
		}
		if size > constants.MAX_FILE_SIZE {
			a.BadRequest(c, "mỗi file tối đa 500MB")
			return
		}
		Render(c, a, a.svc.PresignUpload(c, &dtos.PresignUploadRequest{
			Filename: part.FileName(),
			Head:     head,
			Size:     size,
		}))
		return // đủ tên + head để ký — không đọc tiếp body, Go tự đóng connection
	}
	a.BadRequest(c, "No file is uploaded")
}

// PresignUploads godoc
// @Summary Get presigned upload URLs for multiple files
// @Description Signs temporary PUT URLs for up to 20 files in one call, returned in the same order as the request. Reads the multipart stream part by part — only file names and first 512 sniff bytes are kept, the rest is never buffered or stored
// @Tags files
// @Accept multipart/form-data
// @Produce json
// @Param files formData file true "files to presign (field name \"files\", up to 20)"
// @Success 200 {object} dtos.ResponseResource
// @Failure 400 {object} dtos.ResponseResource
// @Failure 500 {object} dtos.ResponseResource
// @Router /api/v1/uploads/presign/batch [post]
func (a *FileAPI) PresignUploads(c *gin.Context) {
	mr, err := c.Request.MultipartReader()
	if err != nil {
		a.BadRequest(c, "No files are uploaded")
		return
	}
	req := &dtos.PresignUploadsRequest{}
	for seen := 0; ; seen++ {
		if seen >= constants.MAX_STREAM_PARTS {
			a.BadRequest(c, "quá nhiều part trong request")
			return
		}
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			a.BadRequest(c, "No files are uploaded")
			return
		}
		if part.FormName() != "files" || part.FileName() == "" {
			continue
		}
		head, size, err := readHeadAndSize(part)
		if err != nil {
			a.BadRequest(c, "không đọc được file đính kèm")
			return
		}
		if size > constants.MAX_FILE_SIZE {
			a.BadRequest(c, "mỗi file tối đa 500MB")
			return
		}
		req.Files = append(req.Files, &dtos.PresignUploadFile{
			Filename: part.FileName(),
			Head:     head,
			Size:     size,
		})
		if len(req.Files) > constants.MAX_UPLOADS_PER_REQUEST {
			a.BadRequest(c, "tối đa 20 file mỗi request")
			return
		}
	}
	if len(req.Files) == 0 {
		a.BadRequest(c, "No files are uploaded")
		return
	}
	Render(c, a, a.svc.PresignUploads(c, req))
}
