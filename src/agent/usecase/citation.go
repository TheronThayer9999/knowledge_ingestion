package usecase

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Chuẩn hoá trích dẫn ở tầng backend. Prompt (§1, §10) và mô tả tool
// (knowledge_search) đã dạy model viết [^n] và cấm nhắc ID kỹ thuật, nhưng model
// free vẫn lệch: viết [*2] thay vì [^2], in "Article ID 54" vào answer, hoặc bịa
// số lớn hơn số nguồn thực sự có (collector cắt ở trần maxCollectedHits nên hit
// quá trần không có n để model tham chiếu). FE render markdown bằng `marked` —
// marked core không có footnote — nên mấy chỗ lệch đó in ra nguyên văn, user đọc
// như lỗi font.
//
// BE là chỗ cuối còn giữ dữ kiện thật (danh sách cites của đúng lượt này) nên
// chốt lại hình thức ở đây thay vì sửa prompt thêm một câu "đừng làm vậy".

// citationRun bắt MỘT DÃY marker liền nhau ("[^1][^2]", "[1] [2]", "[*3]") để xử
// lý một lần: chuẩn hoá khuôn, lọc số ngoài phạm vi, gộp trùng. Chỉ nhận toàn là
// số trong ngoặc vuông nên không đụng [chữ](link) hay [ ] checkbox; số được tách
// lại bằng singleMarker trong callback nên regex này không cần capture group.
var citationRun = regexp.MustCompile(`\[\s*[*^]?\s*\d{1,3}\s*\](?:\s*\[\s*[*^]?\s*\d{1,3}\s*\])*`)

// singleMarker đọc số n từ MỘT marker đã nằm trong dãy.
var singleMarker = regexp.MustCompile(`\[\s*[*^]?\s*(\d{1,3})\s*\]`)

// leakedArticleID xoá các cách model viết ID nội bộ: "Article ID 54",
// "article_id: 56", "(mã bài viết #12)", "source id = 3". Kèm cả ngoặc bao quanh
// để không để lại "( )" sau khi cắt; khoảng trắng hai bên cũng bị nuốt nên caller
// thay lại bằng một dấu cách rồi dọn.
var leakedArticleID = regexp.MustCompile(`(?i)[(\[]?\s*(?:article[\s_]*id|bài[\s_]*viết[\s_]*id|mã[\s_]*bài[\s_]*viết|nguồn[\s_]*id|source[\s_]*id)\s*[:=#]?\s*\d+\s*[)\]]?`)

// emptyParen là rác còn lại sau khi cắt ID/marker: "()", "( )", "(Theo )",
// "(theo kết quả đọc: )".
var emptyParen = regexp.MustCompile(`(?i)\(\s*(?:theo\s*(?:kết quả đọc:)?\s*)?\)`)

// spaceBeforePunct/nbsp: khoảng trắng thừa giữa câu sau khi xoá marker.
var spaceBeforePunct = regexp.MustCompile(`\s+([,.;:!?])`)
var multiSpace = regexp.MustCompile(`[ \t]{2,}`)

// normalizeCitations sửa answer theo đúng cites của lượt này (nCites là số nguồn
// thực có). Trả số marker bị loại để caller log — model lệch bao nhiêu là chỉ số
// cần theo dõi, không phải nội dung answer.
func normalizeCitations(answer string, nCites int) (string, int) {
	if strings.TrimSpace(answer) == "" {
		return answer, 0
	}
	dropped := 0
	out := citationRun.ReplaceAllStringFunc(answer, func(run string) string {
		var kept []string
		for _, n := range markerNumbers(run) {
			if n < 1 || n > nCites {
				dropped++
				continue
			}
			mark := fmt.Sprintf("[^%d]", n)
			if slices.Contains(kept, mark) {
				continue
			}
			kept = append(kept, mark)
		}
		return strings.Join(kept, "")
	})
	// Thay cụm ID bằng MỘT dấu cách chứ không phải chuỗi rỗng: regex đã nuốt
	// khoảng trắng hai bên cụm, thay bằng "" sẽ dán liền hai từ ("theovà").
	cleaned := leakedArticleID.ReplaceAllString(out, " ")
	if dropped == 0 && cleaned == out {
		// Không có gì bị cắt thì giữ nguyên khoảng trắng model viết — code block và
		// bảng markdown canh lề bằng dấu cách, dọn vô điều kiện là phá layout.
		return cleaned, 0
	}
	return tidyText(cleaned), dropped
}

// markerNumbers tách mọi số n trong một dãy marker, theo đúng thứ tự xuất hiện.
func markerNumbers(run string) []int {
	subs := singleMarker.FindAllStringSubmatch(run, -1)
	nums := make([]int, 0, len(subs))
	for _, s := range subs {
		n, err := strconv.Atoi(s[1])
		if err != nil {
			continue
		}
		nums = append(nums, n)
	}
	return nums
}

// tidyText dọn leftover sau khi cắt: ngoặc rỗng, khoảng trắng kép, khoảng trắng
// trước dấu câu. Không đụng xuống dòng để giữ nguyên layout markdown của model.
func tidyText(s string) string {
	s = emptyParen.ReplaceAllString(s, "")
	s = multiSpace.ReplaceAllString(s, " ")
	s = spaceBeforePunct.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}
