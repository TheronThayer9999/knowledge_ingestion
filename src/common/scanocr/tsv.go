package scanocr

import (
	"bufio"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// tsvRow là một dòng Tesseract TSV đã parse. Chỉ giữ các cột tính năng này dùng;
// level không nằm trong struct vì sau khi lọc level == 5 thì nó vô nghĩa.
type tsvRow struct {
	block, par int
	line       int
	box        Box
	conf       float64
	text       string
}

// parseTSV đổi output `tesseract ... tsv` thành Page với Box tính theo pixel của
// ảnh đã OCR (img.WidthPx/HeightPx).
//
// Chỉ nhận level == 5 (từ). Các level thấp hơn (block/para/line) được dựng lại
// bằng cách GỘP từ theo (block, par, line) thay vì đọc dòng level 4, vì hai lý:
//   - dòng level 4 của Tesseract thường có text rỗng (chỉ có box), còn box của
//     nó là bao cả vùng trắng giữa các từ — dùng để tô sáng thì rất xấu;
//   - gộp từ cho box sát nội dung và luôn có text để hiển thị.
//
// conf < 0 là ký hiệu "dòng này không phải chữ" của Tesseract — loại từ luôn,
// không để nó lọt vào lớp chữ vô hình (một dấu "-" cũng làm Ctrl+F ra kết quả lạ).
func parseTSV(out string, img ImageInfo) (*Page, error) {
	var (
		rows  []tsvRow
		order []lineKey
		seen  = map[lineKey]bool{}
	)
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	header := map[string]int{}
	for scanner.Scan() {
		cols := strings.Split(scanner.Text(), "\t")
		if len(header) == 0 {
			// Tesseract có thể in dòng trống trước header: dòng nào chưa khớp
			// thì thử dòng kế, nhưng không có header thì cả output vô nghĩa.
			header = indexTSVHeader(cols)
			continue
		}
		row, ok := parseTSVRow(cols, header)
		if !ok {
			continue
		}
		// Dòng đầu tiên của một khoá quyết định vị trí của dòng đó trong trang;
		// nếu dùng "khác dòng cuối" như cách cũ thì hai dòng cùng tên xen kẽ sẽ
		// thành hai mục và mục thứ hai mất sạch từ (grouped bị xoá từ lần trước).
		if !seen[row.key()] {
			seen[row.key()] = true
			order = append(order, row.key())
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("đọc tsv: %w", err)
	}
	if len(header) == 0 {
		// Không tìm thấy header ở bất kỳ dòng nào: output không phải TSV của
		// Tesseract (binary trỏ nhầm, hoặc bản khác in text suông) — báo rõ để
		// worker ghi lý do chứ không im lặng trả trang 0 dòng.
		return nil, fmt.Errorf("tsv không có dòng header hợp lệ")
	}
	return pageFromRows(order, rows, img), nil
}

// pageFromRows gom từ theo dòng, giữ nguyên thứ tự xuất hiện trong TSV. Tách ra
// khỏi parseTSV để test trực tiếp không cần chuỗi tsv.
func pageFromRows(order []lineKey, rows []tsvRow, img ImageInfo) *Page {
	grouped := make(map[lineKey][]tsvRow, len(order))
	for _, r := range rows {
		grouped[r.key()] = append(grouped[r.key()], r)
	}
	page := &Page{Num: img.Page, WidthPx: float64(img.WidthPx), HeightPx: float64(img.HeightPx), DPI: img.DPI}
	for _, k := range order {
		group := grouped[k]
		if len(group) == 0 {
			continue
		}
		page.Lines = append(page.Lines, lineFromWords(group))
	}
	return page
}

func lineFromWords(words []tsvRow) Line {
	line := Line{Words: make([]Word, 0, len(words))}
	box := words[0].box
	var sum float64
	var parts []string
	for _, w := range words {
		line.Words = append(line.Words, Word{Text: w.text, Box: w.box, Conf: w.conf})
		box = unionBox(box, w.box)
		sum += w.conf
		parts = append(parts, w.text)
	}
	line.Box = box
	line.Text = strings.Join(parts, " ")
	line.Conf = sum / float64(len(words))
	return line
}

// lineKey là danh tính một dòng theo cách Tesseract đánh số.
type lineKey struct{ block, par, line int }

func (r tsvRow) key() lineKey { return lineKey{r.block, r.par, r.line} }

// unionBox bao hai hình chữ nhật — dùng để dựng box dòng từ box các từ.
func unionBox(a, b Box) Box {
	x1 := math.Min(a.X, b.X)
	y1 := math.Min(a.Y, b.Y)
	x2 := math.Max(a.X+a.W, b.X+b.W)
	y2 := math.Max(a.Y+a.H, b.Y+b.H)
	return Box{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}
}

// indexTSVHeader nhận diện dòng header. Trả map rỗng nếu không phải header để
// caller còn cơ hội ở dòng kế tiếp (Tesseract có thể in dòng trống trước).
func indexTSVHeader(cols []string) map[string]int {
	out := map[string]int{}
	for i, c := range cols {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case "level", "block_num", "par_num", "line_num", "word_num",
			"left", "top", "width", "height", "conf", "text":
			out[strings.ToLower(strings.TrimSpace(c))] = i
		}
	}
	// Thiếu cột sống còn thì coi như không phải header.
	for _, need := range []string{"level", "left", "top", "width", "height", "conf", "text"} {
		if _, ok := out[need]; !ok {
			return map[string]int{}
		}
	}
	return out
}

// parseTSVRow đổi một dòng TSV thành tsvRow, ok=false nếu đó không phải một từ
// đọc được (level != 5, text rỗng, conf -1, số không parse được).
func parseTSVRow(cols []string, h map[string]int) (tsvRow, bool) {
	get := func(name string) string {
		i, ok := h[name]
		if !ok || i >= len(cols) {
			return ""
		}
		return strings.TrimSpace(cols[i])
	}
	num := func(name string) (float64, bool) {
		v, err := strconv.ParseFloat(get(name), 64)
		return v, err == nil
	}
	level, ok := numInt(get("level"))
	if !ok || level != 5 {
		return tsvRow{}, false
	}
	text := get("text")
	// text đã là một cột sau khi Split theo '\t' nên không thể còn tab trong đó;
	// chỉ cần chặn rỗng — một từ không có chữ thì chẳng hiển thị được gì và chỉ
	// làm lớp chữ vô hình sinh ra ký tự lạ.
	if text == "" {
		return tsvRow{}, false
	}
	conf, ok := num("conf")
	if !ok || conf < 0 {
		return tsvRow{}, false
	}
	box, ok := tsvBox(num)
	if !ok {
		return tsvRow{}, false
	}
	block, _ := numInt(get("block_num"))
	par, _ := numInt(get("par_num"))
	line, _ := numInt(get("line_num"))
	return tsvRow{block: block, par: par, line: line,
		box: box, conf: conf, text: text}, true
}

// tsvBox đọc hình chữ nhật pixel của một từ từ bốn cột toạ độ.
//
// Tách riêng vì đây là chỗ duy nhất chấp nhận "số hỏng": left/top đọc được mà
// width/height bằng 0 thì box vô nghĩa — vẽ lên UI sẽ thành một vạch, nhúng vào
// lớp chữ vô hình sẽ thành chữ chồng chữ, nên loại cả từ là đúng.
func tsvBox(num func(string) (float64, bool)) (Box, bool) {
	left, ok1 := num("left")
	top, ok2 := num("top")
	width, ok3 := num("width")
	height, ok4 := num("height")
	if !ok1 || !ok2 || !ok3 || !ok4 || width <= 0 || height <= 0 {
		return Box{}, false
	}
	return Box{X: left, Y: top, W: width, H: height}, true
}

func numInt(s string) (int, bool) {
	v, err := strconv.Atoi(s)
	return v, err == nil
}
