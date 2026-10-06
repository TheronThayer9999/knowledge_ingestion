package services

import (
	"context"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/controller/dtos"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// presignExpiry là thời hạn sống của URL upload — hết hạn client phải xin lại.
const presignExpiry = 15 * time.Minute

// uploadPrefix là "thư mục ảo" trong bucket, để file upload không trộn với file khác.
const uploadPrefix = "uploads/"

// maxFilenameLen chốt độ dài tên file, tránh key quá to.
const maxFilenameLen = 100

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
	item, err := s.presign(ctx, req.File)
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

	prep := make([]prepared, 0, len(req.Files))
	for _, f := range req.Files {
		name := deref(f.Filename)
		key, mime, err := resolveUpload(name)
		if err != nil {
			return dtos.Fail[*dtos.PresignUploadsResponse](err)
		}
		prep = append(prep, prepared{filename: name, key: key, mime: mime})
	}

	items := make([]dtos.PresignUploadsItem, 0, len(prep))
	for _, p := range prep {
		url, err := s.storage.PresignedURL(ctx, p.key, p.mime, presignExpiry)
		if err != nil {
			return dtos.Fail[*dtos.PresignUploadsResponse](err)
		}
		items = append(items, dtos.PresignUploadsItem{
			Filename:    p.filename,
			UploadURL:   url,
			Key:         p.key,
			Method:      http.MethodPut,
			ContentType: p.mime,
			ExpiresAt:   time.Now().Add(presignExpiry),
		})
	}
	return dtos.Ok(&dtos.PresignUploadsResponse{Items: items})
}

// deref trả "" khi con trỏ nil — binding đã chặn nil trước khi tới service,
// đây chỉ là phòng thủ tránh panic.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

//File *multipart.FileHeader `json:"file"`

// presign sinh key, chốt MIME rồi ký 1 URL, dùng chung cho PresignUpload.
func (s *fileService) presign(ctx context.Context, file *multipart.FileHeader) (*dtos.PresignUploadsItem, error) {
	key, mime, err := resolveUpload(file.Filename)
	if err != nil {
		return nil, err
	}
	url, err := s.storage.PresignedURL(ctx, key, mime, presignExpiry)
	if err != nil {
		return nil, err
	}
	return &dtos.PresignUploadsItem{
		Filename:    file.Filename,
		UploadURL:   url,
		Key:         key,
		Method:      http.MethodPut,
		ContentType: file.Header["Content-Type"][0],
		ExpiresAt:   time.Now().Add(presignExpiry),
	}, nil
}

// resolveUpload validate filename của client rồi trả key + MIME do BE tự suy ra
// từ whitelist allowedExts. Client không gửi/không được gửi content_type — loại
// file bị chặn ngay từ đây nếu không có trong whitelist.
func resolveUpload(filename string) (key string, mime string, err error) {
	key, err = buildUploadKey(filename)
	if err != nil {
		return "", "", err
	}
	ext := fileExt(key)
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
// lọc ký tự lạ, thêm chuỗi ngẫu nhiên để không ghi đè file người khác.
func buildUploadKey(filename string) (string, error) {
	name := strings.TrimSpace(filename)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:] // chỉ giữ phần tên file, bỏ mọi thư mục
	}
	name = sanitize(name)
	name = strings.Trim(name, ".")
	// name toàn dấu . - _ (vd "...", ".   .") cũng coi là rỗng
	if name == "" || strings.Trim(name, "._-") == "" || len(name) > maxFilenameLen {
		return "", errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "filename không hợp lệ")
	}

	ext := fileExt(name)

	id, err := GenerateUUIDv7()
	if err != nil {
		return "", err
	}
	return uploadPrefix + id + ext, nil
}

// fileExt trả về phần mở rộng đã viết thường ("bao-cao.pdf" -> ".pdf"),
// chuỗi không có dấu chấm hoặc toàn dấu chấm thì trả rỗng.
func fileExt(name string) string {
	i := strings.LastIndex(name, ".")
	if i <= 0 || i == len(name)-1 {
		return ""
	}
	ext := name[i:]
	if len(ext) > 11 { // ".tar.gz" là hợp lý, ".quaidaronbaomongquylong" thì không
		return ""
	}
	return strings.ToLower(ext)
}

// sanitize giữ lại chữ, số và . - _ , mọi ký tự khác (khoảng trắng, ký tự lạ,
// dấu phân cách) đều thành "_" để key luôn an toàn cho URL.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// GenerateUUIDv7 sinh ra một UUID version 7 (có thể sắp xếp theo thời gian)
func GenerateUUIDv7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", errors.NewCustomHttpError(http.StatusInternalServerError, errors.Internal, "không sinh được UUIDv7")
	}
	return id.String(), nil
}
