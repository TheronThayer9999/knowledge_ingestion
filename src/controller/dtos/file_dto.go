package dtos

import (
	"mime/multipart"
	"time"
)

// PresignUploadRequest là request service nhận — KHÔNG do client gửi thẳng:
// handler đọc file part multipart ("file") rồi gắn nguyên FileHeader vào đây.
// Service tự mở part, sniff magic bytes đối chiếu với đuôi file (đuôi +
// nội dung phải khớp), rồi chốt MIME ký vào URL. Header Content-Type của
// part do client khai nên service lờ hoàn toàn.
type PresignUploadRequest struct {
	File *multipart.FileHeader `json:"file"`
}

// PresignUploadResponse trả về "vé" để client tự PUT file thẳng vào kho,
// không đi qua server. Key do server sinh, client không được tự chọn.
// ContentType do BE chốt và đã nằm trong chữ ký — client PHẢI gửi đúng giá
// trị này khi PUT, sai là 403.
type PresignUploadResponse struct {
	UploadURL   string    `json:"upload_url" example:"http://localhost:8333/app-uploads/uploads/550e8400e29b41d4a716446655440000.pdf?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=900"`
	Key         string    `json:"key" example:"uploads/550e8400e29b41d4a716446655440000.pdf"`
	Method      string    `json:"method" example:"PUT"`
	ContentType string    `json:"content_type" example:"application/pdf"`
	ExpiresAt   time.Time `json:"expires_at" example:"2026-10-05T12:15:00+07:00"`
}

// PresignUploadFile là 1 file trong request batch xin nhiều URL một lần —
// handler gắn FileHeader của từng part, service sniff từng file.
type PresignUploadFile struct {
	File *multipart.FileHeader `json:"file"`
}

// PresignUploadsRequest xin nhiều URL cùng lúc, để client không phải gọi
// /uploads/presign N lần khi upload nhiều file. Tối đa 20 file mỗi request
// (check tay ở service vì tag binding không chạy qua đường multipart).
type PresignUploadsRequest struct {
	Files []*PresignUploadFile `json:"files"`
}

// PresignUploadsItem là URL của 1 file trong batch — Filename là tên gốc
// client gửi, dùng để map lại từng file sau khi upload.
type PresignUploadsItem struct {
	Filename    string    `json:"filename" example:"bao-cao.pdf"`
	UploadURL   string    `json:"upload_url" example:"http://localhost:8333/app-uploads/uploads/550e8400e29b41d4a716446655440000.pdf?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=900"`
	Key         string    `json:"key" example:"uploads/550e8400e29b41d4a716446655440000.pdf"`
	Method      string    `json:"method" example:"PUT"`
	ContentType string    `json:"content_type" example:"application/pdf"`
	ExpiresAt   time.Time `json:"expires_at" example:"2026-10-05T12:15:00+07:00"`
}

// PresignUploadsResponse trả về danh sách URL theo đúng thứ tự files trong request.
type PresignUploadsResponse struct {
	Items []PresignUploadsItem `json:"items"`
}
