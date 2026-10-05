package dtos

import "time"

// PresignUploadRequest là payload client gửi lên để xin URL upload tạm thời.
// Theo DTO Guidelines ở user_dto.go.
type PresignUploadRequest struct {
	Filename    string `json:"filename" binding:"required,min=1,max=255" example:"bao-cao.pdf"`
	ContentType string `json:"content_type" binding:"required,min=3,max=100" example:"application/pdf"`
}

// PresignUploadResponse trả về "vé" để client tự PUT file thẳng vào kho,
// không đi qua server. Key do server sinh, client không được tự chọn.
type PresignUploadResponse struct {
	UploadURL string    `json:"upload_url" example:"http://localhost:8333/app-uploads/uploads/550e8400e29b41d4a716446655440000.pdf?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=900"`
	Key       string    `json:"key" example:"uploads/550e8400e29b41d4a716446655440000.pdf"`
	Method    string    `json:"method" example:"PUT"`
	ExpiresAt time.Time `json:"expires_at" example:"2026-10-05T12:15:00+07:00"`
}
