package eino

import (
	"strings"
	"testing"
)

// Test số do code cắt ra từ text thật của chunk được dẫn — dữ liệu lấy đúng
// hai dạng thấy trong batch Đợt 25 (bảng Excel trích ra số trần, văn bản PDF
// dùng dấu . chia nghìn và dấu , thập phân).

// luongTable cắt nguyên từ snippet cite của conversation 28: hàng tiêu đề 1 ô,
// hàng header, rồi tới dữ liệu và một hàng cộng dồn. Sáu cột số đều có cùng số
// hàng nên đoán mù là vẽ láo — luật chọn cột phải thử được ở đây.
const luongTable = `## Sheet: Luong
| BẢNG LƯƠNG QUÝ 3/2025 - KHỐI VĂN PHÒNG |
| Mã NV | Họ và tên | Chức vụ | Ngày công | Lương ngày (VNĐ) | Lương cơ bản (VNĐ) | Phụ cấp (VNĐ) | Tổng thực nhận (VNĐ) | Thuế TNCN (VNĐ) |
| NV01 | Nguyễn Văn A | Chuyên viên | 22,5 | 200000 | 4500000 | 800000 | 5300000 | 265000 |
| NV02 | Trần Thị B | Nhân viên | 24 | 250000 | 6000000 | 1200000 | 7200000 | 360000 |
| Tổng cộng | | | 46,5 | | 10500000 | 2000000 | 12500000 | 625000 |`

const phuCapTable = `## Sheet: Phu_cap
| Mã NV | Khoản phụ cấp | Mức (VNĐ/tháng) | Điều kiện hưởng |
| NV01 | Phụ cấp ăn trưa | 800000 | Làm việc tại văn phòng từ 22 ngày công trở lên |
| NV02 | Phụ cấp độc hại | 1200000 | Làm việc trong môi trường nặng nhọc |`

const soChiTable = `## Sheet: So_chi_ti_tai
| Số chứng từ | Ngày | Diện giải | Nợ (VNĐ) | Có (VNĐ) |
| CT-011 | 03/10/2025 | Chi phí văn phòng phẩm | 4200000 | 0 |
| CT-012 | 05/10/2025 | Thanh toán tiền điện tháng 9 | 1500000 | 0 |
| CT-013 | 07/10/2025 | Nộp thuế TNCN quý 3 | 360000 | 0 |`

const theoThangTable = `| Tháng | Doanh thu (VNĐ) | Chi phí (VNĐ) |
| 01/07/2025 | 1000000 | 400000 |
| 01/08/2025 | 1200000 | 450000 |
| 01/09/2025 | 900000 | 500000 |`

func TestParseVNNumber(t *testing.T) {
	cases := []struct {
		raw  string
		want float64
		ok   bool
	}{
		{"800000", 800000, true},
		{"21,5", 21.5, true},
		{"0,05%", 0.05, true},
		{"1.500.000", 1500000, true},
		{"150.000 đồng", 150000, true},
		{"3.000", 3000, true},
		{"22.5", 22.5, true},
		{"1,200,000", 1200000, true},
		{"1.200,5", 1200.5, true},
		{"1 500 000", 1500000, true},
		{"-450000", -450000, true},
		{"800000đ", 800000, true},
		{"12.345.67", 12345.67, true}, // dấu cuối 2 số là thập phân, các dấu trước chia nghìn
		{"12.3.4", 0, false},
		// Không phải số: ô CHỨA số bên trong cũng phải bị loại, nếu không cột
		// "Điều kiện hưởng" sẽ giành chỗ cột "Mức (VNĐ/tháng)" và vẽ biểu đồ rác.
		{"từ 22 ngày công trở lên", 0, false},
		{"03/10/2025", 0, false},
		{"NV01", 0, false},
		{"PQ-01", 0, false},
		{"Mức (VNĐ/tháng)", 0, false},
		{"", 0, false},
		{".5", 0, false},
		{"5.", 0, false},
		{"1.2.3", 0, false},
		{"1.2345", 0, false},
	}
	for _, c := range cases {
		got, ok := parseVNNumber(c.raw)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseVNNumber(%q) = %v/%v, muốn %v/%v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

func TestExtractSeriesPicksRichestNumericColumn(t *testing.T) {
	res := extractSeries(luongTable, kindBar, "")
	if res.Note == "" || len(res.Points) == 0 {
		t.Fatalf("note = %q, points = %v — bảng lương thật thì phải vẽ được", res.Note, res.Points)
	}
	// Sáu cột số đều có 2 hàng số, nên luật "tổng lớn nhất" chọn Tổng thực nhận.
	if res.Column != "Tổng thực nhận (VNĐ)" {
		t.Errorf("cột chọn = %q, muốn %q", res.Column, "Tổng thực nhận (VNĐ)")
	}
	want := []dataPoint{
		{Label: "NV01", Value: 5300000},
		{Label: "NV02", Value: 7200000},
	}
	if len(res.Points) != len(want) {
		t.Fatalf("points = %v, muốn %v", res.Points, want)
	}
	for i, p := range res.Points {
		if p != want[i] {
			t.Errorf("point[%d] = %v, muốn %v", i, p, want[i])
		}
	}
	if strings.Contains(res.Note, "Tổng cộng") {
		t.Errorf("hàng cộng dồn bị vẽ hai lần: note = %q", res.Note)
	}
}

func TestExtractSeriesIgnoresNumbersInsideTextCells(t *testing.T) {
	res := extractSeries(phuCapTable, kindPie, "")
	if len(res.Points) != 2 {
		t.Fatalf("points = %v (note %q), muốn 2 điểm phụ cấp", res.Points, res.Note)
	}
	if res.Column != "Mức (VNĐ/tháng)" {
		t.Errorf("cột chọn = %q, muốn %q", res.Column, "Mức (VNĐ/tháng)")
	}
	if res.Points[0].Label != "NV01" || res.Points[0].Value != 800000 {
		t.Errorf("point[0] = %v, muốn NV01/800000", res.Points[0])
	}
}

func TestExtractSeriesHonoursRequestedColumn(t *testing.T) {
	res := extractSeries(luongTable, kindBar, "phụ cấp")
	if res.Column != "Phụ cấp (VNĐ)" {
		t.Fatalf("cột chọn = %q (note %q), muốn %q", res.Column, res.Note, "Phụ cấp (VNĐ)")
	}
	if res.Points[1].Value != 1200000 {
		t.Errorf("point[1] = %v, muốn 1200000", res.Points[1])
	}
	// Model cần biết code đã lấy cột nào để không kể lại sai cho người dùng.
	if !strings.Contains(res.Note, "Phụ cấp (VNĐ)") {
		t.Errorf("note phải nói rõ cột đã vẽ: %q", res.Note)
	}
}

func TestExtractSeriesUnknownColumnListsCandidates(t *testing.T) {
	res := extractSeries(luongTable, kindBar, "thế chấp")
	if len(res.Points) != 0 {
		t.Fatalf("vẽ khi không có cột yêu cầu: %v", res.Points)
	}
	if !strings.Contains(res.Note, "không có cột số nào khớp") || !strings.Contains(res.Note, "Phụ cấp (VNĐ)") {
		t.Errorf("note phải khớp và liệt kê cột dùng được: %q", res.Note)
	}
}

func TestExtractSeriesRejectsProseWithoutTable(t *testing.T) {
	res := extractSeries("Phí bản sao từ trang 01 đến trang 10: 3.000 VNĐ/trang.", kindBar, "")
	if len(res.Points) != 0 || !strings.Contains(res.Note, "không có bảng") {
		t.Fatalf("points = %v, note = %q — văn bản không có bảng thì phải từ chối", res.Points, res.Note)
	}
}

func TestExtractSeriesRejectsColumnWithoutEnoughRows(t *testing.T) {
	text := "| Mã NV | Họ và tên | Phụ cấp (VNĐ) |\n| NV01 | A | 800000 |\n| NV02 | B | không có |"
	res := extractSeries(text, kindBar, "")
	if len(res.Points) != 0 || !strings.Contains(res.Note, "không có cột số nào") {
		t.Fatalf("points = %v, note = %q — 1 hàng số dưới sàn 2 điểm", res.Points, res.Note)
	}
}

func TestExtractSeriesCeilsPoints(t *testing.T) {
	var b strings.Builder
	b.WriteString("| Mã | Số tiền (VNĐ) | Ghi chú |\n")
	for i := range maxSeriesPoints + 1 {
		b.WriteString("| M" + string(rune('A'+i)) + " | 1000 | x |\n")
	}
	res := extractSeries(b.String(), kindBar, "")
	if len(res.Points) != 0 {
		t.Fatalf("vượt trần mà vẫn vẽ %d điểm", len(res.Points))
	}
	if !strings.Contains(res.Note, "vượt trần") {
		t.Errorf("note phải nói vượt trần: %q", res.Note)
	}
}

func TestExtractSeriesLineNeedsOrderedLabels(t *testing.T) {
	ordered := extractSeries(theoThangTable, kindLine, "")
	if ordered.Kind != kindLine {
		t.Errorf("kind = %q, muốn line (nhãn là ngày tăng dần): note %q", ordered.Kind, ordered.Note)
	}
	if ordered.Points[0].Value != 1000000 {
		t.Errorf("cột mặc định = %q với point[0] %v, muốn doanh thu 1000000", ordered.Column, ordered.Points[0])
	}

	// Chứng từ CT-011..013 không phải trục thời gian => rơi về bar kèm note,
	// không vẽ đường biểu diễn một thứ không phải dãy liền.
	fallback := extractSeries(soChiTable, kindLine, "")
	if fallback.Kind != kindBar {
		t.Errorf("kind = %q, muốn bar khi nhãn không lập thành dãy", fallback.Kind)
	}
	if !strings.Contains(fallback.Note, "đã vẽ bar") {
		t.Errorf("note phải nói lý do rơi kind: %q", fallback.Note)
	}
	if fallback.Column != "Nợ (VNĐ)" {
		t.Errorf("cột chọn = %q, muốn Nợ (VNĐ)", fallback.Column)
	}
}

func TestExtractSeriesRejectsUnknownKind(t *testing.T) {
	res := extractSeries(luongTable, "scatter", "")
	if len(res.Points) != 0 || !strings.Contains(res.Note, "kind không hợp lệ") {
		t.Fatalf("points = %v, note = %q — loại biểu đồ không hỗ trợ phải nói rõ", res.Points, res.Note)
	}
}

func TestPipeRowSplitsCells(t *testing.T) {
	cases := []struct {
		line  string
		cells int
		ok    bool
	}{
		{"| NV01 | Ăn trưa | 800000 |", 3, true},
		{"| BẢNG LƯƠNG QUÝ 3/2025 - KHỐI VĂN PHÒNG |", 1, false}, // 1 ô = tiêu đề
		{"## Sheet: Luong", 0, false},
		{"văn bản thường không có pipe", 0, false},
		{"| A | B |", 2, true},
	}
	for _, c := range cases {
		cells, ok := pipeRow(c.line)
		if ok != c.ok || (ok && len(cells) != c.cells) {
			t.Errorf("pipeRow(%q) = %v/%v, muốn %d ô/%v", c.line, cells, ok, c.cells, c.ok)
		}
	}
}
