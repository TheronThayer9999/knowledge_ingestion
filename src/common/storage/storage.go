package storage

import (
	"context"
	"io"
	"time"
)

// IStorage mô tả hợp đồng với một kho file blob — SeaweedFS, AWS S3, MinIO
// hay lưu local đều cài được. Code nghiệp vụ chỉ phụ thuộc interface này nên
// đổi backing store không phải sửa nghiệp vụ. key trong mọi method là đường
// dẫn file trong bucket (vd "uploads/2026/abc.pdf"), không phải URL.
type IStorage interface {
	// Upload ghi stream file vào bucket tại key (server tự lưu — luồng 1).
	// size phải là dung lượng thật của r vì S3 bắt buộc có Content-Length;
	// contentType rỗng thì không đặt MIME type. Trả nil khi ghi thành công.
	Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error

	// Download trả về stream nội dung file tại key. Caller phải Close() kết quả
	// để trả kết nối về pool. Trả error nếu key không tồn tại hoặc kho chết.
	Download(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete xóa vĩnh viễn file tại key. S3 báo thành công cả khi key không tồn tại.
	Delete(ctx context.Context, key string) error

	// PresignedURL ký một URL PUT có hạn expiry để client upload thẳng vào kho
	// mà không đi qua server — luồng 2. Không có I/O mạng, chỉ tính HMAC cục bộ.
	// contentType được nhét vào chữ ký: client PUT mà gửi Content-Type khác
	// (hoặc không gửi) thì S3 trả 403. Hết expiry thì URL vô hiệu; file chỉ
	// tồn tại sau khi client PUT thành công.
	PresignedURL(ctx context.Context, key string, contentType string, expiry time.Duration) (string, error)
}
