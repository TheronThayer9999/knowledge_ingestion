package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Sentinel lỗi của tầng đọc ảnh. Khai báo ở domain (không phải ở infra) vì
// consumer cần phân biệt "ảnh hỏng" với "backend chết" mà không được import
// package infra — cùng lý do domain.Embedder có IsPermanentError.
var (
	// ErrImageEmpty: ảnh rỗng — lỗi lập trình, không phải lỗi mạng.
	ErrImageEmpty = errors.New("vlm: ảnh rỗng")
	// ErrImageTooLarge: ảnh vượt trần payload; đổi model/DPI chứ retry vô nghĩa.
	ErrImageTooLarge = errors.New("vlm: ảnh vượt trần kích thước")
	// ErrImageFormat: MIME không hỗ trợ (chỉ nhận png/jpeg).
	ErrImageFormat = errors.New("vlm: định dạng ảnh không hỗ trợ")
)

// mimeChoPhep là tập MIME model ảnh thực sự nhận. Để hẹp có chủ đích: renderer
// nội bộ chỉ sinh ra 2 loại này (pdftoppm → png), nhận thêm webp/gif chỉ mở
// đường cho dữ liệu lạ.
var mimeChoPhep = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
}

// ImageRef là VALUE OBJECT một ảnh trang tài liệu đi vào model.
//
// VÌ SAO KHÔNG truyền []byte trần: một trang 300 DPI là ~1-3MB, truyền bytes
// qua 3 tầng thì mỗi tầng lại phải tự kiểm "có rỗng không, MIME gì". Gom vào
// đây để (a) kiểm đúng một lần, (b) chỗ dùng nhận type rõ nghĩa thay vì
// (page int, mime string, data []byte).
//
// Field đóng: tạo được ImageRef nghĩa là ảnh đã hợp lệ, nên reader phía sau
// không cần kiểm lại rỗng/MIME nữa.
type ImageRef struct {
	pageNum int
	mime    string
	data    []byte
}

// NewImageRef kiểm và đóng gói ảnh. pageNum <= 0 được phép (ảnh không gắn
// trang, ví dụ ảnh cắt riêng một bảng) — trích dẫn khi đó dùng số trang của
// chunk chứa nó.
func NewImageRef(pageNum int, mime string, data []byte) (ImageRef, error) {
	if len(data) == 0 {
		return ImageRef{}, ErrImageEmpty
	}
	m := strings.ToLower(strings.TrimSpace(mime))
	if _, ok := mimeChoPhep[m]; !ok {
		return ImageRef{}, fmt.Errorf("%w: %q", ErrImageFormat, mime)
	}
	return ImageRef{pageNum: pageNum, mime: m, data: data}, nil
}

// PageNum trả số trang gốc (0 = ảnh không gắn trang cụ thể).
func (i ImageRef) PageNum() int { return i.pageNum }

// MIME trả MIME đã chuẩn hoá lowercase.
func (i ImageRef) MIME() string { return i.mime }

// Bytes trả slice ảnh GỐC, không copy: copy thêm 2-3MB mỗi trang là lý do
// RAM phình khi đọc song song nhiều trang.
func (i ImageRef) Bytes() []byte { return i.data }

// Size trả kích thước ảnh tính bằng byte.
func (i ImageRef) Size() int { return len(i.data) }

// IsZero cho biết có phải giá trị rỗng (chưa khởi tạo) — dùng để chặn trước
// khi gọi backend, tránh gửi request chắc chắn fail.
func (i ImageRef) IsZero() bool { return len(i.data) == 0 }

// Reading là kết quả đọc một trang. Chỉ có text: mọi thứ khác (độ tin cậy,
// toạ độ, bảng) là việc của tầng trích — model ảnh không đáng tin để tự báo
// độ tin cậy của chính nó.
type Reading struct {
	Text  string
	Model string
}

// IVLMReader là VAI của pipeline extract: đọc cả trang thành markdown để thay
// cho text layer/OCR khi trang phức tạp.
type IVLMReader interface {
	// Transcribe đọc cả trang. hint là gợi ý hẹp cho trang đó (ví dụ "trang
	// này có bảng 5 cột"), rỗng thì dùng prompt mặc định.
	Transcribe(ctx context.Context, img ImageRef, hint string) (Reading, error)
	// ModelName trả tên model đang dùng — để log/trace biết bản trích do model
	// nào đọc, cần khi so sánh chất lượng giữa các bản.
	ModelName() string
	// IsPermanentError báo lỗi có retry cũng vậy không (ảnh hỏng, 4xx).
	IsPermanentError(err error) bool
}

// IVLMAsker là VAI của agent: hỏi một câu cụ thể về ảnh. Tách khỏi
// IVLMReader theo ISP — tool của agent không cần và không được thấy
// Transcribe (một call Transcribe cả trang đắt gấp nhiều lần một câu hỏi).
type IVLMAsker interface {
	Ask(ctx context.Context, img ImageRef, question string) (string, error)
}
