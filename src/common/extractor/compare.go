package extractor

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Ngưỡng Jaccard trên tập TỪ (đã bỏ số) để xếp hạng kết quả đối chiếu. Chọn
// theo RAG_RESEARCH.md §6.18-b: >= 0.90 tin được, 0.70-0.90 lệch nhẹ phải ghi
// độ tin cậy trung bình, dưới 0.70 là xung đột thật.
const (
	compareAgreeScore = 0.90
	compareMinorScore = 0.70
)

// Verdict là kết cục đối chiếu 2 bản trích của CÙNG 1 trang.
type Verdict string

const (
	// VerdictAgree: hai bản coi như trùng — lấy bản tất định.
	VerdictAgree Verdict = "agree"
	// VerdictMinor: lệch chữ nhưng số khớp — gắn độ tin cậy trung bình.
	VerdictMinor Verdict = "minor"
	// VerdictConflict: lệch số hoặc lệch chữ nhiều — KHÔNG tự chọn bản nào,
	// trả cả hai cho người dùng/trọng tài.
	VerdictConflict Verdict = "conflict"
)

// CrossCheck là kết quả CompareExtraction. NumbersA/NumbersB là số lượng token
// số KHÁC NHAU của từng bản; OnlyInA/OnlyInB liệt kê đúng những token số không
// có ở bên kia (đã chuẩn hoá) — đây là thứ người vận hành cần thấy trước tiên
// khi có xung đột, vì với văn bản luật sai 1 con số là sai cả câu trả lời.
type CrossCheck struct {
	Verdict  Verdict
	Score    float64
	NumbersA int
	NumbersB int
	OnlyInA  []string
	OnlyInB  []string
	Reason   string
}

// CompareExtraction so hai bản trích của cùng một trang: det là bản tất định
// (text layer / L2 từ toạ độ / OCR), vlm là bản do model ảnh đọc.
//
// VÌ SAO PHẢI TẤT ĐỊNH: để LLM so hai đoạn text thì đắt, không tái lập được
// (cùng input ra kết quả khác), và không giải thích được vì sao kết luận như
// vậy. Ở đây chỉ có so tập hợp — chạy lại luôn ra đúng một kết quả.
//
// THỨ TỰ QUYẾT ĐỊNH (theo §6.18-b):
//  1. Tập SỐ phải bằng nhau tuyệt đối. Lệch 1 số là xung đột ngay, không cần
//     đo độ trùng chữ — vì bản trùng 99% chữ mà sai số vẫn là bản sai.
//  2. Số khớp thì mới chấm Jaccard trên tập từ còn lại.
//
// Số được chuẩn hoá dấu nhóm nghìn trước khi so (1.000.000 == 1000000,
// 0,05 == 0.05) để nhiễu OCR về dấu phân cách không thành xung đột giả.
// '%' là một TỪ riêng chứ không dính vào số: OCR hay đọc rời "50 %", nên
// "50%" và "50 %" phải bằng nhau; còn "50%" và "50" khác nhau ở phần từ và
// chỉ bị coi là lệch chữ, không phải lệch số.
//
// Text rỗng hai bên: agree (không có gì để lệch, ví dụ trang trắng). Text rỗng
// một bên: conflict.
func CompareExtraction(det, vlm string) CrossCheck {
	wordsA, numsA := compareTokens(det)
	wordsB, numsB := compareTokens(vlm)

	out := CrossCheck{
		NumbersA: len(numsA),
		NumbersB: len(numsB),
		OnlyInA:  setDiff(numsA, numsB),
		OnlyInB:  setDiff(numsB, numsA),
		Score:    jaccard(wordsA, wordsB),
	}
	switch {
	case len(out.OnlyInA) > 0 || len(out.OnlyInB) > 0:
		out.Verdict, out.Reason = VerdictConflict, "số lệch"
	case out.Score >= compareAgreeScore:
		out.Verdict, out.Reason = VerdictAgree, "khớp"
	case out.Score >= compareMinorScore:
		out.Verdict, out.Reason = VerdictMinor, "lệch nhẹ"
	default:
		out.Verdict, out.Reason = VerdictConflict, "lệch nhiều"
	}
	return out
}

// compareTokens tách text thành 2 tập: từ (chữ cái, đã lowercase, GIỮ dấu
// tiếng Việt) và số (đã chuẩn hoá dấu nhóm). Không dùng regexp vì số có dấu
// phân cách lồng nhau ("1.000.000", "0,05%") — quét rune cho kết quả rõ ràng
// và kiểm soát được từng nhánh.
func compareTokens(s string) (words, nums map[string]struct{}) {
	words = make(map[string]struct{})
	nums = make(map[string]struct{})
	r := []rune(norm.NFC.String(s))
	for i := 0; i < len(r); {
		switch {
		case unicode.IsDigit(r[i]):
			j := i
			for j < len(r) && unicode.IsDigit(r[j]) {
				j++
			}
			// Nuốt dấu phân cách chỉ khi ngay sau nó là chữ số — dấu ở cuối
			// câu ("1.000.000 đồng.") không được dính vào số.
			for j+1 < len(r) && (r[j] == '.' || r[j] == ',') && unicode.IsDigit(r[j+1]) {
				j++
				for j < len(r) && unicode.IsDigit(r[j]) {
					j++
				}
			}
			nums[canonicalNumber(string(r[i:j]))] = struct{}{}
			i = j
		case unicode.IsLetter(r[i]) || unicode.IsMark(r[i]):
			j := i
			for j < len(r) && (unicode.IsLetter(r[j]) || unicode.IsMark(r[j])) {
				j++
			}
			words[strings.ToLower(string(r[i:j]))] = struct{}{}
			i = j
		case r[i] == '%':
			words["%"] = struct{}{}
			i++
		default:
			i++
		}
	}
	return words, nums
}

// canonicalNumber đưa một token số về dạng so sánh được.
//
// Quy tắc (tất định, ghi rõ để người đọc sau không phải đoán):
//   - Không có dấu phân cách → giữ nguyên.
//   - Nhiều dấu và MỌI nhóm sau nhóm đầu đều đúng 3 chữ số ⇒ tất cả là dấu
//     nhóm nghìn, bỏ hết ("1.000.000" → "1000000").
//   - Còn lại ⇒ dấu xuất hiện SAU CÙNG là dấu thập phân, các dấu khác là nhóm
//     ("1,234,567.89" → "1234567.89").
//   - Đúng 1 dấu, nhóm sau dài 3 và nhóm đầu 1-3 chữ số ⇒ nhóm nghìn
//     ("1.000" → "1000"); nếu nhóm đầu dài hơn 3 thì đó là thập phân
//     ("1000.500" giữ nguyên).
//
// Hệ quả đã biết: "1.234" được hiểu là 1234 (kiểu Việt Nam). Tài liệu dùng
// "." làm dấu thập phân cho đúng 3 chữ số lẻ sẽ bị chuẩn hoá sai — chấp nhận,
// vì đổi lại là không còn xung đột giả trên biểu phí/mức phạt hay gặp.
func canonicalNumber(tok string) string {
	parts := strings.FieldsFunc(tok, func(r rune) bool { return r == '.' || r == ',' })
	if len(parts) <= 1 {
		return tok
	}
	seps := make([]byte, 0, len(parts)-1)
	for i := 0; i < len(tok); i++ {
		if tok[i] == '.' || tok[i] == ',' {
			seps = append(seps, tok[i])
		}
	}
	allGrouped := true
	for _, p := range parts[1:] {
		if len(p) != 3 {
			allGrouped = false
			break
		}
	}
	if len(seps) >= 2 {
		if allGrouped {
			return strings.Join(parts, "")
		}
		return joinWithDecimal(parts)
	}
	if len(parts[1]) == 3 && len(parts[0]) >= 1 && len(parts[0]) <= 3 {
		return strings.Join(parts, "")
	}
	return joinWithDecimal(parts)
}

// joinWithDecimal nối các nhóm, dùng dấu thập phân chuẩn "." cho nhóm cuối:
// "12","5" → "12.5"; "1234","567","89" → "1234567.89".
func joinWithDecimal(parts []string) string {
	if len(parts) < 2 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], "") + "." + parts[len(parts)-1]
}

// jaccard đo độ trùng tập từ. Hai tập rỗng coi như trùng hoàn toàn (1) — trang
// trắng ở cả hai nguồn không phải là xung đột.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 1
	}
	return float64(inter) / float64(union)
}

// setDiff trả phần tử có ở a mà không có ở b, đã sắp xếp để log/UI ổn định.
func setDiff(a, b map[string]struct{}) []string {
	var out []string
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
