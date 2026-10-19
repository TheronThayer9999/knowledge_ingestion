package extractor

import (
	"strings"
	"unicode/utf8"
)

// LÀM SẠCH THEO CỬA SỔ TRƯỢT — cả 2 việc đều phụ thuộc CẢ FILE nên phải
// quét trước rồi mới áp, nhưng không cần giữ cả file trong RAM.
//
//	pass 1: Cleaner.Scan(text)   từng trang, chỉ CỘNG số đếm (vài chục byte)
//	pass 2: Cleaner.Apply(text)  từng trang, lọc dòng lặp + khử chữ rải
//
// Nhờ vậy RAM của cả 2 pass đều O(1 trang), độc lập kích thước file.

// maxRepeatScanLines số dòng đầu + cuối mỗi trang được xét là "vùng header /
// footer". Header/footer của PDF xuất từ Word nằm sát mép, không bao giờ ở
// giữa — giới hạn vùng giúp dòng trùng do VĂN BẢN lặp (hai điều khoản mở đầu
// giống nhau, dòng bảng lặp) không bị xoá oan.
const maxRepeatScanLines = 3

// maxRepeatKeyLen dòng dài hơi ngần này không thể là header/footer — bỏ qua
// luôn để khỏi đếm vô ích dòng nội dung.
const maxRepeatKeyLen = 80

// RepeatOptions chỉnh bộ dò dòng lặp — số 0 thì lấy mặc định.
type RepeatOptions struct {
	// MinRepeat là số lần tối thiểu để coi 1 dòng là lặp. Mặc định 3: dưới
	// ngưỡng này trùng cả 2 lần thường là trùng ngẫu nhiên, xoá là mất nội
	// dung thật.
	MinRepeat int
	// MinPages là số trang tối thiểu mới bắt đầu dò. Mặc định 5: file 2-3
	// trang thì thứ "lặp" cũng chỉ vài dòng, giữ nguyên an toàn hơn.
	MinPages int
}

func (o RepeatOptions) limits() (minRepeat, minPages int) {
	minRepeat, minPages = o.MinRepeat, o.MinPages
	if minRepeat <= 0 {
		minRepeat = 3
	}
	if minPages <= 0 {
		minPages = 5
	}
	return minRepeat, minPages
}

// Cleaner gom quyết định của pass 1 để áp ở pass 2. Chỉ giữ: bảng đếm dòng lặp
// (vài chục entry) + cờ "file này chữ rải" + bộ chấm điểm chữ rải (2 số).
type Cleaner struct {
	totalPages int
	opt        RepeatOptions
	// repeatOff bật khi số trang đủ lớn để việc dò dòng lặp có ý nghĩa.
	repeatOff bool
	counts    map[string]int
	acc       docSpacedAcc
}

// NewCleaner dựng Cleaner cho 1 tài liệu — totalPages là SỐ TRANG THẬT, truyền
// vào lúc tạo vì ngưỡng "file đủ dài" phải biết trước, không thể biết khi
// mới đọc trang thứ 3.
func NewCleaner(totalPages int, opt RepeatOptions) *Cleaner {
	_, minPages := opt.limits()
	return &Cleaner{
		totalPages: totalPages,
		opt:        opt,
		repeatOff:  totalPages >= minPages,
		counts:     make(map[string]int),
	}
}

// Scan là pass 1: chấm điểm dòng lặp + chấm điểm chữ rải của 1 trang thô.
// Chỉ cộng số, KHÔNG giữ text — nên gọi cho cả file cũng chỉ tốn vài chục KB.
//
// Điểm chấm chữ rải cộng dần theo trang là đúng vì mỗi dòng chấm độc lập,
// không mang state qua dòng khác (xem docSpacedAcc).
func (c *Cleaner) Scan(text string) {
	for _, l := range strings.Split(text, "\n") {
		c.acc.addLine(l)
	}
	if !c.repeatOff {
		return
	}
	lines := nonEmptyLines(text)
	n := len(lines)
	for j, l := range lines {
		if !isEdgeLine(j, n) {
			continue
		}
		if key := repeatKey(l); key != "" {
			c.counts[key]++
		}
	}
}

// Apply là pass 2: trả text trang ĐÃ LÀM SẠCH — bỏ dòng lặp ở vùng
// header/footer rồi khử chữ rải theo quyết định của cả file.
//
// THỨ TỰ CÓ LÝ DO: bỏ dòng lặp trước, khử chữ rải sau. Dòng header/footer
// lặp cũng góp phần vào điểm chữ rải, mà pass 1 chấm trên text THÔ có chúng —
// nếu pass 2 xoá trước rồi mới khử thì phần text còn lại được xử lý với quyết
// định đã tính trên bộ chữ lớn hơn, lệch so với chính số liệu đã quyết.
func (c *Cleaner) Apply(text string) string {
	if c.repeatOff {
		if stripped, ok := c.stripRepeat(text); ok {
			text = stripped
		}
	}
	return c.despaceLines(text)
}

// stripRepeat bỏ dòng lặp ở vùng mép trang — vấn đề riêng của PDF xuất từ
// Word: mọi trang đều in "Tên cơ quan ban hành", "Số: …", "Trang 3/40".
//
// VÌ SAO PHẢI BỎ: phase index ghép các trang thành văn bản liền mạch nên dòng
// lặp chen giữa 2 trang, cắt ngang câu, đẩy "ĐIỀU 5" sát mép trang ra xa
// tiêu đề thật và làm semantic splitter chấm sai.
func (c *Cleaner) stripRepeat(text string) (string, bool) {
	minRepeat, _ := c.opt.limits()
	lines := nonEmptyLines(text)
	n := len(lines)
	kept := make([]string, 0, n)
	removed := false
	for j, l := range lines {
		if isEdgeLine(j, n) && c.counts[repeatKey(l)] >= minRepeat {
			removed = true
			continue
		}
		kept = append(kept, l)
	}
	if !removed {
		return text, false
	}
	return strings.Join(kept, "\n"), true
}

// despaceLines áp quyết định "cả file chữ rải hay sạch" lên 1 trang — giống
// hệt vòng áp cũ trong extractPDFPages, nhưng quyết định đã được tính sẵn ở
// pass 1 nên không cần giữ mọi trang trong RAM.
func (c *Cleaner) despaceLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if c.acc.spaced() {
			lines[i] = joinLine(l)
		} else {
			lines[i] = despaceLine(l)
		}
	}
	return strings.Join(lines, "\n")
}

// isEdgeLine báo dòng thứ j (trong n dòng) nằm ở vùng header/footer.
func isEdgeLine(j, n int) bool {
	return j < maxRepeatScanLines || j >= n-maxRepeatScanLines
}

// nonEmptyLines tách text trang thành dòng có chữ — PDF hay để lại dòng trống
// quanh khối chữ, mà dòng trống đó không phải "vị trí" thật nên đếm vùng mép
// bằng chỉ số trên danh sách đã lọc mới đúng.
func nonEmptyLines(text string) []string {
	raw := strings.Split(text, "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// repeatKey chuẩn hoá 1 dòng để so sánh "giống nhau": hạ chữ thường, bỏ khoảng
// trắng, mọi cụm chữ số thay bằng "#". Dòng quá dài hoặc không có chữ cái nào
// (chỉ số trang, "--- 5 ---") trả "" nghĩa là không dò — dòng đó tự nó không
// đáng để đếm, và nếu đếm thì số trang đổi mỗi trang sẽ không gom được.
func repeatKey(line string) string {
	t := strings.TrimSpace(line)
	if utf8.RuneCountInString(t) > maxRepeatKeyLen {
		return ""
	}
	var b strings.Builder
	b.Grow(len(t))
	digits := false
	for _, r := range strings.ToLower(t) {
		switch {
		case r >= '0' && r <= '9':
			digits = true
		case r == ' ' || r == '\t':
			// Bỏ hẳn: "Trang  3 / 40" và "Trang 3/40" phải là 1 key.
			if digits {
				b.WriteString("#")
				digits = false
			}
		default:
			if digits {
				b.WriteString("#")
				digits = false
			}
			b.WriteRune(r)
		}
	}
	if digits {
		b.WriteString("#")
	}
	key := b.String()
	if !strings.ContainsFunc(key, isLetterRune) {
		return ""
	}
	return key
}

func isLetterRune(r rune) bool {
	return ('a' <= r && r <= 'z') || ('à' <= r && r <= 'ỹ')
}

// OCRCoveredRatio là ngưỡng coi phần OCR là "trùng text layer" — để lọc bằng
// số này thay hard-code trong hàm.
const OCRCoveredRatio = 0.7

// MergeOCRText gộp text layer với OCR của cùng 1 trang.
//
// Cần GỘP chứ không thay: trang export từ Word có cả text layer (tiêu đề, số
// hiệu chữ thật) LẪN ảnh scan, OCR đọc ảnh thêm phần chữ mà text layer thiếu.
// Nhưng nối vô điều kiện sẽ lặp ở trang chỉ có ảnh trang trí. Vì vậy chỉ nối
// khi phần OCR có thật sự BỔ SUNG — đo bằng tỉ lệ từ của OCR đã xuất hiện
// trong text layer.
func MergeOCRText(layer, ocrText string) string {
	layer = strings.TrimSpace(layer)
	ocrText = strings.TrimSpace(ocrText)
	switch {
	case ocrText == "":
		return layer
	case layer == "":
		return ocrText
	}
	if OCRCoveredBy(layer, ocrText) {
		return layer
	}
	return layer + "\n" + ocrText
}

// OCRCoveredBy báo phần lớn từ của OCR đã có trong text layer chưa. Ngưỡng 0.7
// vì OCR hay lệch ký tự ("Đạo đức" → "Dao duc") nên so khớp tuyệt đối sẽ
// luôn kết luận "chưa phủ" và nối trùng.
func OCRCoveredBy(layer, ocrText string) bool {
	ocrWords := wordSet(ocrText)
	if len(ocrWords) == 0 {
		return true
	}
	have := wordSet(layer)
	hits := 0
	for w := range ocrWords {
		if _, ok := have[w]; ok {
			hits++
		}
	}
	return float64(hits)/float64(len(ocrWords)) >= OCRCoveredRatio
}

// wordSet tách thành từ đã hạ chữ thường, bỏ dấu câu — so ở mức TỪ chứ không
// phải chuỗi con để "số 12" khớp với "số 12/2024".
func wordSet(s string) map[string]struct{} {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !isWordRune(r)
	})
	out := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		if f != "" {
			out[f] = struct{}{}
		}
	}
	return out
}

// isWordRune là 1 ký tự giữ được trong "từ" — chữ cái (kể cả tiếng Việt có
// dấu) hoặc chữ số.
func isWordRune(r rune) bool {
	return isLetterRune(r) || (r >= '0' && r <= '9')
}
