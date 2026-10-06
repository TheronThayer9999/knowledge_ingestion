package utils

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// SniffHeadLen là số byte đầu đọc từ file để đoán loại nội dung —
// http.DetectContentType cũng chỉ xét tối đa 512 byte đầu.
const SniffHeadLen = 512

// SanitizeFileName giữ lại chữ, số và . - _ , mọi ký tự khác (khoảng trắng,
// ký tự lạ, dấu phân cách) đều thành "_" để tên file luôn an toàn cho URL
// và key trong kho.
func SanitizeFileName(s string) string {
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

// FileExt trả về phần mở rộng đã viết thường ("bao-cao.pdf" -> ".pdf").
// Chuỗi không có dấu chấm, toàn dấu chấm, hoặc đuôi quá dài thì trả rỗng.
func FileExt(name string) string {
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

// SniffContentType đoán MIME từ magic bytes bằng http.DetectContentType rồi
// cắt bỏ tham số "; charset=..." (vd "text/plain; charset=utf-8" -> "text/plain")
// để so sánh thẳng với MIME kỳ vọng.
func SniffContentType(head []byte) string {
	sniffed := http.DetectContentType(head)
	if i := strings.Index(sniffed, ";"); i >= 0 {
		sniffed = strings.TrimSpace(sniffed[:i])
	}
	return sniffed
}

// ReadHead đọc tối đa SniffHeadLen byte đầu từ r (thường là part đang stream)
// để đoán loại nội dung — không buffer cả file vào RAM hay đĩa. Trả plain
// error để tầng gọi tự map sang mã lỗi của nó (service map lỗi đọc thành
// 500 internal).
func ReadHead(r io.Reader) ([]byte, error) {
	buf := make([]byte, SniffHeadLen)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("đọc file đính kèm: %w", err)
	}
	return buf[:n], nil
}

// NewUUIDv7 sinh chuỗi UUID version 7 (có thể sắp xếp theo thời gian) để làm
// tên duy nhất chống ghi đè. Trả plain error để tầng gọi tự map mã lỗi.
func NewUUIDv7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("sinh UUIDv7: %w", err)
	}
	return id.String(), nil
}
