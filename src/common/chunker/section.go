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

// headwordRe quét mọi cụm headword+số trong text (mọi vị trí) — việc quyết
// định đâu là tiêu đề thật nằm ở findHeadings, không nhét vào regex.
var headwordRe = regexp.MustCompile(`(ĐIỀU|Điều|MỤC|Mục|CHƯƠNG|Chương|PHẦN|Phần|BÀI|Bài)\s+([IVXLCDM\d]+)`)

// headingPos là 1 match headword+số: Start ở đầu headword, End ngay sau số,
// Head là dạng thô ("ĐIỀU 1") để dựng tên sau.
type headingPos struct {
	Start, End int
	Head       string
}

// findHeadings quét mọi vị trí headword+số, giữ lại tiêu đề thật theo 2 cửa:
// (1) đầu dòng (đầu text hoặc chỉ khoảng trắng/> tới \n gần nhất) thì nhận
// mọi kiểu chữ — giữ hành vi headingRe cũ; (2) giữa dòng thì headword phải
// VIẾT HOA (văn bản hành chính viết hoa tiêu đề, chữ "điều" thường trong
// câu không dính) và sau số phải có dấu phân cách chứ không phải dấu phẩy
// — loại viện dẫn chéo kiểu "Điều 1, Khoản 2". Text extract PDF dồn một
// dòng dài nên cửa (2) là cứu cánh cho section (lỗi cũ: chunk nào cũng rỗng
// vì regex cũ đòi ^ đầu dòng).
func findHeadings(text string) []headingPos {
	var out []headingPos
	for _, loc := range headwordRe.FindAllStringSubmatchIndex(text, -1) {
		word := text[loc[2]:loc[3]]
		start, numEnd := loc[0], loc[5]
		lineStart := true
		for k := start - 1; k >= 0; k-- {
			c := text[k]
			if c == '\n' {
				break
			}
			if c != ' ' && c != '\t' && c != '\r' && c != '>' {
				lineStart = false
				break
			}
		}
		sep := byte(0)
		for i := numEnd; i < len(text); i++ {
			c := text[i]
			if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
				continue
			}
			sep = c
			break
		}
		if !lineStart {
			if word != strings.ToUpper(word) {
				continue
			}
			if sep != ':' && sep != '.' && sep != ')' && sep != '-' && sep != ']' && sep != 0 {
				continue
			}
		}
		out = append(out, headingPos{Start: start, End: numEnd, Head: word + " " + text[loc[4]:loc[5]]})
	}
	return out
}

// khoanRe chặn tên mục trước mốc khoản (" 1. ...") — tên Điều luôn đứng
// trước khoản 1, không bao giờ nuốt nội dung khoản (lỗi cũ: section
// "...ĐIỀU 6... 2. Hỗ" do (.*)$ nuốt cả dòng rồi cắt cụt giữa chữ).
var khoanRe = regexp.MustCompile(`\s+\d+\.\s`)

// headingTitle dựng "ĐIỀU 1: TÊN" từ 1 match — tên lấy từ sau số hiệu tới
// biên gần nhất: tiêu đề kế (nextStart), mốc khoản, xuống dòng, rồi trần ký
// tự cắt ở ranh giới từ. Tiêu đề không tên ("ĐIỀU 1:" trơ trọi) thì giữ
// headword + số.
func headingTitle(text string, m headingPos, nextStart int) string {
	rest := text[m.End:]
	if nextStart > m.End && nextStart-m.End < len(rest) {
		rest = rest[:nextStart-m.End]
	}
	rest = strings.TrimLeft(rest, " \t:.)]-")
	if i := strings.IndexAny(rest, "\r\n"); i >= 0 {
		rest = rest[:i]
	}
	if loc := khoanRe.FindStringIndex(rest); loc != nil {
		rest = rest[:loc[0]]
	}
	// Cắt ở tiêu đề kế còn sót (headword HOA giữa dòng mà findHeadings bỏ
	// sót vì thiếu dấu phân cách) — chặn tương đối, không tuyệt đối.
	if loc := headwordRe.FindStringIndex(rest); loc != nil && loc[0] > 0 {
		rest = rest[:loc[0]]
	}
	name := strings.Join(strings.Fields(rest), " ")
	if name == "" {
		return normalizeSection(m.Head)
	}
	if utf8.RuneCountInString(name) > maxSectionRunes {
		r := []rune(name)
		cut := maxSectionRunes
		for cut > 0 && r[cut-1] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = maxSectionRunes
		}
		name = strings.TrimSpace(string(r[:cut]))
	}
	return normalizeSection(m.Head + ": " + name)
}

// firstHeading trả tiêu đề ĐẦU TIÊN trong text — chunk gán mục theo Điều mà
// nó bắt đầu, không lấy heading cuối (lỗi cũ: chunk mở đầu Điều 1 nhưng
// chứa Điều 2 ở cuối thì bị gán Điều 2).
func firstHeading(text string) string {
	ms := findHeadings(text)
	if len(ms) == 0 {
		return ""
	}
	next := len(text)
	if len(ms) > 1 {
		next = ms[1].Start
	}
	return headingTitle(text, ms[0], next)
}

// lastHeading trả tiêu đề CUỐI CÙNG trong text — dùng để tính carry cho
// chunk/trang sau (phần sau chunk thuộc về Điều cuối này).
func lastHeading(text string) string {
	ms := findHeadings(text)
	if len(ms) == 0 {
		return ""
	}
	return headingTitle(text, ms[len(ms)-1], len(text))
}

// Block là 1 Điều/mục nguyên vẹn trong text — service cắt size bên trong
// từng block, không gộp/cắt ngang ranh giới Điều (lỗi cũ: chunk trộn Điều
// 1+2+3, LLM không biết quy tắc thuộc Điều nào).
//
// Start/End là byte offset của block trong text ĐÃ CHO SplitBlocks (Start
// inclusive, End exclusive). Offset sinh ra để dùng, không phải để trang
// trí: phase chunk chạy SplitBlocks trên TOÀN VĂN BẢN (thấy Điều nào nằm ở
// đâu kể cả khi tiêu đề dính sát cuối trang), rồi dùng offset cắt block tại
// ranh giới trang để từng chunk vẫn nằm gọn trong 1 trang — xem
// extractor.Document.Cuts.
type Block struct {
	Heading string
	Text    string
	Start   int
	End     int
}

// SplitBlocks chẻ text thành các block theo tiêu đề — không tiêu đề thì 1
// block rỗng heading (giữ hành vi cũ cho văn bản không có Điều). Mỗi block
// bắt đầu đúng ở đầu tiêu đề để chunk đầu block luôn gán đúng mục. Text
// đứng trước tiêu đề đầu (đuôi Điều trang trước, hoặc cột phải mà extractor
// nhả trước cột trái ở PDF 2 cột) KHÔNG được rơi mất: thành block riêng
// heading rỗng để caller gán carry — mất là mất khoản (đã từng mất cả
// "không cộng dồn", "dịch bệnh 14 ngày", "ứng trước 2 triệu" ở article 10).
func SplitBlocks(text string) []Block {
	ms := findHeadings(text)
	if len(ms) == 0 {
		return []Block{{Text: text, Start: 0, End: len(text)}}
	}
	var out []Block
	if pre := text[:ms[0].Start]; strings.TrimSpace(pre) != "" {
		out = append(out, Block{Text: pre, Start: 0, End: ms[0].Start})
	}
	for i, m := range ms {
		end := len(text)
		if i+1 < len(ms) {
			end = ms[i+1].Start
		}
		out = append(out, Block{Heading: headingTitle(text, m, end), Text: text[m.Start:end], Start: m.Start, End: end})
	}
	return out
}

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

// Annotate gán mục cho từng chunk theo thứ tự: chunk chứa tiêu đề thì lấy
// tiêu đề ĐẦU trong chunk (Điều mà chunk bắt đầu), chunk không có thì thừa
// hưởng carry (mục của chunk/trang trước). Carry mới là tiêu đề cuối trong
// chunk (phần sau thuộc về Điều đó). Trả danh sách đã gán + carry mới để
// caller nối sang trang sau — chunk giữa Điều không bao giờ mất ngữ cảnh mục.
func Annotate(parts []string, carry string) ([]SectionedChunk, string) {
	out := make([]SectionedChunk, 0, len(parts))
	cur := carry
	for _, p := range parts {
		if h := firstHeading(p); h != "" {
			cur = h
		}
		out = append(out, SectionedChunk{Section: cur, Text: p})
		if h := lastHeading(p); h != "" {
			cur = h
		}
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

// MaxMetaScanRunes chỉ quét đầu văn bản tìm metadata — phiên bản/hiệu lực
// luôn nằm ở trang đầu, quét cả file chỉ tốn công. Phase extract gọi trực tiếp
// vì đó là lúc duy nhất giữ trọn văn bản.
const MaxMetaScanRunes = 3000

// ExtractMeta trích phiên bản + ngày hiệu lực từ đầu văn bản — ("","") khi
// không có, caller lưu rỗng thay vì đoán. head nên là phần đầu văn bản (xem
// headText trong services.article_extract_service), hàm tự cắt còn tối đa
// MaxMetaScanRunes rune.
func ExtractMeta(head string) (version, effective string) {
	r := []rune(head)
	if len(r) > MaxMetaScanRunes {
		head = string(r[:MaxMetaScanRunes])
	}
	if m := versionRe.FindStringSubmatch(head); len(m) > 1 {
		version = strings.Trim(m[1], " \t.,;:")
	}
	if m := effectiveRe.FindStringSubmatch(head); len(m) > 2 {
		effective = m[2]
	}
	return version, effective
}
