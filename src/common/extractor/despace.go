package extractor

import (
	"strings"
	"unicode/utf8"
)

// Ngưỡng nhận diện dòng chữ rải — một số PDF (kết xuất từ tool lạ) lưu mỗi
// ký tự (hoặc từng cặp như "ng", "ườ") rời nên text trích ra thành "Đ ạ o"
// hay "Con ng ườ i" thay vì "Đạo đức", "Con người".
//
// Chữ rải là tính chất cả file nên có 2 mức: strict cho file sạch (chỉ đếm
// token 1 chữ — câu thường tiếng Việt gần như không có, trừ "ở", "y" lẻ tẻ),
// còn file đã xác định rải (docSpaced) thì nối mọi dòng không qua ngưỡng.
// Không dùng token 2 chữ ở mức strict: câu ngắn toàn từ 2 chữ ("Cô ấy đi
// ra.") mà dính ngưỡng sẽ bị nối oan.
const (
	minStrictSingles = 10
	minShortSingles  = 4
	minStrictRatio   = 0.5
)

// countSingles đếm token lõi 1 chữ / 2 chữ trong dòng (bỏ dấu câu bám như
// "nh.", "u," trước khi đếm).
func countSingles(toks []string) (ones, twos int) {
	for _, t := range toks {
		switch utf8.RuneCountInString(coreLetters(t)) {
		case 1:
			ones++
		case 2:
			twos++
		}
	}
	return ones, twos
}

// isSpacedLine (strict) báo dòng rải chữ trong file sạch — chỉ tin token 1
// chữ, ngưỡng cao để câu ngắn nhiều từ 2 chữ không bị nối oan.
func isSpacedLine(toks []string) bool {
	ones, _ := countSingles(toks)
	if ones >= minStrictSingles {
		return true
	}
	return ones >= minShortSingles && float64(ones)/float64(len(toks)) >= minStrictRatio
}

// spacedScore chấm mức rải 1 dòng để đo cả file: token 1 chữ tính 1, token
// 2 chữ tính nửa (từ 2 chữ thật như "đi", "ăn" gây nhiễu nên chỉ nửa điểm).
// Dòng ngắn (<4 token) không chấm để tiêu đề mục lục khỏi làm lệch.
func spacedScore(toks []string) float64 {
	if len(toks) < 4 {
		return 0
	}
	ones, twos := countSingles(toks)
	return (float64(ones) + 0.5*float64(twos)) / float64(len(toks))
}

// docSpaced báo cả file có phải chữ rải không: quá 15% số dòng đạt điểm rải
// (≥0.3) thì là file rải. File sạch điểm này ≈0 nên không chạm.
//
// Chỉ là wrapper quanh docSpacedAcc cho test + chỗ gọi 1 lần; extractor
// PDF dùng acc để cộng dần theo trang, không dựng bản sao toàn văn bản.
func docSpaced(text string) bool {
	var acc docSpacedAcc
	for _, l := range strings.Split(text, "\n") {
		acc.addLine(l)
	}
	return acc.spaced()
}

// docSpacedAcc gom chấm điểm rải của CẢ file mà không cần giữ văn bản.
//
// Vì sao gom dần được: quyết định "file này rải hay không" chỉ là tỉ lệ
// (số dòng đạt điểm / tổng số dòng ≥4 token), và mỗi dòng chấm độc lập —
// không state nào mang qua dòng khác. Nên accumulator cho kết quả y hệt
// docSpaced(text), chỉ khác là không phải dựng thêm một bản sao toàn file.
//
// Đổi lại: quyết định CHỈ CÓ ĐƯỢC sau khi đọc hết file. Đó là điểm chặn
// việc stream trang ra ngoài — xem pdf.go.
type docSpacedAcc struct{ qual, total int }

// addLine cộng 1 dòng vào bộ đếm — bỏ qua dòng <4 token vì không chấm (tiêu
// đề mục lục không được làm lệch tỉ lệ).
func (a *docSpacedAcc) addLine(line string) {
	toks := strings.Fields(line)
	if len(toks) < 4 {
		return
	}
	a.total++
	if spacedScore(toks) >= 0.3 {
		a.qual++
	}
}

// DespacePages khử chữ rải cho CẢ file rồi áp lên từng trang — chỉ dùng ở
// test/công cụ tay. Worker KHÔNG đi đường này vì phải giữ mọi trang trong
// RAM; nó dùng Cleaner (xem clean.go): pass 1 Scan chỉ cộng số đếm, pass 2
// Apply áp từng trang — cùng kết quả, RAM O(1 trang).
func DespacePages(pages []Page) {
	if len(pages) == 0 {
		return
	}
	c := NewCleaner(len(pages), RepeatOptions{})
	for _, pg := range pages {
		c.Scan(pg.Text)
	}
	for i := range pages {
		pages[i].Text = c.Apply(pages[i].Text)
	}
}

// spaced trả kết luận sau khi đã addLine đủ các dòng của file.
func (a *docSpacedAcc) spaced() bool {
	return a.total >= 4 && float64(a.qual)/float64(a.total) > 0.15
}

// vowelStart là nguyên âm tiếng Việt (thường + hoa) — run 1-ký-tự mở đầu
// bằng nguyên âm ("ất", "ận") thường là đuôi âm tiết của token ngắn đứng
// trước ("xu", "gi"), còn mở đầu bằng phụ âm ("lệ") là âm tiết mới.
func vowelStart(r rune) bool {
	switch r {
	case 'a', 'á', 'à', 'ả', 'ã', 'ạ', 'ă', 'ắ', 'ằ', 'ẳ', 'ẵ', 'ặ',
		'â', 'ấ', 'ầ', 'ẩ', 'ẫ', 'ậ', 'e', 'é', 'è', 'ẻ', 'ẽ', 'ẹ',
		'ê', 'ế', 'ề', 'ể', 'ễ', 'ệ', 'i', 'í', 'ì', 'ỉ', 'ĩ', 'ị',
		'o', 'ó', 'ò', 'ỏ', 'õ', 'ọ', 'ô', 'ố', 'ồ', 'ổ', 'ỗ', 'ộ',
		'ơ', 'ớ', 'ờ', 'ở', 'ỡ', 'ợ', 'u', 'ú', 'ù', 'ủ', 'ũ', 'ụ',
		'ư', 'ứ', 'ừ', 'ử', 'ữ', 'ự', 'y', 'ý', 'ỳ', 'ỷ', 'ỹ', 'ỵ',
		'A', 'Á', 'À', 'Ả', 'Ã', 'Ạ', 'Ă', 'Ắ', 'Ằ', 'Ẳ', 'Ẵ', 'Ặ',
		'Â', 'Ấ', 'Ầ', 'Ẩ', 'Ẫ', 'Ậ', 'E', 'É', 'È', 'Ẻ', 'Ẽ', 'Ẹ',
		'Ê', 'Ế', 'Ề', 'Ể', 'Ễ', 'Ệ', 'I', 'Í', 'Ì', 'Ỉ', 'Ĩ', 'Ị',
		'O', 'Ó', 'Ò', 'Ỏ', 'Õ', 'Ọ', 'Ô', 'Ố', 'Ồ', 'Ổ', 'Ỗ', 'Ộ',
		'Ơ', 'Ớ', 'Ờ', 'Ở', 'Ỡ', 'Ợ', 'U', 'Ú', 'Ù', 'Ủ', 'Ũ', 'Ụ',
		'Ư', 'Ứ', 'Ừ', 'Ử', 'Ữ', 'Ự', 'Y', 'Ý', 'Ỳ', 'Ỷ', 'Ỹ', 'Ỵ':
		return true
	}
	return false
}

// isPlainLetters báo token toàn chữ cái (không số, không dấu câu) — chỉ
// token này mới được dính đuôi âm tiết (tránh dính vào "3.", "\"ném\"").
func isPlainLetters(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if isLetter(r) {
			continue
		}
		return false
	}
	return true
}

// despaceLine nối các token 1 ký tự đứng liền nhau trong dòng bị rải chữ
// ("Con ng ườ i" → "Con người"). Token nhiều ký tự ("Con", "3.", "\"ném\"")
// giữ nguyên vị trí — chỉ nối đúng chỗ rải, không dồn cả dòng. Run mở đầu
// bằng nguyên âm ("ất", "ận") thì dính luôn vào token chữ đứng trước nếu
// token đó ngắn (≤2 rune: "xu"+"ất" → "xuất", "gi"+"ận" → "giận"); run mở
// đầu bằng phụ âm ("lệ") là âm tiết mới nên tách ("nô lệ").
// isLetter báo 1 rune chữ cái (ta + tây) — dùng chung cho coreLetters và
// isPlainLetters.
func isLetter(r rune) bool {
	return ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') ||
		('à' <= r && r <= 'ỹ') || ('À' <= r && r <= 'Ỹ') ||
		r == 'đ' || r == 'Đ'
}

// coreLetters cắt dấu câu bám đầu/cuối token ("nh.", ""ném"", "(yếu)") để
// xét chữ rời — dấu câu không phải chữ rải.
func coreLetters(t string) string {
	rs := []rune(t)
	i := 0
	for i < len(rs) && !isLetter(rs[i]) {
		i++
	}
	j := len(rs)
	for j > i && !isLetter(rs[j-1]) {
		j--
	}
	return string(rs[i:j])
}

// splitFrag tách token thành dấu đầu + lõi chữ + dấu cuối ("nh." → ""+"nh"
// +"."). Lõi lẫn dấu ở giữa ("a-b", "2024") thì trả lõi rỗng để caller giữ
// nguyên token (số và từ gạch nối không bao giờ là chữ rải).
func splitFrag(t string) (lead, core, trail string) {
	rs := []rune(t)
	i := 0
	for i < len(rs) && !isLetter(rs[i]) {
		i++
	}
	j := len(rs)
	for j > i && !isLetter(rs[j-1]) {
		j--
	}
	core = string(rs[i:j])
	for _, r := range core {
		if !isLetter(r) {
			return "", "", t
		}
	}
	return string(rs[:i]), core, string(rs[j:])
}

// isJoinCore báo lõi token có phải chữ rời cần nối: 1-2 chữ cái (chữ số,
// dấu câu như "2.", "02", "\"ném\"" không bao giờ là chữ rải).
func isJoinCore(core string) bool {
	n := utf8.RuneCountInString(core)
	return n == 1 || n == 2
}

// joinLine nối token rời trong dòng đã xác định rải — không qua ngưỡng,
// caller (despaceLine strict hay vòng force cả file) quyết định gọi hay
// không. Dấu câu bám được giữ đúng chỗ ("yế u," → "yếu,", "mạ nh." →
// "mạnh."). Token nhiều chữ ("Con", "3.") giữ nguyên vị trí — chỉ nối đúng
// chỗ rải, không dồn cả dòng. Run mở đầu bằng nguyên âm ("ất", "ận") thì
// dính luôn vào token chữ đứng trước nếu token đó ngắn và sạch dấu ("xu"+
// "ất" → "xuất"); run mở đầu bằng phụ âm ("lệ") là âm tiết mới nên tách
// ("nô lệ").
func joinLine(line string) string {
	toks := strings.Fields(line)
	if len(toks) == 0 {
		return line
	}
	var sb strings.Builder
	var run []string
	runLead, runTrail := "", ""
	last, lastClean := "", false // token chữ cuối đã ghi (để xét dính đuôi)
	flush := func() {
		if len(run) == 0 {
			return
		}
		joined := strings.Join(run, "")
		syls := splitSyllables(joined)
		if len(syls) == 1 {
			if first, _ := utf8.DecodeRuneInString(joined); vowelStart(first) &&
				utf8.RuneCountInString(last) <= 2 && isPlainLetters(last) && lastClean {
				// Dính đuôi vào token trước: sb đang kết thúc bằng " "+last
				// (lastClean đảm bảo không có dấu bám) nên cắt đúng đuôi đó
				// rồi ghi đè liền mạch ("xu"+"ất").
				s := sb.String()
				sb.Reset()
				sb.WriteString(s[:len(s)-len(last)])
				sb.WriteString(last + joined)
				last += joined
				run = nil
				runLead, runTrail = "", ""
				return
			}
		}
		if sb.Len() > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(runLead + strings.Join(syls, " ") + runTrail)
		// Run độc lập không thành mốc dính cho run sau — tránh run sau cắt
		// nhầm đuôi sb (lúc đó sb không còn kết thúc bằng last).
		last, lastClean = "", false
		run = nil
		runLead, runTrail = "", ""
	}
	for _, t := range toks {
		lead, core, trail := splitFrag(t)
		if !isJoinCore(core) {
			flush()
			if sb.Len() > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(t)
			last, lastClean = core, lead == "" && trail == ""
			continue
		}
		if len(run) > 0 && lead != "" {
			flush() // dấu mở (ngoặc, quote) chen giữa thì chốt run cũ
		}
		if len(run) == 0 {
			runLead = lead
		}
		run = append(run, core)
		runTrail = trail
		if trail != "" {
			flush() // dấu cuối (",", ".", ":") chốt run ngay: "yếu," rồi nối tiếp
		}
	}
	flush()
	return sb.String()
}

// despaceLine (strict) chỉ nối dòng đạt ngưỡng chữ rải — file sạch đi qua
// nguyên vẹn. File đã xác định rải (docSpaced) thì caller dùng joinLine
// thẳng cho mọi dòng.
func despaceLine(line string) string {
	toks := strings.Fields(line)
	if len(toks) == 0 || !isSpacedLine(toks) {
		return line
	}
	return joinLine(line)
}

// isVowel báo nguyên âm tiếng Việt (thường + hoa, có/không dấu) + chữ số
// (số dính trong run thì giữ nguyên, không bao giờ là điểm tách).
func isVowel(r rune) bool {
	if '0' <= r && r <= '9' {
		return true
	}
	return vowelStart(r) || isPlainVowel(r)
}

// isPlainVowel báo nguyên âm không dấu thanh (a ă â e ê i o ô ơ u ư y...).
func isPlainVowel(r rune) bool {
	switch r {
	case 'a', 'ă', 'â', 'e', 'ê', 'i', 'o', 'ô', 'ơ', 'u', 'ư', 'y',
		'A', 'Ă', 'Â', 'E', 'Ê', 'I', 'O', 'Ô', 'Ơ', 'U', 'Ư', 'Y':
		return true
	}
	return false
}

// hasTone báo run có dấu thanh (sắc/huyền/hỏi/ngã/nặng) — run không dấu
// ("Camus", "2024") thì giữ nguyên, khỏi tách bừa từ ngoại lai.
func hasTone(s string) bool {
	for _, r := range s {
		if vowelStart(r) && !isPlainVowel(r) {
			return true
		}
	}
	return false
}

// isConsonant báo phụ âm chữ cái (không phải nguyên âm, không phải số).
func isConsonant(r rune) bool {
	if ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') ||
		('à' <= r && r <= 'ỹ') || ('À' <= r && r <= 'Ỹ') ||
		r == 'đ' || r == 'Đ' {
		return !isVowel(r)
	}
	return false
}

// validOnsets là các phụ âm đầu hợp lệ để tách âm tiết: đơn + cụm đôi/ba.
// Tách chỉ đặt trước onset hợp lệ ("tâm|hồn", "Đạo|đức"), cụm lạ ("mh")
// thì không — tránh chẻ bừa.
var validOnsets = map[string]bool{
	"ch": true, "gh": true, "gi": true, "kh": true, "ng": true,
	"ngh": true, "nh": true, "ph": true, "qu": true, "th": true, "tr": true,
}

// onsetLen trả độ dài onset hợp lệ tại pos (3 → 2 → 1), 0 nếu không có.
func onsetLen(lower []rune, pos int) int {
	for _, n := range []int{3, 2, 1} {
		if pos+n > len(lower) {
			continue
		}
		s := string(lower[pos : pos+n])
		if n > 1 && validOnsets[s] {
			return n
		}
		if n == 1 && isConsonant(lower[pos]) {
			return 1
		}
	}
	return 0
}

// splitSyllables tách run chữ rời đã nối thành các âm tiết ("Đạođức" →
// "Đạo đức"). Quy tắc: chỉ tách khi run có dấu thanh (từ ngoại lai/số như
// "Camus", "2024" giữ nguyên); điểm tách đặt trước onset hợp lệ mà sau nó
// là nguyên âm ("tâm|hồn", "Đạo|đức"); âm tiết hiện tại phải đã có nguyên âm
// (không chẻ trong cụm đầu như "ng|ười"); không tách trước chữ số.
func splitSyllables(s string) []string {
	if !hasTone(s) {
		return []string{s}
	}
	r := []rune(s)
	lower := []rune(strings.ToLower(s))
	var parts []string
	start := 0
	hasVowel := false
	for i := 0; i < len(lower); {
		if isVowel(r[i]) {
			hasVowel = true
			i++
			continue
		}
		if !isConsonant(r[i]) {
			i++ // ký hiệu/số: giữ nguyên, không tách
			continue
		}
		if n := onsetLen(lower, i); n > 0 && i > start && hasVowel &&
			i+n < len(r) && isVowel(r[i+n]) {
			parts = append(parts, string(r[start:i]))
			start = i
			hasVowel = false
		}
		i++
	}
	return append(parts, string(r[start:]))
}
