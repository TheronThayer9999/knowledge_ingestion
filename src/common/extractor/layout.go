package extractor

import (
	"strings"
	"unicode"
)

// Ngưỡng của detector. Đây là HẰNG SỐ có chủ đích, không phải config: điểm số
// được lưu vào DB (article_pages.layout_score) nên khi cần chỉnh ngưỡng chỉ
// sửa đúng một chỗ này rồi chạy lại truy vấn, không phải trích lại cả kho.
const (
	// layoutComplexThreshold là mức điểm để coi một trang là phức tạp. Đặt
	// bằng đúng trọng số của MỘT dấu hiệu mạnh: hoặc có khe cột (0.4), hoặc
	// có dòng nhiều số (0.4) ⇒ phức tạp. Chỉ riêng đường kẻ (0.2) thì không
	// đủ — trang văn bản có dòng "-----" phân cách không phải bảng.
	layoutComplexThreshold = 0.4
	// Số dòng tối thiểu để chấm điểm — trang 2-3 dòng (bia, chú thích ảnh)
	// không đáng bật xử lý nặng.
	layoutMinLines = 5
	// layoutGapRows: số dòng có khe cột (>=2 khoảng trắng liên tiếp) để tin
	// trang có bố cục nhiều cột. Một hai dòng lẻ là do căn lề, không phải bảng.
	layoutGapRows = 3
	// layoutNumericRows: số dòng có >=3 token số — dấu hiệu mạnh nhất của bảng
	// biểu (biểu phí, mức phạt, bảng so sánh chỉ tiêu).
	layoutNumericRows = 3
	// layoutNumericPerRow: số token số tối thiểu trên 1 dòng để tính là "dòng số".
	layoutNumericPerRow = 3
	// layoutRuleRows: số dòng là đường kẻ bảng.
	layoutRuleRows = 2
)

// Trọng số từng dấu hiệu. Tổng của ba dấu hiệu = 1.0 nên Score luôn trong [0,1].
const (
	layoutGapWeight     = 0.4
	layoutNumericWeight = 0.4
	layoutRuleWeight    = 0.2
)

// LayoutSignals là các dấu hiệu bố cục đếm được trên text `pdftotext -layout`
// của MỘT trang. Giữ các con số rời thay vì chỉ trả 1 điểm, để khi cần giải
// thích "vì sao trang này bị coi là phức tạp" còn có dữ liệu mà log.
type LayoutSignals struct {
	Lines       int // số dòng khác rỗng
	GapRows     int // số dòng có khe cột
	NumericRows int // số dòng nhiều số
	RuleRows    int // số dòng là đường kẻ bảng
}

// Score quy các dấu hiệu về [0,1] theo trọng số ở trên.
//
// VÌ SAO CẦN CẢ BA: bảng không đường kẻ (rất phổ biến trong văn bản hành
// chính Việt Nam) chỉ lộ ra qua khe cột và/hoặc dòng số; còn bảng có đường kẻ
// mà cột dính nhau thì chỉ lộ qua đường kẻ. Cộng dồn theo trọng số để trang
// có nhiều hơn một dấu hiệu vẫn được xếp trên trang chỉ có một.
func (s LayoutSignals) Score() float64 {
	if s.Lines < layoutMinLines {
		return 0
	}
	var score float64
	if s.GapRows >= layoutGapRows {
		score += layoutGapWeight
	}
	if s.NumericRows >= layoutNumericRows {
		score += layoutNumericWeight
	}
	if s.RuleRows >= layoutRuleRows {
		score += layoutRuleWeight
	}
	return score
}

// Complex cho biết trang có được coi là phức tạp không.
func (s LayoutSignals) Complex() bool { return s.Score() >= layoutComplexThreshold }

// IsComplex trả kết luận từ ĐIỂM ĐÃ LƯU (article_pages.layout_score) — cùng
// một ngưỡng với Complex, để chỗ đọc DB và chỗ đo lúc trích không lệch nhau.
func IsComplex(score float64) bool { return score >= layoutComplexThreshold }

// DetectLayout đếm dấu hiệu bố cục trên text của một trang.
//
// Đầu vào PHẢI là text `pdftotext -layout`: bản không có -layout đã bị dồn cột
// nên khe cột biến mất, detector sẽ luôn trả 0.
func DetectLayout(text string) LayoutSignals {
	var s LayoutSignals
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		s.Lines++
		if hasColumnGap(line) {
			s.GapRows++
		}
		if countNumericTokens(line) >= layoutNumericPerRow {
			s.NumericRows++
		}
		if isRuleLine(line) {
			s.RuleRows++
		}
	}
	return s
}

// hasColumnGap báo dòng có khe cột giữa các cột: từ 2 khoảng trắng liên tiếp
// trở lên, hoặc một tab.
//
// Bỏ qua khoảng trắng ĐẦU DÒNG: thụt lề đầu đoạn (2-4 space) là chuyện bình
// thường của mọi trang văn bản, nếu tính thì trang chữ thuần nào cũng thành
// "nhiều cột". Chỉ khe nằm giữa nội dung mới là dấu hiệu của bảng.
func hasColumnGap(line string) bool {
	body := strings.TrimLeft(line, " \t")
	if strings.ContainsRune(body, '\t') {
		return true
	}
	run := 0
	for _, r := range body {
		if r == ' ' {
			run++
			if run >= 2 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}

// countNumericTokens đếm token mà toàn bộ ký tự là số và dấu phân cách
// (1.000.000, 0,05%, 2026). Không đếm số nằm trong từ ("Điều7", "COVID-19")
// vì đó là chữ, không phải cột số.
func countNumericTokens(line string) int {
	count := 0
	for _, tok := range strings.Fields(line) {
		tok = strings.Trim(tok, "()[]{}.,;:\"'")
		if tok == "" {
			continue
		}
		if isNumericToken(tok) {
			count++
		}
	}
	return count
}

// isNumericToken kiểm token chỉ gồm chữ số và tối đa 1 dấu % ở cuối.
func isNumericToken(tok string) bool {
	tok = strings.TrimSuffix(tok, "%")
	if tok == "" {
		return false
	}
	hasDigit := false
	for _, r := range tok {
		switch {
		case unicode.IsDigit(r):
			hasDigit = true
		case r == '.' || r == ',' || r == '-' || r == '/':
		default:
			return false
		}
	}
	return hasDigit
}

// isRuleLine báo dòng là đường kẻ bảng: toàn ký tự kẻ (| - = + _ và khoảng
// trắng) và có ít nhất 3 ký tự kẻ. Tránh bắt nhầm dòng chữ có gạch nối.
func isRuleLine(line string) bool {
	rules := 0
	for _, r := range line {
		switch r {
		case '|', '-', '=', '+', '_':
			rules++
		case ' ', '\t':
		default:
			return false
		}
	}
	return rules >= 3
}
