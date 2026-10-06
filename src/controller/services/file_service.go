package services

import (
	"context"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/dtos"
	"net/http"
	"slices"
	"strings"
	"time"
)

// allowedExts là whitelist loại file được upload — BE chốt, không tin content_type
// client gửi (client khai bừa cũng không ảnh hưởng vì MIME gắn với phần mở rộng).
// Key là phần mở rộng đã viết thường, value là MIME do BE gán và nhét vào chữ ký.
var allowedExts = map[string]string{
	".pdf":  "application/pdf",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".txt":  "text/plain",
	".md":   "text/markdown",
	".csv":  "text/csv",
	".json": "application/json",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".zip":  "application/zip",
}

type IFileService interface {
	PresignUpload(ctx context.Context, req *dtos.PresignUploadRequest) dtos.Result[*dtos.PresignUploadResponse]
	PresignUploads(ctx context.Context, req *dtos.PresignUploadsRequest) dtos.Result[*dtos.PresignUploadsResponse]
}

type fileService struct {
	storage storage.IStorage
}

func NewFileService(st storage.IStorage) IFileService {
	return &fileService{storage: st}
}

// PresignUpload sinh key mới (client không được tự chọn key — chống path
// traversal và ghi đè), ký URL PUT tạm thời, trả về cho client tự upload.
func (s *fileService) PresignUpload(ctx context.Context, req *dtos.PresignUploadRequest) dtos.Result[*dtos.PresignUploadResponse] {
	item, err := s.presign(ctx, req.Filename, req.Head, req.Size)
	if err != nil {
		return dtos.Fail[*dtos.PresignUploadResponse](err)
	}
	return dtos.Ok(&dtos.PresignUploadResponse{
		UploadURL:   item.UploadURL,
		Key:         item.Key,
		Method:      item.Method,
		ContentType: item.ContentType,
		ExpiresAt:   item.ExpiresAt,
	})
}

// PresignUploads ký nhiều URL trong 1 lần gọi. Validate toàn bộ filename trước,
// rồi mới ký — tránh 400 nửa chừng làm phí một lượt ký. Kết quả theo đúng thứ
// tự files trong request để client map lại từng file.
func (s *fileService) PresignUploads(ctx context.Context, req *dtos.PresignUploadsRequest) dtos.Result[*dtos.PresignUploadsResponse] {
	type prepared struct{ filename, key, mime string }

	if len(req.Files) == 0 {
		return dtos.Fail[*dtos.PresignUploadsResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "thiếu file đính kèm"))
	}
	if len(req.Files) > constants.MAX_UPLOADS_PER_REQUEST {
		return dtos.Fail[*dtos.PresignUploadsResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "tối đa 20 file mỗi request"))
	}

	prep := make([]prepared, 0, len(req.Files))
	for _, f := range req.Files {
		if f == nil {
			return dtos.Fail[*dtos.PresignUploadsResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "thiếu file đính kèm"))
		}
		key, mime, err := resolveUploadContent(f.Filename, f.Head, f.Size)
		if err != nil {
			return dtos.Fail[*dtos.PresignUploadsResponse](err)
		}
		prep = append(prep, prepared{filename: f.Filename, key: key, mime: mime})
	}

	items := make([]dtos.PresignUploadsItem, 0, len(prep))
	for _, p := range prep {
		url, err := s.storage.PresignedURL(ctx, p.key, p.mime, constants.PRESIGN_EXPIRY)
		if err != nil {
			return dtos.Fail[*dtos.PresignUploadsResponse](err)
		}
		items = append(items, dtos.PresignUploadsItem{
			Filename:    p.filename,
			UploadURL:   url,
			Key:         p.key,
			Method:      http.MethodPut,
			ContentType: p.mime,
			ExpiresAt:   time.Now().Add(constants.PRESIGN_EXPIRY),
		})
	}
	return dtos.Ok(&dtos.PresignUploadsResponse{Items: items})
}

// presign chốt MIME từ tên + head sniff được (đuôi + magic bytes phải khớp),
// chặn file vượt MAX_FILE_SIZE, sinh key rồi ký 1 URL, dùng chung cho PresignUpload.
func (s *fileService) presign(ctx context.Context, filename string, head []byte, size int64) (*dtos.PresignUploadsItem, error) {
	key, mime, err := resolveUploadContent(filename, head, size)
	if err != nil {
		return nil, err
	}
	url, err := s.storage.PresignedURL(ctx, key, mime, constants.PRESIGN_EXPIRY)
	if err != nil {
		return nil, err
	}
	return &dtos.PresignUploadsItem{
		Filename:    filename,
		UploadURL:   url,
		Key:         key,
		Method:      http.MethodPut,
		ContentType: mime,
		ExpiresAt:   time.Now().Add(constants.PRESIGN_EXPIRY),
	}, nil
}

// sniffAllow là các MIME nội dung chấp nhận được cho từng đuôi file, vì
// utils.SniffContentType không phân biệt được họ hàng nhà zip (.docx/.xlsx/
// .pptx đều sniff ra application/zip) và text thật vẫn sniff ra text/plain.
// Đây là policy nghiệp vụ nên ở lại service, không vào utils.
var sniffAllow = map[string][]string{
	".docx": {"application/zip"},
	".xlsx": {"application/zip"},
	".pptx": {"application/zip"},
	".txt":  {"text/plain"},
	".md":   {"text/plain", "text/markdown"},
	".csv":  {"text/plain", "text/csv"},
	".json": {"text/plain", "application/json"},
}

// resolveUploadContent validate filename rồi đối chiếu loại nội dung sniff từ
// bytes thật với MIME của đuôi file — .exe đổi tên .pdf bị chặn ở đây vì
// sniff ra application/octet-stream. size là tổng dung lượng handler đo trên
// stream — vượt MAX_FILE_SIZE thì 400 ngay, chưa tới bước ký URL.
// Trả key + MIME đã verify để ký.
func resolveUploadContent(filename string, head []byte, size int64) (key string, mime string, err error) {
	if size <= 0 {
		return "", "", errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "file rỗng")
	}
	if size > constants.MAX_FILE_SIZE {
		return "", "", errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "mỗi file tối đa 500MB")
	}
	if len(head) == 0 {
		return "", "", errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "file rỗng")
	}
	key, mime, err = resolveUpload(filename)
	if err != nil {
		return "", "", err
	}
	sniffed := utils.SniffContentType(head)
	if sniffed == mime {
		return key, mime, nil
	}
	if slices.Contains(sniffAllow[utils.FileExt(key)], sniffed) {
		return key, mime, nil
	}
	return "", "", errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "nội dung file không khớp loại file: "+sniffed)
}

// resolveUpload validate filename của client rồi trả key + MIME do BE tự suy ra
// từ whitelist allowedExts. Client không gửi/không được gửi content_type — loại
// file bị chặn ngay từ đây nếu không có trong whitelist.
func resolveUpload(filename string) (key string, mime string, err error) {
	key, err = buildUploadKey(filename)
	if err != nil {
		return "", "", err
	}
	ext := utils.FileExt(key)
	m, ok := allowedExts[ext]
	if !ok {
		label := ext
		if label == "" {
			label = "không có"
		}
		return "", "", errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "loại file không được hỗ trợ: "+label)
	}
	return key, m, nil
}

// buildUploadKey biến filename của client thành key an toàn: bỏ mọi đường dẫn,
// lọc ký tự lạ (utils), thêm UUIDv7 để không ghi đè file người khác.
func buildUploadKey(filename string) (string, error) {
	name := strings.TrimSpace(filename)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:] // chỉ giữ phần tên file, bỏ mọi thư mục
	}
	name = utils.SanitizeFileName(name)
	name = strings.Trim(name, ".")
	// name toàn dấu . - _ (vd "...", ".   .") cũng coi là rỗng
	if name == "" || strings.Trim(name, "._-") == "" || len(name) > constants.MAX_FILENAME_LEN {
		return "", errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "filename không hợp lệ")
	}

	ext := utils.FileExt(name)

	id, err := utils.NewUUIDv7()
	if err != nil {
		return "", errors.NewCustomHttpError(http.StatusInternalServerError, errors.Internal, "không sinh được key upload")
	}
	return constants.UPLOAD_PREFIX + id + ext, nil
}
