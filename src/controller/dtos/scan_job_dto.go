package dtos

import "time"

// CreateScanJobRequest là phần form của `POST /scan/jobs` (multipart có thêm
// field `file`). Toàn bộ field là TUỲ CHỌN: để trống thì server lấy mặc định
// trong scan_ocr.*, nên FE có thể gửi mỗi tệp mà không cần dựng bảng chọn nâng
// cao ngay từ đầu.
//
// Tag `form` bắt buộc vì gin bind multipart bằng form tag, tag `json` để cùng
// một struct còn dùng được cho swagger và cho đường JSON sau này.
type CreateScanJobRequest struct {
	Lang     string `json:"lang" form:"lang" example:"vie"`
	DPI      int    `json:"dpi" form:"dpi" example:"300"`
	PSM      int    `json:"psm" form:"psm" example:"6"`
	Engine   string `json:"engine" form:"engine" example:"tesseract"`
	ForceOCR bool   `json:"force_ocr" form:"force_ocr" example:"false"`
}

// ScanJobResponse là một job quét — cùng hình dạng cho lúc tạo, lúc xem tiến độ
// và lúc liệt kê, để FE chỉ có MỘT kiểu để render.
//
// Progress là % trang đã xong (0..100) do server tính:FE poll 1 giây một lần mà
// để client tự chia thì mỗi bản FE một cách làm tròn khác nhau.
type ScanJobResponse struct {
	ID          int64  `json:"id" example:"12"`
	Name        string `json:"name" example:"hop-don.pdf"`
	Status      string `json:"status" example:"processing"`
	TotalPages  int    `json:"total_pages" example:"200"`
	DonePages   int    `json:"done_pages" example:"120"`
	FailedPages int    `json:"failed_pages" example:"2"`
	// OCRTargetPages số trang thực sự OCR; TotalPages - OCRTargetPages là số trang
	// giữ nguyên vì đã có chữ (điểm autoai đo cả file rồi bỏ sót).
	OCRTargetPages int  `json:"ocr_target_pages" example:"198"`
	HasTextLayer   bool `json:"has_text_layer" example:"true"`
	ForceOCR       bool `json:"force_ocr" example:"false"`

	Lang   string `json:"lang" example:"vie"`
	DPI    int    `json:"dpi" example:"300"`
	PSM    int    `json:"psm" example:"6"`
	Engine string `json:"engine" example:"tesseract"`

	Progress int `json:"progress" example:"60"`

	// ErrorCode/ErrorMessage là lỗi HIỂN THỊ được cho người dùng (tab Lịch sử +
	// khung báo lỗi). FEmap sang câu tiếng Việt + nút "Thử lại", không bao giờ in
	// nguyên văn stack trace hay stderr của engine.
	ErrorCode    string `json:"error_code,omitempty" example:"ocr_unavailable"`
	ErrorMessage string `json:"error_message,omitempty" example:"không đọc được tệp"`

	// ExpiresAt hạn janitor dọn bản tạm (B.11) — người dùng cần biết file còn
	// tải được tới lúc nào trước khi bấm "Đưa vào kho".
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	// OutputURL presigned GET của PDF search được; rỗng khi chưa dựng xong.
	// Chữ ký là credential tạm nên giá trị này KHÔNG được ghi vào log.
	OutputURL string `json:"output_url,omitempty" example:"https://s3/scan/12/output.pdf?X-Amz-…"`
}

// ScanJobListResponse là trang listing của tab Lịch sử.
type ScanJobListResponse struct {
	Items []ScanJobResponse `json:"items"`
	Total int64             `json:"total" example:"57"`
	Limit int               `json:"limit" example:"20"`
	// Offset trang hiện tại để FE bấm "tải thêm" mà không dựng state riêng.
	Offset int `json:"offset" example:"0"`
}

// ScanJobDeleted giá trị `status` trả về cho thao tác xoá. Đây KHÔNG phải một
// trạng thái hàng đợi (row không còn nữa) mà là tín hiệu để FE gỡ dòng khỏi tab
// Lịch sử ngay mà không chờ một cú GET.
const ScanJobDeleted = "deleted"

// ScanJobActionResponse cho các thao tác ngắn (huỷ, xoá, OCR lại một trang): FE
// chỉ cần trạng thái mới để cập nhật UI ngay, không chờ thêm một round trip.
type ScanJobActionResponse struct {
	ID     int64  `json:"id" example:"12"`
	Status string `json:"status" example:"cancelled"`
	// PagesAffected số trang đổi trạng thái theo thao tác (huỷ thì đây là số trang
	// pending bị cắt; xoá thì là số object đã dọn).
	PagesAffected int `json:"pages_affected" example:"34"`
}

// ScanJobDownloadResponse là URL tải tệp kết quả — FE gán thẳng vào <iframe>
// hay thẻ <a>, KHÔNG proxy qua API.
type ScanJobDownloadResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	Filename  string    `json:"filename" example:"hop-don.pdf"`
}

// ScanJobPageWord là MỘT từ đã OCR của trang tab Xem & sửa. Box chuẩn hoá 0..1
// theo ảnh render (§B.3) — đơn vị duy nhất FE vẽ overlay và renderer cùng dùng,
// không phụ thuộc DPI hay zoom trình duyệt.
type ScanJobPageWord struct {
	T string  `json:"t" example:"Phiên"`
	X float64 `json:"x" example:"0.2794"`
	Y float64 `json:"y" example:"0.0456"`
	W float64 `json:"w" example:"0.0484"`
	H float64 `json:"h" example:"0.0174"`
	C float64 `json:"c" example:"96.1"`
}

// ScanJobPageResponse là MỘT trang của job cho tab Xem & sửa: chữ kèm từ tin
// từng từ để đối chiếu với ảnh, cộng URL ảnh render presigned.
//
// RenderURL rỗng khi chưa render được — FE hiển thị khung chữ thôi chứ không
// báo lỗi trang; ảnh render lazily (lần mở đầu render + cache vào kho, các lần
// sau đọc presigned).
type ScanJobPageResponse struct {
	JobID    int64             `json:"job_id" example:"12"`
	Page     int               `json:"page" example:"3"`
	Status   string            `json:"status" example:"ocr_done"`
	Words    []ScanJobPageWord `json:"words"`
	Text     string            `json:"text" example:"Phiên bản: 2024-Q4"`
	MeanConf float64           `json:"mean_conf" example:"91.8"`
	// LowConfWords số từ < ngưỡng tự tin — UI chỉ số "N từ nghi ngờ" ngay đầu
	// trang thay vì đếm lại toàn bộ mảng words mỗi lần render.
	LowConfWords  int    `json:"low_conf_words" example:"8"`
	CharsExisting int    `json:"chars_existing" example:"812"`
	HasTextLayer  bool   `json:"has_text_layer" example:"false"`
	LastError     string `json:"last_error,omitempty" example:"tesseract: missing vie.traineddata"`
	RenderURL     string `json:"render_url,omitempty" example:"https://s3/scan/12/p3.jpg?X-Amz-…"`
}
