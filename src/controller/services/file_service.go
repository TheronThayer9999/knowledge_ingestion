package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/controller/dtos"
	"net/http"
	"strings"
	"time"
)

// presignExpiry là thời hạn sống của URL upload — hết hạn client phải xin lại.
const presignExpiry = 15 * time.Minute

// uploadPrefix là "thư mục ảo" trong bucket, để file upload không trộn với file khác.
const uploadPrefix = "uploads/"

// maxFilenameLen chốt độ dài tên file, tránh key quá to.
const maxFilenameLen = 100

type IFileService interface {
	PresignUpload(ctx context.Context, req *dtos.PresignUploadRequest) dtos.Result[*dtos.PresignUploadResponse]
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
	key, err := buildUploadKey(req.Filename)
	if err != nil {
		return dtos.Fail[*dtos.PresignUploadResponse](err)
	}

	url, err := s.storage.PresignedURL(ctx, key, presignExpiry)
	if err != nil {
		return dtos.Fail[*dtos.PresignUploadResponse](err)
	}

	return dtos.Ok(&dtos.PresignUploadResponse{
		UploadURL: url,
		Key:       key,
		Method:    http.MethodPut,
		ExpiresAt: time.Now().Add(presignExpiry),
	})
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

	id, err := randomHex(16)
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

// randomHex sinh chuỗi hex ngẫu nhiên làm tên file duy nhất trong bucket.
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", errors.NewCustomHttpError(http.StatusInternalServerError, errors.Internal, "không sinh được key upload")
	}
	return hex.EncodeToString(buf), nil
}
