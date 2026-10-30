package scanocr

import (
	"fmt"
	"strings"
	"time"
)

// Config là cấu hình engine OCR của tính năng tab Quét. Nằm ở package này (không
// phải package config) để scanocr không phụ thuộc ngược vào config — cùng khuôn
// với extractor.OCRConfig của đường nạp kho. Service build struct này từ
// config.GetScanOCR() một lần lúc boot.
type Config struct {
	Enabled bool
	// Engine là tên adapter trong registry (engine.go) — mặc định "tesseract".
	// Đây là núm duy nhất để đổi engine: UI, bảng words, PDF chỉ nhìn hợp đồng
	// Engine nên không chỗ nào khác phải sửa theo.
	Engine string
	Binary string
	Lang   string
	PSM    int
	// Timeout là trần cho MỘT lần gọi engine; PageTimeout là trần cho cả một
	// trang (render + OCR) khi worker tự đặt deadline riêng.
	Timeout     time.Duration
	PageTimeout time.Duration

	// RenderBinary là đường PDF→ảnh (poppler pdftoppm) dành riêng cho tính năng
	// này, tách khỏi đường nạp kho để hai bên chỉnh không ảnh hưởng nhau.
	RenderBinary string
	// RenderDPI là độ phân giải ảnh đưa vào OCR — 300 là điểm ngọt Tesseract.
	RenderDPI int
	// PreviewDPI là độ phân giải ảnh FE dùng để hiển thị và vẽ vùng. 150 đủ nét
	// mà nhẹ hơn 300 khoảng 4 lần; box đã chuẩn hoá 0..1 nên đổi DPI không làm
	// lệch vị trí.
	PreviewDPI int

	// TextLayerMinChars là ngưỡng "trang này ĐÃ có chữ", tính theo rune: trang có
	// số ký tự in được dưới ngưỡng mới OCR.
	//
	// KHÁC cách autoai làm: nó chỉ hỏi "file có trang nào mang chữ không" nên PDF
	// 1 trang số + 199 trang scan bị bỏ qua toàn bộ (rag/ocr_tools.py:124-136).
	// Ngưỡng phải áp cho TỪNG trang.
	TextLayerMinChars int
	// MinConfWord là ngưỡng dưới: từ có confidence thấp hơn bị đánh dấu đáng ngờ.
	// Đo trên fixture tiếng Việt: trung bình 91,8, 8/387 từ <70 (≈2%).
	MinConfWord int

	// MaxAttempts là retry theo TRANG (không theo job) — một trang mờ không được
	// kéo cả 200 trang làm lại.
	MaxAttempts int
	// Parallel là số trang chạy đồng thời trong một worker (semaphore).
	Parallel int
}

// Normalize kẹp các giá trị vô lý về mặc định an toàn. Hàm thuần, test được.
//
// Tách hai chặng (engine + renderer/ngưỡng) chỉ để mỗi hàm đọc được trong một
// màn hình theo ngân sách của repo — hành vi là một dãy "rỗng thì điền", không
// có thứ tự phụ thuộc nào ngoài PageTimeout đọc Timeout ở chặng đầu.
func (c Config) Normalize() Config {
	out := c.withEngineDefaults().withRenderDefaults()
	out.Engine = strings.ToLower(strings.TrimSpace(out.Engine))
	return out
}

// withEngineDefaults: chọn engine + tham số một lần gọi Tesseract.
func (c Config) withEngineDefaults() Config {
	out := c
	if strings.TrimSpace(out.Engine) == "" {
		out.Engine = EngineTesseract
	}
	if strings.TrimSpace(out.Binary) == "" {
		out.Binary = "tesseract"
	}
	if out.Lang == "" {
		// "vie" chứ không "vie+eng": máy chưa cài gói eng sẽ làm tesseract
		// exit != 0 cho MỌI trang, không chỉ kém chính xác.
		out.Lang = "vie"
	}
	if out.PSM <= 0 {
		out.PSM = 6
	}
	if out.Timeout <= 0 {
		out.Timeout = 2 * time.Minute
	}
	// Trần của một trang PHẢI chứa trần của một lần gọi engine: trang gồm render
	// + OCR, đặt nhỏ hơn thì deadline cắt giữa lúc Tesseract đang chạy và trang
	// ấy fail oan đúng chỗ tốn tiền nhất.
	if out.PageTimeout <= out.Timeout {
		out.PageTimeout = out.Timeout + time.Minute
	}
	if out.MaxAttempts <= 0 {
		out.MaxAttempts = 3
	}
	if out.Parallel <= 0 {
		out.Parallel = 2
	}
	return out
}

// withRenderDefaults: đường dẫn + DPI poppler và các ngưỡng hiển thị.
func (c Config) withRenderDefaults() Config {
	out := c
	if strings.TrimSpace(out.RenderBinary) == "" {
		// Đường dẫn tuyệt đối trên Windows đã có trong config; rỗng nghĩa là
		// máy Linux/prod cài poppler hệ thống nên cứ tin PATH.
		out.RenderBinary = "pdftoppm"
	}
	if out.RenderDPI <= 0 {
		out.RenderDPI = 300
	}
	if out.PreviewDPI <= 0 {
		out.PreviewDPI = 150
	}
	if out.TextLayerMinChars <= 0 {
		out.TextLayerMinChars = 40
	}
	if out.MinConfWord <= 0 {
		out.MinConfWord = 70
	}
	return out
}

// String mô tả ngắn để log lúc boot — không in nội dung tài liệu, không in key.
func (c Config) String() string {
	return fmt.Sprintf("scanocr{engine=%s lang=%s psm=%d dpi=%d/%d th=%d conf<%d par=%d}",
		c.Engine, c.Lang, c.PSM, c.RenderDPI, c.PreviewDPI,
		c.TextLayerMinChars, c.MinConfWord, c.Parallel)
}
