package scanocr

import (
	"context"
	"errors"
	"fmt"
)

// Các lỗi chuẩn của package. Service map sang lỗi HTTP tiếng Việt ở tầng
// controller — engine không tự soạn thông điệp cho người dùng.
var (
	// ErrDisabled: tính năng chưa bật trên máy chủ (scan_ocr.enabled = false).
	ErrDisabled = errors.New("scanocr: tính năng OCR tài liệu scan đang tắt")
	// ErrEngineUnknown: cấu hình trỏ tên engine không có trong registry.
	ErrEngineUnknown = errors.New("scanocr: không nhận ra engine đã cấu hình")
	// ErrEngineUnavailable: engine có thật nhưng máy này chưa chạy được (thiếu
	// binary, thiếu tessdata). Trả lúc dựng job để người dùng biết sớm thay vì
	// thấy 200 trang cùng fail.
	ErrEngineUnavailable = errors.New("scanocr: engine OCR chưa sẵn sàng trên máy chủ")
	// ErrNoText: OCR chạy xong nhưng không đọc ra chữ nào (ảnh trắng/mờ). Với
	// tính năng này đây KHÔNG phải lỗi hệ thống — trang vẫn lưu, chỉ báo 0 từ.
	ErrNoText = errors.New("scanocr: không đọc được chữ nào trong ảnh")
)

// ImageInfo mô tả ảnh đưa vào OCR. WidthPx/HeightPx bắt buộc đúng vì box của
// engine tính theo pixel của CHÍNH ảnh này — sai một là lớp chữ vô hình lệch
// khỏi chữ trong ảnh (Ctrl+F ra đúng đoạn nhưng bôi đen lại nhảy chỗ khác).
type ImageInfo struct {
	// Page là số trang thật trong PDF (1-based); ảnh đơn thì 1.
	Page      int
	WidthPx   int
	HeightPx  int
	DPI       int
	Extension string
}

// Engine là cổng OCR của tính năng tab Quét. Hợp đồng tối thiểu: từng từ kèm box
// pixel và confidence.
//
// Vì sao phải là interface: tài liệu của tính năng này là CHỮ IN, mà OCR chữ in
// là bài toán có rất nhiều engine làm được (Tesseract, PaddleOCR, RapidOCR,
// docTR, VietOCR, các model VLM OCR, cloud). Cột chặt vào một engine thì ngày
// khách cần tiếng Việt đẹp hơn là phải sửa cả UI lẫn tầng dựng PDF. Để đổi engine
// chỉ cần: viết adapter trả đúng kiểu Page này + thêm một dòng trong defaultEngines().
type Engine interface {
	// Name là khoá để cấu hình scan_ocr.engine và để ghi vào DB (đọc lại một
	// trang cũ vẫn biết nó do engine nào sinh ra — các engine cho box khác nhau).
	Name() string

	// Available kiểm tra engine chạy được trên máy này không. Trả lỗi có nội dung
	// gọn để service escalate thành ErrEngineUnavailable.
	Available(ctx context.Context) error

	// Recognize đọc chữ in trên MỘT ảnh. data là nguyên văn bytes ảnh PNG/JPEG.
	// Trả Page với Box tính theo pixel của ảnh trong ImageInfo.
	Recognize(ctx context.Context, img ImageInfo, data []byte) (*Page, error)
}

// registry là danh sách engine tính năng này dùng. Thêm engine = thêm file adapter
// rồi khai báo ở đây, không sửa service/API/UI/PDF (cùng triết lý với
// extractor.defaultReaders cho các định dạng tệp).
//
// Engine nào cũng được dựng từ Config; engine chưa cấu hình đủ thì trả lỗi lúc
// NewEngine chứ không âm thầm rơi về tesseract — im lặng đổi engine là cách PDF
// của người dùng tự nhiên kém chất lượng mà không ai biết vì sao.
func defaultEngines(cfg Config) map[string]func(Config) Engine {
	return map[string]func(Config) Engine{
		EngineTesseract: func(c Config) Engine { return newTesseractEngine(c) },
	}
}

// EngineTesseract là tên adapter Tesseract trong registry.
const EngineTesseract = "tesseract"

// NewEngine dựng engine theo tên đã cấu hình.
func NewEngine(cfg Config) (Engine, error) {
	if !cfg.Enabled {
		return nil, ErrDisabled
	}
	normalized := cfg.Normalize()
	for name, build := range defaultEngines(normalized) {
		if name == normalized.Engine {
			return build(normalized), nil
		}
	}
	known := make([]string, 0, 4)
	for name := range defaultEngines(normalized) {
		known = append(known, name)
	}
	return nil, fmt.Errorf("%w: %q (danh sách: %v)", ErrEngineUnknown, normalized.Engine, known)
}
