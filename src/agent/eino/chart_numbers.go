package eino

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Cắt chuỗi số từ CHÍNH text của chunk đã dẫn (plans/2026-10-27-dot-26 muc 3.3).
// Vì sao nằm ở đây chứ không để model gõ số vào spec: biểu đồ sai số trông
// thuyết phục hơn một đoạn văn sai, nên model chỉ được nói "nguồn nào, loại gì,
// cột nào" — mọi con số do code lấy từ text trích dẫn, rồi cổng validate ở
// chart_from_cite.go đối chiếu lại một lần nữa.

const (
	// minSeriesPoints là sàn điểm: một cột chỉ 1 hàng số thì vẽ biểu đồ là bịa
	// thêm ngữ cảnh, thà trả lời bằng văn.
	minSeriesPoints = 2
	// maxSeriesPoints là trần điểm: pie 30 lát không đọc ra gì, bar dài như vậy
	// cũng vậy — từ chối chứ không vẽ dở.
	maxSeriesPoints = 12
	// minTableCells: hàng bảng thật phải có ít nhất từng này ô. Hàng tiêu đề
	// của Excel trích ra dạng "| BẢNG LƯƠNG QUÝ 3/2025 |" (1 ô) vì thế bị loại,
	// không bị nhầm thành dữ liệu.
	minTableCells = 2

	kindBar  = "bar"
	kindLine = "line"
	kindPie  = "pie"
)

// dataPoint là một điểm trên biểu đồ.
type dataPoint struct {
	Label string
	Value float64
}

// seriesResult là kết quả trích chuỗi số. Points rỗng thì Note bắt buộc giải
// thích vì sao không vẽ được — tool đưa note này cho model, không được im lặng.
type seriesResult struct {
	Points []dataPoint
	// Column là tên cột đã chọn để vẽ (rỗng khi text không có bảng).
	Column string
	// Kind là loại biểu đồ thực vẽ — có thể khác kind xin vì rơi về bar.
	Kind string
	Note string
}

// extractSeries là cổng duy nhất từ text chunk tới số của biểu đồ. wantColumn
// rỗng nghĩa là model không chỉ định, code tự chọn theo thứ tự ưu tiên cố định
// trong pickColumn.
func extractSeries(text, kind, wantColumn string) seriesResult {
	res := seriesResult{Kind: normalizeKind(kind)}
	if res.Kind == "" {
		res.Note = "kind không hợp lệ — chart_from_cite chỉ nhận bar, line, pie"
		return res
	}
	header, rows := tableOf(text)
	if len(rows) == 0 {
		res.Note = "nguồn dẫn không có bảng dạng | ô | ô | nên không cắt được chuỗi số — hãy trả lời bằng văn bản hoặc bảng markdown"
		return res
	}
	stats := numericColumns(header, rows)
	if len(stats) == 0 {
		res.Note = "bảng trong nguồn không có cột số nào ở từ 2 hàng trở lên nên không có gì để vẽ"
		return res
	}
	col, note := pickColumn(stats, wantColumn)
	if note != "" {
		res.Note = note
		return res
	}
	res.Column = col.Name
	pts, note := pointsInBudget(rows, col.Index)
	if note != "" {
		res.Note = note
		return res
	}
	res.Kind, res.Note = fitKind(res.Kind, pts, col)
	res.Points = pts
	return res
}

// normalizeKind trả "" cho mọi loại không hỗ trợ — để tool nói rõ chứ không âm
// thầm vẽ bar khi model xin scatter.
func normalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case kindBar:
		return kindBar
	case kindLine:
		return kindLine
	case kindPie:
		return kindPie
	}
	return ""
}

// tableOf tách text thành hàng header (toàn ô chữ) và các hàng dữ liệu sau nó.
// Hàng ngoài bảng pipe bị bỏ: chúng là "## Sheet:" hoặc chữ của file PDF.
func tableOf(text string) (header []string, rows [][]string) {
	for _, line := range strings.Split(text, "\n") {
		cells, ok := pipeRow(line)
		if !ok {
			continue
		}
		if header == nil && !hasNumericCell(cells) {
			header = cells
			continue
		}
		if isTotalRow(cells) {
			continue
		}
		rows = append(rows, cells)
	}
	return header, rows
}

// pipeRow tách một dòng bảng pipe thành các ô đã trim. TrimSpace luôn ăn cả
// "\r" nên không cần xử lý riêng cho file Windows.
func pipeRow(line string) ([]string, bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "|") || strings.Count(s, "|") < minTableCells+1 {
		return nil, false
	}
	cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(s, "|"), "|"), "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells, true
}

func hasNumericCell(cells []string) bool {
	for _, c := range cells {
		if _, ok := parseVNNumber(c); ok {
			return true
		}
	}
	return false
}

// totalRe bắt hàng cộng dồn — vẽ luôn hàng này là tính hai lần một tổng.
var totalRe = regexp.MustCompile(`(?i)tổng cộng|total|^cộng$`)

func isTotalRow(cells []string) bool {
	for _, c := range cells {
		if totalRe.MatchString(c) {
			return true
		}
	}
	return false
}

// columnStat là độ "số" của một cột: bao nhiêu hàng parse được số và tổng giá
// trị tuyệt đối của chúng.
type columnStat struct {
	Index int
	Name  string
	Count int
	Sum   float64
}

// numericColumns đếm số cho từng cột, chỉ giữ cột đủ sàn minSeriesPoints.
func numericColumns(header []string, rows [][]string) []columnStat {
	width := 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	stats := make([]columnStat, 0, width)
	for i := range width {
		st := columnStat{Index: i, Name: columnName(header, i)}
		for _, r := range rows {
			if v, ok := cellNumber(r, i); ok {
				st.Count++
				st.Sum += math.Abs(v)
			}
		}
		if st.Count >= minSeriesPoints {
			stats = append(stats, st)
		}
	}
	return stats
}

func columnName(header []string, i int) string {
	if i < len(header) {
		return header[i]
	}
	return fmt.Sprintf("cột %d", i+1)
}

func cellNumber(cells []string, i int) (float64, bool) {
	if i >= len(cells) {
		return 0, false
	}
	return parseVNNumber(cells[i])
}

// pickColumn chọn cột vẽ. Model gọi tên cột thì theo tên; không gọi thì ưu tiên
// cố định: nhiều hàng số nhất, rồi tổng lớn nhất, rồi cột trái nhất. Thứ tự này
// bất định nên lần chạy sau vẽ đúng cột của lần trước.
func pickColumn(stats []columnStat, want string) (columnStat, string) {
	want = strings.TrimSpace(want)
	if want == "" {
		best := stats[0]
		for _, st := range stats[1:] {
			if st.Count > best.Count || (st.Count == best.Count && st.Sum > best.Sum) {
				best = st
			}
		}
		return best, ""
	}
	for _, st := range stats {
		if sameColumnName(st.Name, want) {
			return st, ""
		}
	}
	return columnStat{}, fmt.Sprintf("không có cột số nào khớp %q — các cột vẽ được: %s", want, columnList(stats))
}

func sameColumnName(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	return a == b || strings.Contains(a, b) || strings.Contains(b, a)
}

// columnList kê tên các cột ứng viên cho note, kèm số hàng số để model hiểu vì
// sao cột nó muốn không có.
func columnList(stats []columnStat) string {
	parts := make([]string, 0, len(stats))
	for i, st := range stats {
		if i == listSheetsMaxNames {
			parts = append(parts, "...")
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%d hàng số)", st.Name, st.Count))
	}
	return strings.Join(parts, "; ")
}

// pointsInBudget cắt điểm theo trần; note khác rỗng là từ chối vẽ. Sàn điểm
// không kiểm tra ở đây vì numericColumns đã lọc cột có dưới minSeriesPoints
// hàng số rồi — hai chỗ đếm cùng một dấu.
func pointsInBudget(rows [][]string, idx int) ([]dataPoint, string) {
	pts := make([]dataPoint, 0, len(rows))
	for _, r := range rows {
		v, ok := cellNumber(r, idx)
		if !ok {
			continue
		}
		pts = append(pts, dataPoint{Label: rowLabel(r), Value: v})
	}
	if len(pts) > maxSeriesPoints {
		return nil, fmt.Sprintf("nguồn có %d hàng số, vượt trần %d điểm — biểu đồ sẽ không đọc ra gì, hãy trả lời bằng bảng", len(pts), maxSeriesPoints)
	}
	return pts, ""
}

// rowLabel lấy ô đầu tiên không phải số làm nhãn ("NV01" chứ không phải
// "800000"); cả hàng toàn số thì nhãn rỗng.
func rowLabel(cells []string) string {
	for _, c := range cells {
		if c == "" {
			continue
		}
		if _, ok := parseVNNumber(c); !ok {
			return c
		}
	}
	return ""
}

// fitKind chốt loại vẽ thật kèm note: line chỉ đáng vẽ khi nhãn lập thành dãy
// tăng dần (ngày/tháng/quý/năm/số), dữ liệu phân loại thì là bar.
func fitKind(kind string, pts []dataPoint, col columnStat) (string, string) {
	note := fmt.Sprintf("đã vẽ cột %q (%d hàng có số)", col.Name, col.Count)
	if kind != kindLine || orderedLabels(pts) {
		return kind, note
	}
	return kindBar, note + "; kind line không hợp với dữ liệu phân loại, đã vẽ bar"
}

// orderedLabels kiểm tra nhãn tăng dần — điều kiện duy nhất để một chuỗi đáng
// vẽ đường thay vì cột.
func orderedLabels(pts []dataPoint) bool {
	prev := 0.0
	for i, p := range pts {
		v, ok := labelOrdinal(p.Label)
		if !ok {
			return false
		}
		if i > 0 && v <= prev {
			return false
		}
		prev = v
	}
	return len(pts) > 0
}

var (
	dateRe    = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{2,4})$`)
	monthRe   = regexp.MustCompile(`(?i)^tháng\s*(\d{1,2})$`)
	quarterRe = regexp.MustCompile(`(?i)^q([1-4])\s*(\d{4})?$`)
	seqRe     = regexp.MustCompile(`^\d+(\.\d+)?$`)
)

// labelOrdinal quy một nhãn về trục số để so tăng dần.
func labelOrdinal(label string) (float64, bool) {
	s := strings.TrimSpace(label)
	if m := dateRe.FindStringSubmatch(s); m != nil {
		return dateOrdinal(m[1], m[2], m[3])
	}
	if m := monthRe.FindStringSubmatch(s); m != nil {
		return float64(parseIntOrZero(m[1])), true
	}
	if m := quarterRe.FindStringSubmatch(s); m != nil {
		return float64(parseIntOrZero(m[1])), true
	}
	if seqRe.MatchString(s) {
		v, err := strconv.ParseFloat(s, 64)
		return v, err == nil
	}
	return 0, false
}

func dateOrdinal(dd, mm, yy string) (float64, bool) {
	y := parseIntOrZero(yy)
	if len(yy) == 2 {
		y += 2000
	}
	m, d := parseIntOrZero(mm), parseIntOrZero(dd)
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return 0, false
	}
	return float64(time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC).Unix()), true
}

func parseIntOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// numberUnits là đơn vị đứng sau số trong văn bản tiếng Việt. Chỉ bỏ đơn vị,
// KHÔNG nhân thêm "ngàn"/"triệu" — gặp dạng đó thì chịu, thà không vẽ.
var numberUnits = []string{"VND", "ĐỒNG", "Đ", "USD", "EUR", "%"}

// parseVNNumber đọc một số viết kiểu Việt Nam: "1.500.000" là một triệu năm
// trăm nghìn (dấu . chia nghìn), "21,5" và "0,05" dùng dấu , thập phân,
// "800000" là số trần Excel trích ra. Chỉ nhận ô LÀ một số — "từ 22 ngày công"
// hay "03/10/2025" không phải số và phải bị loại.
//
// Hai quyết định có chủ đích vì corpus tiếng Việt: "1.500" là 1500 (chia nghìn)
// chứ không phải 1.5, vì dấu cuối có đúng 3 chữ số theo sau thì theo luật nhóm;
// và "1,200,000" kiểu quốc tế cũng ra 1200000 — cùng kết quả nên không cần phân
// biệt.
func parseVNNumber(raw string) (float64, bool) {
	s := numberToken(raw)
	if s == "" {
		return 0, false
	}
	intPart, frac := "", ""
	switch {
	case plainNumberRe.MatchString(s): // "800000" — Excel trả số trần, không có dấu chia
		intPart = s
	default:
		m := groupedNumberRe.FindStringSubmatch(s)
		if m == nil {
			return 0, false
		}
		intPart, frac = m[1], m[2]
	}
	norm := stripSeparators(intPart)
	if frac != "" {
		norm += "." + frac
	}
	v, err := strconv.ParseFloat(norm, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// numberToken dọn khoảng trắng (kể cả NBSP do PDF sinh ra) và đơn vị tiền hoặc
// phần trăm đứng cuối.
func numberToken(raw string) string {
	s := strings.ToUpper(numberSpace.Replace(strings.TrimSpace(raw)))
	for _, unit := range numberUnits {
		if strings.HasSuffix(s, unit) {
			return strings.TrimSuffix(s, unit)
		}
	}
	return s
}

var (
	numberSpace = strings.NewReplacer(" ", "", "\t", "", "\u00a0", "", "\u202f", "")
	// plainNumberRe là số không có dấu chia ("800000", "-15").
	plainNumberRe = regexp.MustCompile(`^[+-]?\d+$`)
	// groupedNumberRe bắt định dạng có dấu chia. Nhóm \d{1,3}([.,]\d{3})* khớp
	// tham lam nên "1.500" là 1500 (chia nghìn) chứ không phải 1.5; phần thập
	// phân chỉ được 1-2 chữ số, vì vậy "22.5"/"0,05" vẫn nhận còn "5." thì không.
	groupedNumberRe = regexp.MustCompile(`^([+-]?\d{1,3}(?:[.,]\d{3})*)(?:[.,](\d{1,2}))?$`)
)

func isSeparator(r rune) bool { return r == '.' || r == ',' }

func stripSeparators(s string) string {
	return strings.Map(func(r rune) rune {
		if isSeparator(r) {
			return -1
		}
		return r
	}, s)
}
