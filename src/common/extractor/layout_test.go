package extractor

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestDetectLayout(t *testing.T) {
	// Lưu ý: đầu vào là text `pdftotext -layout` — cột canh bằng KHOẢNG TRẮNG,
	// không có ký tự `|` (đó là markdown của mình, không phải poppler).
	tests := []struct {
		name        string
		text        string
		wantComplex bool
	}{
		{
			name: "chữ thuần có thụt lề đầu đoạn",
			text: "   Điều 1. Phạm vi điều chỉnh\n" +
				"   Quy chế này quy định về việc thu nộp và quản lý các khoản phí.\n" +
				"   Điều 2. Đối tượng áp dụng\n" +
				"   Áp dụng cho mọi tổ chức, cá nhân có liên quan.\n" +
				"   Điều 3. Giải thích từ ngữ\n" +
				"   Các từ ngữ dưới đây được hiểu như sau.",
			wantComplex: false,
		},
		{
			name: "bảng 3 cột canh bằng khoảng trắng",
			text: "STT   Hành vi                   Mức phạt\n" +
				"1     Chậm nộp hồ sơ            200.000\n" +
				"2     Khai sai nội dung          500.000\n" +
				"3     Trốn thuế                 1.000.000\n" +
				"4     Không nộp tờ khai          100.000\n" +
				"Ghi chú: áp dụng từ 01/07/2026",
			wantComplex: true,
		},
		{
			name: "trang số liệu, cột số dính nhau",
			text: "Chỉ tiêu 2021 2022 2023\n" +
				"Doanh thu 12 45 67\n" +
				"Chi phí 8 30 41\n" +
				"Lợi nhuận 4 15 26\n" +
				"Thuế 1 3 5\n" +
				"Tỷ lệ 12,5 30,2 41,8",
			wantComplex: true,
		},
		{
			name: "mục lục chỉ có dấu chấm dẫn trang",
			text: "Mục 1 Máy tính bảng .................... 3\n" +
				"Mục 2 Thiết bị di động .................... 7\n" +
				"Mục 3 Phụ kiện .................... 9\n" +
				"Mục 4 Bảo hành .................... 11\n" +
				"Mục 5 Liên hệ .................... 13\n" +
				"Mục 6 Phụ lục .................... 15",
			wantComplex: false,
		},
		{
			name: "đường kẻ một mình không đủ kết luận",
			text: "--------------------\n" +
				"--------------------\n" +
				"--------------------\n" +
				"--------------------\n" +
				"--------------------\n" +
				"--------------------",
			wantComplex: false,
		},
		{
			name:        "trang rỗng",
			text:        "",
			wantComplex: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectLayout(tc.text)
			if got.Complex() != tc.wantComplex {
				t.Fatalf("complex = %v (score %.2f, signals %+v), muốn %v",
					got.Complex(), got.Score(), got, tc.wantComplex)
			}
		})
	}
}

// TestLayoutScoreBaoHoa khoá bất biến: Score luôn trong [0,1] — dùng để lưu
// thẳng vào article_pages.layout_score, sau này đổi trọng số không được làm
// tràn range đó.
func TestLayoutScoreBaoHoa(t *testing.T) {
	max := LayoutSignals{Lines: 20, GapRows: 20, NumericRows: 20, RuleRows: 20}
	if s := max.Score(); s < 0 || s > 1 {
		t.Fatalf("score = %v, muốn trong [0,1]", s)
	}
	if !max.Complex() {
		t.Error("trang có đủ 3 dấu hiệu phải là complex")
	}
}

func TestIsComplex(t *testing.T) {
	if !IsComplex(layoutComplexThreshold) {
		t.Error("đúng ngưỡng phải là complex")
	}
	if IsComplex(layoutComplexThreshold - 0.01) {
		t.Error("dưới ngưỡng không được là complex")
	}
	if IsComplex(0) {
		t.Error("điểm 0 (tắt đo hoặc trang sạch) không được là complex")
	}
}

// TestPDFLayoutScoreWiring khoá phần NỐI DÂY, không phải phần chấm điểm: đo
// thật một PDF có bảng qua pdftotext để chắc rằng (1) option thực sự truyền
// `-layout` xuống, (2) điểm theo Page ra tới người gọi, (3) không bật option
// thì không đo. Thiếu test này, một chỗ quên trải option sẽ âm thầm cho mọi
// trang điểm 0 và pipeline không bao giờ thấy trang bảng.
func TestPDFLayoutScoreWiring(t *testing.T) {
	raw := buildPDFWithContent(t, tableContent())

	collect := func(opts ...ExtractOption) []Page {
		var pages []Page
		err := ExtractRawPages(t.Context(), "uploads/bang.pdf", bytes.NewReader(raw),
			OCRConfig{RenderBinary: popplerPdftoppm(t)}, nil, func(p Page) error {
				pages = append(pages, p)
				return nil
			}, opts...)
		if err != nil {
			t.Fatalf("extract lỗi: %v", err)
		}
		return pages
	}

	on := collect(WithLayoutDetection(true))
	if len(on) != 1 {
		t.Fatalf("want 1 trang, got %d", len(on))
	}
	if !IsComplex(on[0].Layout) {
		t.Fatalf("bảng 3 cột phải complex, điểm = %v", on[0].Layout)
	}
	if off := collect(); off[0].Layout != 0 {
		t.Fatalf("không bật option thì không được đo, got %v", off[0].Layout)
	}
}

// tableContent dựng content stream của 1 trang bảng 3 cột: mỗi ô là một text
// object ở toạ độ x riêng, nên pdftotext -layout sinh khe cột THẬT — đúng thứ
// detector cần đo, không phải text giả lập.
func tableContent() string {
	var b strings.Builder
	rows := [][3]string{
		{"STT", "Hanh vi vi pham", "Muc phat"},
		{"1", "Cham nop ho so", "200.000"},
		{"2", "Khai sai noi dung", "500.000"},
		{"3", "Tron thue", "1000.000"},
		{"4", "Khong nop to khai", "100.000"},
		{"5", "Nop muon qua han", "300.000"},
	}
	y := 720
	for _, r := range rows {
		for _, cell := range []struct {
			x int
			s string
		}{{72, r[0]}, {260, r[1]}, {440, r[2]}} {
			fmt.Fprintf(&b, "BT /F1 10 Tf %d %d Td (%s) Tj ET\n", cell.x, y, cell.s)
		}
		y -= 24
	}
	return b.String()
}
