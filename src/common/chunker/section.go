package chunker

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// SectionedChunk là 1 chunk kèm tiêu đề mục chứa nó (ví dụ "ĐIỀU 5: BẢO
// HIỂM") — service lưu Section vào DB, phase 2 prepend vào text embed để
// vector + Qdrant payload luôn mang ngữ cảnh mục, agent attribute đúng Điều
// mà không cần đọc thêm.
type SectionedChunk struct {
	Section string
	Text    string
}

// headingRe bắt tiêu đề mục văn bản hành chính Việt Nam ở đầu dòng: ĐIỀU,
// MỤC, CHƯƠNG, PHẦN, BÀI + số (Ả Rập hoặc La Mã) + tên mục tùy chọn. Chỉ
// nhận đầu dòng để không bắt nhầm chữ "điều" trong câu thường.
var headingRe = regexp.MustCompile(`(?m)^[\s>]*((ĐIỀU|Điều|MỤC|Mục|CHƯƠNG|Chương|PHẦN|Phần|BÀI|Bài)\s+([IVXLCDM\d]+)[\s.:)\-]*(.*))$`)

// maxSectionRunes chặn tiêu đề quá dài (dòng lỗi OCR) khỏi phình payload.
const maxSectionRunes = 120

// normalizeSection gộp khoảng trắng, cắt ngắn — tiêu đề sạch để prepend.
func normalizeSection(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxSectionRunes {
		return string([]rune(s)[:maxSectionRunes])
	}
	return s
}

// lastHeading trả tiêu đề mục cuối cùng xuất hiện trong text, "" nếu không có.
func lastHeading(text string) string {
	found := ""
	for _, m := range headingRe.FindAllStringSubmatch(text, -1) {
		if t := normalizeSection(m[1]); t != "" {
			found = t
		}
	}
	return found
}

// Annotate gán mục cho từng chunk theo thứ tự: chunk chứa tiêu đề thì lấy
// tiêu đề cuối trong chunk, chunk không có thì thừa hưởng carry (mục của
// chunk/trang trước). Trả danh sách đã gán + carry mới để caller nối sang
// trang sau — chunk giữa Điều không bao giờ mất ngữ cảnh mục.
func Annotate(parts []string, carry string) ([]SectionedChunk, string) {
	out := make([]SectionedChunk, 0, len(parts))
	cur := carry
	for _, p := range parts {
		if h := lastHeading(p); h != "" {
			cur = h
		}
		out = append(out, SectionedChunk{Section: cur, Text: p})
	}
	return out, cur
}

// HeaderFor dựng tiền tố ngữ cảnh prepend vào text lúc embed — agent và
// full-text search đều thấy "[ĐIỀU 5: ...]" ngay đầu đoạn.
func HeaderFor(section string) string {
	if section == "" {
		return ""
	}
	return "[" + section + "]\n"
}

// versionRe bắt "Phiên bản: 2024-Q3" ở đầu văn bản — best-effort, không có
// thì "" (service không failed vì thiếu metadata). Nhóm version không nuốt
// dấu "-" lẻ ("2025- Alpha" → "2025", còn "2025-Alpha"/"2024-Q3" giữ đủ).
var versionRe = regexp.MustCompile(`(?i)phiên bản\s*[:\-]?\s*([A-Za-z0-9._]+(?:-[A-Za-z0-9._]+)*)`)

// effectiveRe bắt "hiệu lực từ 01/10/2024" (dấu ":" sau "từ" tùy chọn vì
// đầu văn bản thường ghi "Hiệu lực từ: ...") — giữ nguyên chuỗi ngày thô,
// không parse time để khỏi failed với định dạng lạ.
var effectiveRe = regexp.MustCompile(`(?i)hiệu lực\s+(từ|kể từ)\s*:?\s*(\d{1,2}/\d{1,2}/\d{4})`)

// maxMetaScanRunes chỉ quét đầu văn bản tìm metadata — phiên bản/hiệu lực
// luôn nằm ở trang đầu, quét cả file chỉ tốn công.
const maxMetaScanRunes = 3000

// ExtractMeta trích phiên bản + ngày hiệu lực từ đầu văn bản — ("","") khi
// không có, caller lưu rỗng thay vì đoán.
func ExtractMeta(head string) (version, effective string) {
	r := []rune(head)
	if len(r) > maxMetaScanRunes {
		head = string(r[:maxMetaScanRunes])
	}
	if m := versionRe.FindStringSubmatch(head); len(m) > 1 {
		version = strings.Trim(m[1], " \t.,;:")
	}
	if m := effectiveRe.FindStringSubmatch(head); len(m) > 2 {
		effective = m[2]
	}
	return version, effective
}
