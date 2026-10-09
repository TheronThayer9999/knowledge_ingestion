package utils

import (
	"context"
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

// ReadHeadAndSize đọc tối đa SniffHeadLen byte đầu để đoán loại nội dung
// rồi drain phần còn lại ra /dev/null để đo tổng size thật — không buffer
// toàn bộ vào RAM hay đĩa. Dừng ở maxSize+1 byte để bên gọi phát hiện vượt
// ngưỡng sớm thay vì nuốt hết stream. Trả head + tổng size (gồm head).
func ReadHeadAndSize(r io.Reader, maxSize int64) (head []byte, size int64, err error) {
	head, err = ReadHead(r)
	if err != nil {
		return nil, 0, err
	}
	// head đã vượt ngưỡng thì khỏi drain tiếp
	if int64(len(head)) > maxSize {
		return head, int64(len(head)) + 1, nil
	}
	drained, err := io.Copy(io.Discard, io.LimitReader(r, maxSize+1-int64(len(head))))
	if err != nil {
		return nil, 0, err
	}
	return head, int64(len(head)) + drained, nil
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

// traceIDKey khóa ctx mang trace_id — runner (middleware) sinh 1 ID mỗi lượt
// chạy job rồi nhét vào ctx, service tầng dưới chỉ đọc ra gắn vào log. Key
// unexported để không ai ghi đè từ package khác.
type traceIDKey struct{}

// ContextWithTraceID gắn trace_id vào ctx để lan xuống các tầng dưới.
func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

// TraceIDFromCtx đọc trace_id runner đã gắn — trả rỗng nếu ctx không có
// (caller gọi service trực tiếp như trong test thì service tự sinh fallback).
func TraceIDFromCtx(ctx context.Context) string {
	if id, ok := ctx.Value(traceIDKey{}).(string); ok {
		return id
	}
	return ""
}

// Ngưỡng phân trang chung cho các endpoint list.
const (
	DefaultListLimit = 20
	MaxListLimit     = 100
)

// NormalizePagination kẹp limit/offset về ngưỡng an toàn — limit <= 0 thì
// lấy mặc định, vượt trần thì cắt, offset âm thì về 0.
func NormalizePagination(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// Batch cắt slice thành các đợt tối đa size phần tử — dùng cho gửi Ollama
// theo đợt (constants.EMBED_BATCH_SIZE) và INSERT theo đợt (chunkInsertBatch). size <= 0
// thì trả nguyên slice làm 1 đợt để caller lỗi không bao giờ treo/chia 0.
func Batch[T any](items []T, size int) [][]T {
	if len(items) == 0 {
		return nil
	}
	if size <= 0 || size >= len(items) {
		return [][]T{items}
	}
	out := make([][]T, 0, (len(items)+size-1)/size)
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		out = append(out, items[i:end])
	}
	return out
}
